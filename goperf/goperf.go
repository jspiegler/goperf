/* goperf.go -- network test tool modeled after iperf
 * 4 modes: TCP client and server, UDP client and server
 * currently, clients connect and send data, servers listen and receive data
 * UDP server measures inter-packet arrival times and calculates jitter
 * other features to come...
 *
 * Developed by Jeffrey D. Spiegler
 * Copyright (c) 2018 Scimitar Global Systems Corp. All rights reserved.
 */

package main

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"os/signal"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

const goperfVersion string = "0.7.0"

type droppedTicksTracker struct {
	sync.Mutex
	N uint64
}

/******************************************************************************/
/******************************************************************************/
/* logRate data strucrures ****************************************************/
/******************************************************************************/
/******************************************************************************/
/******************************************************************************/
type LogData struct {
	sync.Mutex
	NBytes          int
	JitterSum       uint64
	JitterN         uint64
	NPktsSent       uint64
	NPktsRecd       uint64
	NPktsDropped    uint64
	NPktsOutOfOrder uint64
	RcvdSeqNumber   uint64
	SentSeqNumber   uint64

	NSec uint64 // written by lograte, allows data generators/receivers to check time limit

	NWrites       uint64
	NDroppedTicks uint64

	InUse    bool
	Shutdown bool
	Extended int
}

const LOG_EXTEND_NO = 0
const LOG_EXTEND_UDP_CLIENT = 1
const LOG_EXTEND_UDP_SERVER = 2

const N_LINES_HDR_REFRESH = 24

var prevNDroppedTicks uint64 = 0

/******************************************************************************/
type historyTrack struct {
	current      uint64
	last10       [10]uint64
	nFilled      int
	nextInLast10 int
	totalEver    uint64
	totalLast10  uint64
}

/******************************************************************************/
/******************************************************************************/
/* end logRate data strucrures ************************************************/
/******************************************************************************/
/******************************************************************************/

var dt droppedTicksTracker

var exitCode int = 0

