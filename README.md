# Description

## goperf

An application, in the command-line style of iperf, written in Go, for testing the setup of TCP and UDP data connections, monitoring and reporting of a connection's data rate, and verification that the received data matches the sent.

# Installation

go get github.com/jspiegler/goperf/goperf

# Usage

goperf is controlled via command line flags, and generates its output to the console from which it was run. All available flags are described below:

| Flag       | Parameter Type | Description  |
| ---------- |----------------|--------------|
| -Mbps      | integer        | -Mpbs nnn: for UDP connections, specify udp rate in megabits per second |
| -c         | string         | -c host:port: run as client, making connection to IP address *host*, port number *port* |
| -nb        | int            | -nb nnn: send/receive *nnn* bytes, then quit (default: no byte limit) |
| -ns        | int            | -ns nnn: send/receive for *nnn* seconds, then quit (default: no time limit) |
| -pps       | int            | -pps nnn: for UDP connections, send *nnn* packets per second |
| -psize     | int            | -psize nnn: for UDP, send *nnn* bytes per packet (+ IP/UDP headers) |
| -qocc      | boolean        | -qocc: for server operation, quit on closed connection (default: go back to listening) |


  -qode
    	qode, quit on data error (default: go back to listening)
  -rate string
    	rate nnn[X], specify rate in bps, with an optional multiplier X (K, M, or G)
  -rbs int
    	rbs nnn, set read buffer size (for TCP) to nnn (default 1MB) (default -1)
  -s string
    	s N, server, listen on port N (all interfaces)
  -scroll
    	makes output scroll
  -tcp
    	tcp
  -ts
    	print timestamp
  -udp
    	udp
  -v	v, display version and quit
  -wbs int
    	rbs nnn, set write buffer size (for TCP) to nnn (default 1MB) (default -1)