/******************************************************************************/
func main() {
	defer func() {
		os.Exit(exitCode)
	}()

	tcpPtr := flag.Bool("tcp", false, "tcp")
	udpPtr := flag.Bool("udp", false, "udp")
	cPtr := flag.String("c", "", "c host:port, client, make connection to host:port")
	sPtr := flag.String("s", "", "s N, server, listen on port N (all interfaces)")
	scrollPtr := flag.Bool("scroll", false, "makes output scroll")
	tsPtr := flag.Bool("ts", false, "print timestamp")
	mbpsPtr := flag.Int64("Mbps", -1, "Mbps nnn, specify udp rate in megabits per second")
	psizePtr := flag.Int64("psize", -1, "psize mmm, use mmm packet size (+ IP/UDP headers) for udp")
	ppsPtr := flag.Int64("pps", -1, "pps zzz, packets per second to send for udp")
	nbPtr := flag.Int64("nb", -1, "nb nnn, send/receive nnn bytes then quit (default: no byte limit)")
	nsPtr := flag.Int64("ns", -1, "ns nnn, send/receive for nnn seconds, then quit (default: no time limit)")
	vPtr := flag.Bool("v", false, "v, display version and quit")
	ratePtr := flag.String("rate", "", "rate nnn[X], specify rate in bps, with an optional multiplier X (K, M, or G)")
	qodePtr := flag.Bool("qode", false, "qode, quit on data error (default: go back to listening)")
	qoccPtr := flag.Bool("qocc", false, "qocc, quit on closed connection (default: go back to listening)")
	rbsPtr := flag.Int("rbs", -1, "rbs nnn, set read buffer size (for TCP) to nnn (default 1MB)")
	wbsPtr := flag.Int("wbs", -1, "rbs nnn, set write buffer size (for TCP) to nnn (default 1MB)")
	flag.Parse()

	var rate int = 0
	var success bool

	if *vPtr != false {
		fmt.Printf("goperf: Version %v \n", goperfVersion)
		return
	}

	if *ratePtr != "" {
		rate, success = convertToBps(*ratePtr)
		if !success {
			fmt.Println("Invalid rate argument", *ratePtr, rate)
			exitCode = 5
			return
		}
		fmt.Printf("rate=%v \n", rate)
	}

	if *tcpPtr == true && *udpPtr == true {
		fmt.Println("Error: cannot select both tcp and udp")
		exitCode = 5
		return
	}

	if *tcpPtr == false && *udpPtr == false {
		fmt.Println("Error: must select one of tcp or udp")
		exitCode = 5
		return
	}

	if *cPtr != "" && *sPtr != "" {
		fmt.Println("Error: cannot select both client and server")
		exitCode = 5
		return
	}

	if *cPtr == "" && *sPtr == "" {
		fmt.Println("Error: must select one of either client or server")
		exitCode = 5
		return
	}

	if *sPtr != "" {
		port, err := strconv.Atoi(*sPtr)
		if err != nil {
			fmt.Println("Error: -s value must be numeric characters only")
			exitCode = 5
			return
		}

		if port < 1024 || port > 65535 {
			fmt.Println("Error: -s value's valid range is 1024-65535")
			exitCode = 5
			return
		}
	}

	if *cPtr != "" {
		_, _, err := net.SplitHostPort(*cPtr)
		if err != nil {
			fmt.Println("Error: -c value not a valid host:port")
			exitCode = 5
			return
		}
	}

	if *nbPtr != -1 && *nsPtr != -1 {
		fmt.Printf("-nb (byte limit) and -ns (time limit) both specified, using time limit of %d seconds \n", *nsPtr)
		*nbPtr = -1
	} else if *nbPtr != -1 {
		var direction string
		if *cPtr != "" {
			direction = "send"
		} else {
			direction = "receive"
		}
		fmt.Printf("Will %s %d bytes, then quit \n", direction, *nbPtr)
	} else if *nsPtr != -1 {
		var direction string
		if *cPtr != "" {
			direction = "send"
		} else {
			direction = "receive"
		}
		fmt.Printf("Will %s for %d seconds, then quit \n", direction, *nsPtr)
	}

	if *udpPtr {
		var pps, psize int64

		if *sPtr != "" {
			exitCode = runUdpServer(*sPtr, *scrollPtr, *tsPtr, *nsPtr, *nbPtr, *qodePtr, *qoccPtr)
			return
		} else {
			// verify correct number of UDP parameters for UDP client
			if *mbpsPtr != -1 && *psizePtr != -1 && *ppsPtr != -1 ||
				*mbpsPtr == -1 && *psizePtr == -1 && *ppsPtr == -1 ||
				*mbpsPtr != -1 && *psizePtr == -1 && *ppsPtr == -1 ||
				*mbpsPtr == -1 && *psizePtr != -1 && *ppsPtr == -1 ||
				*mbpsPtr == -1 && *psizePtr == -1 && *ppsPtr != -1 {
				fmt.Println("Error: must specify 2 (and only 2) of 3 parameters (Mbps, psize, pps) for UDP")
				exitCode = 5
				return
			}

			if *ppsPtr != -1 {
				pps = *ppsPtr
			} else {
				pps = ((*mbpsPtr * 1000000) / 8) / *psizePtr
			}

			if *psizePtr != -1 {
				psize = *psizePtr
			} else {
				psize = ((*mbpsPtr * 1000000) / 8) / *ppsPtr
			}

			exitCode = runUdpClient(*cPtr, *scrollPtr, *tsPtr, pps, psize, *nsPtr, *nbPtr)
			return
		}
	} else {
		/* TCP */
		if *sPtr != "" {
			exitCode = runTcpServer(*sPtr, *scrollPtr, *tsPtr, *nsPtr, *nbPtr, *qodePtr, *qoccPtr, *rbsPtr, *wbsPtr)
			return
		} else {
			exitCode = runTcpClient(*cPtr, *scrollPtr, *tsPtr, *nsPtr, *nbPtr, rate, *rbsPtr, *wbsPtr)
			return
		}
	}
}

/******************************************************************************/
func runTcpServer(s string, scroll bool, ts bool, ns int64, nb int64, qode bool, qocc bool, rbs int, wbs int) int {

	var connClosed bool = false
	var dataError bool = false
	var ctlCRecd bool = false

	laddr, err := net.ResolveTCPAddr("tcp", "0.0.0.0:"+s)
	if err != nil {
		fmt.Println("Error resolving:", err.Error())
		return 6
	}

	ln, err := net.ListenTCP("tcp", laddr)
	if err != nil {
		fmt.Println("Error listening:", err.Error())
		return 7
	}

	defer ln.Close()

	for {
		fmt.Println("Listening on", "0.0.0.0:"+s)
		conn, err := ln.AcceptTCP()
		if err != nil {
			fmt.Println("Error accepting:", err.Error())
			return 8
		}

		/*
			fmt.Println("Connection open, getting control data...")
			buf := make([]byte, 10)
			n, err := conn.Read(buf)
			if err != nil {
				fmt.Printf( "Error reading control data, err=%v \n", err)
				return 50
			}

			fmt.Printf( "buf=%v \n", buf)
		*/
		// channel for signal notification
		sigc := make(chan os.Signal, 1)
		signal.Notify(sigc, os.Interrupt)

		go func() {
			<-sigc
			fmt.Println()
			ctlCRecd = true
		}()

		if ret := setBufferSizes(conn, rbs, wbs); ret != 0 {
			return ret
		}

		var totalBytesRcvd int64 = 0
		var nsec uint64 = 0

		var c LogData
		c.Extended = LOG_EXTEND_NO
		c.InUse = true

		go LogRates(&c, scroll, ts, true)

		buf := make([]byte, 1024)

		runtime.LockOSThread()

		var i int = 0

		for {
			var err error
			var n int

			err = conn.SetReadDeadline(time.Now().Add(time.Millisecond * 500))
			n, err = conn.Read(buf)
			//			fmt.Printf("Abc \n")
			i++
			if err == io.EOF {
				fmt.Println()
				fmt.Println("EOF detected")
				conn.Close()

				connClosed = true
			} else if err != nil {
				s := err.Error()
				fmt.Printf("err type=%T, err=%v, n=%v \n", err, err, n)
				fmt.Printf("s=%v \n", s)
				conn.Close()
				connClosed = true
			} else if err = verifyTCPRead(buf, n, totalBytesRcvd); err != nil {
				fmt.Printf("%v", err)
				conn.Close()

				dataError = true
			}

			c.Lock()
			c.NBytes += n
			nsec = c.NSec
			c.Unlock()

			totalBytesRcvd += int64(n)
			if nb != -1 && totalBytesRcvd > nb {
				fmt.Printf("\nByte limit (%d) reached, quitting, %d total bytes received \n", nb, totalBytesRcvd)
				return 20
			}

			if ns != -1 && nsec >= uint64(ns) {
				fmt.Printf("\nTime limit (%d seconds) reached, quitting \n", ns)
				return 21
			}

			if connClosed || dataError || ctlCRecd {
				c.Lock()
				c.Shutdown = true
				c.Unlock()
				for {
					time.Sleep(100 * time.Millisecond)
					c.Lock()
					if c.Shutdown == false {
						c.InUse = false
						c.Unlock()
						break
					}
					c.Unlock()
				}

				signal.Reset(os.Interrupt)

				if connClosed {
					// in here due to connection close on the other side
					if qocc {
						return 30
					}
				} else if dataError {
					// in here due to a data error
					if qode {
						return 31
					}
				} else {
					// must be in here due to ^C
					return 32
				}

				// we didn't return in one of the 'if' statements aboce, so setup
				// to start listening again
				totalBytesRcvd = 0
				connClosed = false
				dataError = false
				break
			}
		}
	}
	return 0
}

/******************************************************************************/
func verifyTCPRead(buf []byte, n int, totalBytesRcvd int64) error {

	val := byte(totalBytesRcvd % 256)

	for i := 0; i < n; i++ {
		if buf[i] != val {
			return fmt.Errorf("\nERROR: invalid value detected at byte %v: should have been %d, received %v \n       Closing connection \n",
				totalBytesRcvd+int64(i)+1,
				val,
				buf[i])
		}

		val++
	}

	return nil
}

/******************************************************************************/
func runTcpClient(hostport string, scroll bool, ts bool, ns int64, nb int64, rate int, rbs int, wbs int) int {

	var ctlCRecd bool = false

	fmt.Println("Attempting to make a TCP connection to", hostport)
	raddr, err := net.ResolveTCPAddr("tcp", hostport)
	if err != nil {
		fmt.Println(err.Error())
		return 6
	}

	conn, err := net.DialTCP("tcp", nil, raddr)
	if err != nil {
		fmt.Println(err.Error())
		return 9
	}

	if ret := setBufferSizes(conn, rbs, wbs); ret != 0 {
		return ret
	}

	var totalBytesSent int64 = 0
	var nsec uint64 = 0

	// start logger
	var c LogData
	c.Extended = LOG_EXTEND_NO
	c.InUse = true
	go LogRates(&c, scroll, ts, true)

	// make a channel for keyboard input routing
	k := make(chan rune)
	go kbInput(k)

	// channel for signal notification
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)

	go func() {
		<-sigc
		fmt.Println()
		fmt.Println("hello from here")
		ctlCRecd = true
	}()

	if rate != 0 {
		baseWriteSize := rate / 8000
		extra := (rate - baseWriteSize*8000)
		writeControl := make([]bool, 8000)

		for i := 0; i < 8000; i++ {
			writeControl[i] = false
		}

		var i int
		for i = 0; i < extra; i++ {
			writeControl[i] = true
		}

		b := make([]byte, 0)
		buf := bytes.NewBuffer(b)

		ticker := time.NewTicker(time.Millisecond)
		defer ticker.Stop()

		var writeControlIdx int = 0

		runtime.LockOSThread()

		var retCode int
		var terminating bool = false

		for {
			select {

			case <-ticker.C:

				var writeSize int

				if writeControl[writeControlIdx] {
					writeSize = baseWriteSize + 1
				} else {
					writeSize = baseWriteSize
				}

				for {
					if writeSize < buf.Len() {
						break
					}
					buf = appendBuffer(buf, 128*1024)
				}

				var n int = 0
				var err error

				if writeSize > 0 {
					bb := buf.Next(writeSize)
					conn.SetWriteDeadline(time.Now().Add(time.Millisecond))
					n, err = conn.Write(bb)

					if err != nil {
						fmt.Printf("\nError writing: %v \n", err.Error())
						conn.Close()
						retCode = 10
						terminating = true
					}
				}

				writeControlIdx++
				if writeControlIdx == 8000 {
					writeControlIdx = 0
				}

				c.Lock()

				c.NBytes += n
				nsec = c.NSec
				c.NWrites++

				dt.Lock()
				c.NDroppedTicks = dt.N
				dt.Unlock()

				c.Unlock()

				totalBytesSent += int64(n)
				if nb != -1 && totalBytesSent > nb {
					fmt.Printf("\nSend byte limit (%d) reached, quitting, sent %d bytes \n", nb, totalBytesSent)
					retCode = 20
					terminating = true
				}

				if ns != -1 && nsec >= uint64(ns) {
					fmt.Printf("\nTime limit (%d seconds) reached, quitting \n", ns)
					retCode = 21
					terminating = true
				}
			}

			if ctlCRecd {
				fmt.Printf("\nOperator interrupt, terminating \n")
				retCode = 32
				terminating = true
			}

			if terminating {
				signal.Reset(os.Interrupt)
				return retCode
			}
		}

	} else {
		// create slice to hold values to send
		buf := make([]byte, 128*1024)
		for i := 0; i < 128*1024; i++ {
			buf[i] = (byte)(i % 256)
		}

		runtime.LockOSThread()

		var retCode int
		var terminating bool = false

		for {

			var n int = 0
			var err error

			// enable this section to test data error detection code on server side
			// 	if nsec > 5 {
			// 		buf[21] = 13
			// 	}

			// for testing timeout code on server side
			// code reference: https://gist.github.com/hongster/04660a20f2498fb7b680
			conn.SetWriteDeadline(time.Now().Add(time.Millisecond))
			n, err = conn.Write(buf)
			if err != nil {
				fmt.Printf("\nError writing: %v \n", err.Error())
				conn.Close()
				retCode = 10
				terminating = true
			}

			c.Lock()
			c.NBytes += n
			nsec = c.NSec
			c.NWrites++
			c.Unlock()

			totalBytesSent += int64(n)

			if nb != -1 && totalBytesSent > nb {
				fmt.Printf("\nSend byte limit (%d) reached, quitting, sent %d bytes \n", nb, totalBytesSent)
				retCode = 20
				terminating = true
			}

			if ns != -1 && nsec >= uint64(ns) {
				fmt.Printf("\nTime limit (%d seconds) reached, quitting \n", ns)
				retCode = 21
				terminating = true
			}

			if ctlCRecd {
				fmt.Printf("\nOperator interrupt, terminating \n")
				retCode = 32
				terminating = true
			}

			if terminating {
				signal.Reset(os.Interrupt)
				return retCode
			}
		}
	}
}

/******************************************************************************/
func appendBuffer(b *bytes.Buffer, n int) *bytes.Buffer {

	s := b.Next(b.Len())
	//	fmt.Printf("AAA: type=%T, s=%v \n", s, s)

	s2 := make([]byte, n)
	for i := 0; i < n; i++ {
		s2[i] = byte(i % 256)
	}
	s = append(s, s2...)
	//	fmt.Printf("BBB: type=%T, s=%v \n", s, s)
	b2 := bytes.NewBuffer(s)

	//	fmt.Printf("CCC: cap=%v, len=%v, b2=%v \n", b2.Cap(), b2.Len(), b2)
	return b2
}

/******************************************************************************/
func tickSender(t chan bool) {
	runtime.LockOSThread()

	ticker := time.NewTicker(time.Millisecond * 10)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			select {
			case t <- true:
			default:
				dt.Lock()
				dt.N++
				dt.Unlock()
			}
		}
	}
}

/******************************************************************************/
func setBufferSizes(conn *net.TCPConn, rbs int, wbs int) int {

	fd, _ := conn.File()

	valRcv, _ := syscall.GetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
	valSnd, _ := syscall.GetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
	fmt.Printf("Startup socket read buffer size=%v \n", valRcv)
	fmt.Printf("Startup socket send buffer size=%v \n", valSnd)

	if rbs != -1 {
		err := conn.SetReadBuffer(rbs)

		if err != nil {
			fmt.Printf("Error: attempting to set read buffer size to %v, function SetReadBuffer() failed, error=%v \n",
				rbs,
				err.Error())
			return 10
		}
		valRcv, _ = syscall.GetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_RCVBUF)
		fmt.Printf("Called SetReadBuffer with value=%v, readback shows value=%v \n", rbs, valRcv)
	}

	if wbs != -1 {
		err := conn.SetWriteBuffer(wbs)

		if err != nil {
			fmt.Printf("Error: attempting to set write buffer size to %v, function SetWriteBuffer() failed, error=%v \n",
				wbs,
				err.Error())
			return 10
		}
		valSnd, _ = syscall.GetsockoptInt(int(fd.Fd()), syscall.SOL_SOCKET, syscall.SO_SNDBUF)
		fmt.Printf("Called SetWriteBuffer with value=%v, readback shows value=%v \n", wbs, valSnd)
	}

	fmt.Printf("Using %v for read buffer size, %v for write buffer size \n", valRcv, valSnd)

	return 0
}

var udpSeqNumberNextSeqNumSend uint64 = 1

var udpSeqNumRecd uint64 = 0
var udpHighestSeqNumRecd uint64 = 0
var udpPrevSeqNumRecd uint64 = 0
var udp2BackSeqNumRecd uint64 = 0

var udpSenderNano uint64 = 0
var udpPrevSenderNano uint64 = 0

var udpTimeRecdNano uint64 = 0
var udpPrevTimeRecdNano uint64 = 0
var udp2BackTimeRecdNano uint64 = 0

var udpNPacketsRecd uint64 = 0
var udpNPacketsDropped uint64 = 0
var udpNPacketsOutOfOrder uint64 = 0

var udpSumSinceStartJitter uint64 = 0
var sumSinceStartJitterNSamples uint64 = 0

var udpSumLast10Jitter [10]uint64
var udpSumLast10JitterNSamples [10]uint64

/******************************************************************************/
func runUdpServer(s string, scroll bool, ts bool, ns int64, nb int64, qode bool, qocc bool) int {

	/* Let's prepare a address at any address at port s */
	ServerAddr, err := net.ResolveUDPAddr("udp", "0.0.0.0"+":"+s)
	if err != nil {
		fmt.Println("Error: ", err)
		return 6
	}

	/* Now listen at selected port */
	ServerConn, err := net.ListenUDP("udp", ServerAddr)
	if err != nil {
		fmt.Println("Error: ", err)
		return 40
	}

	defer ServerConn.Close()

	var totalBytesRcvd int64 = 0
	var nsec uint64 = 0

	udpPrevSenderNano = udpSenderNano
	udpPrevTimeRecdNano = udpTimeRecdNano

	// start logger
	var c LogData
	c.Extended = LOG_EXTEND_UDP_SERVER
	c.InUse = false
	go LogRates(&c, scroll, ts, true)

	buf := make([]byte, 1024)

	runtime.LockOSThread()

	for {
		var jitter uint64

		n, _, err := ServerConn.ReadFromUDP(buf)
		udpTimeRecdNano = getTimeNanoseconds()

		if err != nil {
			fmt.Println("Error: ", err)
			return 41
		}

		udpSenderNano = binary.BigEndian.Uint64(buf[0:8])
		udpSeqNumRecd = binary.BigEndian.Uint64(buf[8:16])

		/*
			fmt.Fprintf(os.Stdout, "%s: senderNano delta=%v (%7.3f ms), senderSeqNum=%v, timeRecdNano delta=%v (%7.3f ms) \n",
				getFunc(),
				udpSenderNano-udpPrevSenderNano,
				float64(udpSenderNano-udpPrevSenderNano)/1000000.,
				udpSeqNumRecd,
				udpTimeRecdNano-udpPrevTimeRecdNano,
				float64(udpTimeRecdNano-udpPrevTimeRecdNano)/1000000.)
		*/

		// this packet and the previous 2 all in order, and
		// they are the last 3 received?
		if udp2BackTimeRecdNano != 0 &&
			udpSeqNumRecd == udpHighestSeqNumRecd+1 &&
			udpSeqNumRecd == udpPrevSeqNumRecd+1 &&
			udpPrevSeqNumRecd == udp2BackSeqNumRecd+1 {

			// calculate jitter: abs( (t2-t1) - (t3-t2)) = abs(t2*2 - t1 - t3) = abs( t2*2 - (t1 + t3))
			t2x2 := udpPrevTimeRecdNano << 1
			t3plusT1 := udp2BackTimeRecdNano + udpTimeRecdNano
			if (t2x2) > t3plusT1 {
				jitter = t2x2 - t3plusT1
				/*				fmt.Fprintf(os.Stdout, "jitter A: udp2TRN=%v, udpPTRN=%v, udpTRN=%v, jitter=%v \n",
								udp2BackTimeRecdNano,
								udpPrevTimeRecdNano,
								udpTimeRecdNano,
								jitter)
				*/
			} else {
				jitter = t3plusT1 - t2x2
				//				fmt.Fprintf(os.Stdout, "jitter B: %v \n", jitter)
			}

			/*
				if jitter > 2000000 {
					fmt.Fprintf(os.Stderr, "%s: jitter=%v (very high), uPTRN=%v, u2BTRN=%v, uTRN=%v \n",
						getFunc(),
						jitter,
						udpPrevTimeRecdNano,
						udp2BackTimeRecdNano,
						udpTimeRecdNano)
				}
			*/
		} else {

			// highest seq number so far?
			if udpSeqNumRecd > udpHighestSeqNumRecd {
				// dropped packet(s)?
				if udpSeqNumRecd != udpHighestSeqNumRecd+1 {
					udpNPacketsDropped += (udpSeqNumRecd - udpHighestSeqNumRecd - 1)
				}
			} else {
				// must be out-of-order

				// decrement dropped packets counter, since it must have
				// been incremented when seq number gap was first detected
				udpNPacketsDropped--
				udpNPacketsOutOfOrder++
			}

			jitter = 0
		}

		// update all of the seq number tracker variables
		udp2BackSeqNumRecd = udpPrevSeqNumRecd
		udpPrevSeqNumRecd = udpSeqNumRecd

		udp2BackTimeRecdNano = udpPrevTimeRecdNano
		udpPrevTimeRecdNano = udpTimeRecdNano

		if udpSeqNumRecd > udpHighestSeqNumRecd {
			udpHighestSeqNumRecd = udpSeqNumRecd
		}

		// count the packet
		udpNPacketsRecd++

		// update the data rate counter
		c.Lock()
		c.NBytes += n
		c.JitterSum += jitter
		c.JitterN++
		c.NPktsRecd = udpNPacketsRecd
		c.RcvdSeqNumber = udpHighestSeqNumRecd
		c.NPktsDropped = udpNPacketsDropped
		c.NPktsOutOfOrder = udpNPacketsOutOfOrder
		c.InUse = true
		nsec = c.NSec
		c.Unlock()

		totalBytesRcvd += int64(n)
		if nb != -1 && totalBytesRcvd >= nb {
			fmt.Printf("\nByte limit (%d) reached, quitting, %d total bytes received \n", nb, totalBytesRcvd)
			return 20
		}

		if ns != -1 && nsec >= uint64(ns) {
			fmt.Printf("\nTime limit (%d seconds) reached, quitting \n", ns)
			return 21
		}
	}
}

/******************************************************************************/
func runUdpClient(hostport string, scroll bool, ts bool, pps, psize int64, ns int64, nb int64) int {

	ServerAddr, err := net.ResolveUDPAddr("udp", hostport)
	if err != nil {
		fmt.Println("Error: ", err)
		return 6
	}

	LocalAddr := GetOutboundIP()

	var udpAddr = net.UDPAddr{IP: LocalAddr}
	Conn, err := net.DialUDP("udp", &udpAddr, ServerAddr)
	if err != nil {
		fmt.Println("Error:", err)
		return 42
	}

	defer Conn.Close()

	var totalBytesSent int64 = 0
	var nsec uint64 = 0

	// start logger
	var c LogData
	c.Extended = LOG_EXTEND_UDP_CLIENT
	c.InUse = true
	go LogRates(&c, scroll, ts, true)

	// create payload slice
	pl := make([]byte, psize*2+16)

	// create slice for nanosecond counter
	nanoSlice := pl[:8]

	// create slice for sequence number
	snSlice := pl[8:16]

	var i int64
	for i = 0; i < psize*2-16; i++ {
		pl[i+16] = (byte)(i % 256)
	}

	// get a ticker to time the outgoing packets
	interval := time.NewTicker(time.Nanosecond * (time.Duration)(1000000000/pps))
	defer interval.Stop()

	// make a channel for keyboard input routing
	k := make(chan rune)
	go kbInput(k)

	var saveNum uint64 = 0
	var triggerOOO bool

	// channel for signal notification
	sigc := make(chan os.Signal, 1)
	signal.Notify(sigc, os.Interrupt)

	runtime.LockOSThread()

	for {
		select {
		case <-interval.C:
			nano := getTimeNanoseconds()
			binary.BigEndian.PutUint64(nanoSlice, nano)
			if triggerOOO == true {
				saveNum = udpSeqNumberNextSeqNumSend
				udpSeqNumberNextSeqNumSend++
				binary.BigEndian.PutUint64(snSlice, udpSeqNumberNextSeqNumSend)
				udpSeqNumberNextSeqNumSend++
				triggerOOO = false
			} else if saveNum != 0 {
				binary.BigEndian.PutUint64(snSlice, saveNum)
				saveNum = 0
			} else {
				binary.BigEndian.PutUint64(snSlice, udpSeqNumberNextSeqNumSend)
				udpSeqNumberNextSeqNumSend++
			}

			runtime.Gosched()
			n, err := Conn.Write(pl[0 : psize+16])
			if err != nil {
				fmt.Printf("\nError writing: %v \n", err.Error())
				return 43
			}

			c.Lock()
			c.NBytes += n
			c.NPktsSent++
			c.SentSeqNumber = udpSeqNumberNextSeqNumSend - 1
			nsec = c.NSec
			c.Unlock()

			totalBytesSent += int64(n)
			if nb != -1 && totalBytesSent > nb {
				fmt.Printf("\nSend byte limit (%d) reached, quitting, sent %d bytes \n", nb, totalBytesSent)
				return 20
			}

			if ns != -1 && nsec >= uint64(ns) {
				fmt.Printf("\nTime limit (%d seconds) reached, quitting \n", ns)
				return 21
			}

			break

		case cmd, _ := <-k:
			if cmd == 's' {
				udpSeqNumberNextSeqNumSend++
			} else if cmd == 'o' {
				triggerOOO = true
			}

			break

		case <-sigc:
			// give logger time to run one last time
			time.Sleep(2 * time.Second)
			fmt.Println()
			return 32

		}
	}
}

/******************************************************************************/
func kbInput(ky chan rune) {
	reader := bufio.NewReader(os.Stdin)
	for {
		char, _, err := reader.ReadRune()

		if err != nil {
			fmt.Println(err)
		} else {
			ky <- char
		}
	}
}

/******************************************************************************/
func convertToBps(inputRate string) (rate int, success bool) {
	var multiplier int64 = 1

	if strings.HasSuffix(inputRate, "K") {
		multiplier = 1000
		inputRate = strings.TrimSuffix(inputRate, "K")
	}

	if strings.HasSuffix(inputRate, "M") {
		multiplier = 1000000
		inputRate = strings.TrimSuffix(inputRate, "M")
	}

	if strings.HasSuffix(inputRate, "G") {
		multiplier = 1000000000
		inputRate = strings.TrimSuffix(inputRate, "G")
	}

	var s int64
	var err error
	if s, err = strconv.ParseInt(inputRate, 10, 32); err != nil {
		return 0, false
	}

	return int(s * multiplier), true
}

/******************************************************************************/
func GetOutboundIP() net.IP {
	// Get preferred outbound ip of this machine

	// IP address in the following statement is irrevelant, as UDP doesn't
	// make a "connection"
	conn, err := net.Dial("udp", "8.8.8.8:80")
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)

	return localAddr.IP
}

/******************************************************************************/
func getFunc() string {
	function, _, _, _ := runtime.Caller(1)
	return runtime.FuncForPC(function).Name()
}

/******************************************************************************/
func isLittleEndian() bool {
	var i int32 = 0x01020304
	u := unsafe.Pointer(&i)
	pb := (*byte)(u)
	b := *pb
	return (b == 0x04)
}

/******************************************************************************/
func getTimeNanoseconds() uint64 {
	t := time.Now()
	return uint64(t.UnixNano())
}

/******************************************************************************/
/******************************************************************************/
/* logRate functions **********************************************************/
/******************************************************************************/
/******************************************************************************/
/******************************************************************************/
func LogRates(c *LogData, scroll bool, use_ts bool, use_stdout bool) {

	var localLogData LogData
	needHeader := true

	var nsec uint64

	var r historyTrack // rate
	var j historyTrack // jitter

	var line_term string
	var linesToHdrRefresh int
	if scroll == true {
		line_term = "\n"
		linesToHdrRefresh = N_LINES_HDR_REFRESH
	} else {
		line_term = "\r"
	}

	var output io.Writer
	if use_stdout {
		output = os.Stdout
	} else {
		output = os.Stderr
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {

		case <-ticker.C:

			// grab the values from the generating routine
			c.Lock()

			if !c.InUse {
				c.Unlock()
				continue
			}

			localLogData = *c

			nsec++

			c.NBytes = 0
			c.JitterSum = 0
			c.JitterN = 0
			c.NWrites = 0
			c.NSec = nsec

			c.Unlock()

			t2 := time.Now()
			t2 = t2.UTC()
			ts := t2.Format("[01-02-2006][15:04:05.000000]")

			updateHistoryTrack(&r, (uint64)(localLogData.NBytes*8))

			var jitterThisPeriod uint64
			if localLogData.JitterN == 0 {
				jitterThisPeriod = 0
			} else {
				jitterThisPeriod = localLogData.JitterSum / localLogData.JitterN
			}
			updateHistoryTrack(&j, jitterThisPeriod)

			if localLogData.Shutdown {
				fmt.Println("Final statistics:")
				displayHeader(output, use_ts, localLogData.Extended)
				writeDataLine(output, use_ts, ts, nsec, r, j, localLogData, line_term, scroll, &linesToHdrRefresh)
				fmt.Println()
				c.Lock()
				c.NSec = 0
				c.Shutdown = false
				c.Unlock()
				return
			} else {
				if needHeader {
					displayHeader(output, use_ts, localLogData.Extended)
					needHeader = false
				}

				writeDataLine(output, use_ts, ts, nsec, r, j, localLogData, line_term, scroll, &linesToHdrRefresh)

			}
		}
	}
}

/******************************************************************************/
func writeDataLine(output io.Writer,
	use_ts bool,
	ts string,
	nsec uint64,
	r historyTrack,
	j historyTrack,
	localLogData LogData,
	line_term string,
	scroll bool,
	linesToHdrRefresh *int) {

	if use_ts {
		fmt.Fprintf(output, "%s", ts)
	}

	/* # seconds */
	fmt.Fprintf(output, "[ %7d ]", nsec)

	/* last second */
	fmt.Fprintf(output, "[ %9s ]", formatRate(r.current))

	/* last 10 seconds */
	fmt.Fprintf(output, "[ %9s ]", formatRate(r.totalLast10/(uint64)(r.nFilled)))

	/* total */
	fmt.Fprintf(output, "[ %9s ]", formatRate(r.totalEver/nsec))

	//	fmt.Fprintf(output, "{ %v }{ %v }", localLogData.NWrites, localLogData.NDroppedTicks-prevNDroppedTicks)
	prevNDroppedTicks = localLogData.NDroppedTicks

	if localLogData.Extended == LOG_EXTEND_UDP_SERVER {
		fmt.Fprintf(output, "[ %8.3f ][ %9.3f ][ %11.3f ][ %7d ][ %7d ][ %7d ]",
			float64(j.current)/1000000.,
			float64(j.totalLast10/uint64(j.nFilled))/1000000.,
			float64(j.totalEver/nsec)/1000000.,
			localLogData.NPktsRecd,
			localLogData.NPktsDropped,
			localLogData.NPktsOutOfOrder)
	} else if localLogData.Extended == LOG_EXTEND_UDP_CLIENT {
		fmt.Fprintf(output, "[ %14d ]", localLogData.NPktsSent)
	}

	fmt.Fprintf(output, line_term)
	if scroll {
		*linesToHdrRefresh--
		if *linesToHdrRefresh == 0 {
			displayHeader(output, use_ts, localLogData.Extended)
			*linesToHdrRefresh = N_LINES_HDR_REFRESH
		}
	}
}

/******************************************************************************/
func formatRate(bps uint64) string {

	var label string
	var r float64
	var ret string
	var bpsf float64 = (float64)(bps)

	switch {
	case bps > 1000000000:
		label = "G"
		r = bpsf / 1000000000.

	case bps > 1000000:
		label = "M"
		r = bpsf / 1000000.

	case bps > 1000:
		label = "K"
		r = bpsf / 1000.

	default:
		label = " "
		r = bpsf
	}

	ret = fmt.Sprintf("%5.3f%s", r, label)
	return ret
}

/******************************************************************************/
func updateHistoryTrack(r *historyTrack, current uint64) {

	r.current = current

	r.totalEver += r.current
	r.last10[r.nextInLast10] = r.current

	r.nextInLast10++
	if r.nextInLast10 == 10 {
		r.nextInLast10 = 0
	}

	if r.nFilled < 10 {
		r.nFilled++
	}

	/* last 10 seconds */
	r.totalLast10 = 0
	for i := 0; i < r.nFilled; i++ {
		r.totalLast10 += r.last10[i]
	}
}

/******************************************************************************/
func displayHeader(output io.Writer, use_ts bool, extended int) {
	if use_ts {
		fmt.Fprintf(output, "                             ")
	}
	fmt.Fprintf(output, "           [ <-------- Data Rate (bps) --------> ]")

	if extended == LOG_EXTEND_UDP_SERVER {
		fmt.Fprintf(output, "[ <---------- Jitter (ms) -----------> ][ <---- Number of Packets ----> ] \n")
	} else {
		fmt.Fprintf(output, "\n")
	}

	if use_ts {
		fmt.Fprintf(output, "[                  Timestamp]")
	}
	fmt.Fprintf(output, "[  # Secs ][ Lst Secnd ][  Lst 10 S ][ Snce Strt ]")

	if extended == LOG_EXTEND_UDP_SERVER {
		fmt.Fprintf(output, "[ Last Sec ][ Last 10 S ][ Since Start ][ Receivd ][ Dropped ][ OutOrdr ] \n")
	} else if extended == LOG_EXTEND_UDP_CLIENT {
		fmt.Fprintf(output, "[ # Packets Sent ] \n")
	} else {
		fmt.Fprintf(output, " \n")
	}
}

/******************************************************************************/
/******************************************************************************/
/* end logRate functions ******************************************************/
/******************************************************************************/
/******************************************************************************/
