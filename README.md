# Description

## goperf

An application, in the command-line style of iperf, written in Go, for testing the setup of TCP and UDP data connections, monitoring and reporting of a connection's data rate, and verification that the received data matches the sent.

# Installation

go get github.com/jspiegler/goperf/goperf

# Usage

goperf is controlled via command line flags, and generates its output to the console from which it was run. All available flags are described below:

| Flag       | Parameter Type | Description                                                                             |
| ---------- |----------------|-----------------------------------------------------------------------------------------|
| -Mbps      | integer        | -Mbps nnn: for UDP connections, specify udp rate in megabits per second                 |
| -c         | string         | -c host:port: run as client, making connection to IP address *host*, port number *port* |
| -nb        | integer        | -nb nnn: send/receive *nnn* bytes, then quit (default: no byte limit)                   |
| -ns        | integer        | -ns nnn: send/receive for *nnn* seconds, then quit (default: no time limit)             |
| -pps       | integer        | -pps nnn: for UDP connections, send *nnn* packets per second                            |
| -psize     | integer        | -psize nnn: for UDP, send *nnn* bytes per packet (+ IP/UDP headers)                     |
| -qocc      | boolean        | -qocc: for server operation, quit on closed connection (default: go back to listening)  |
| -qode      | boolean        | -qode: for server operation, quit on data error (default: go back to listening)         |
| -rate      | string         | -rate nnn[X]: specify rate in bps, with an optional multiplier X (K, M, or G)           |
| -rbs  (*)  | integer        | -rbs nnn: for TCP connections, set read buffer size to *nnn* (default 1MB)              |
| -s         | string         | -s N, for server operation, listen on port *N* (all interfaces)                         |
| -scroll    | boolean        | -scroll: make output scroll (default: no scroll)                                        |
| -tcp       | boolean        | -tcp: use TCP                                                                           |
| -ts        | boolean        | -ts: display timestamp on each line of output                                           |
| -udp       | booelan        | -udp: use UDP                                                                           |
| -v         | boolean        | -v: display version and quit                                                            |
| -wbs  (*)  | integer        | -wbs nnn: for TCP connections, set write buffer size to *nnn* (default 1MB)             |

(*) experimental, not recommended for production use

# Examples

Examples assume 2 machines with IP addresses 10.0.0.1 and 10.0.0.2

TCP connection from 10.0.0.1 to 10.0.0.2, unlimited data rate:

On 10.0.0.1: <tt>./goperf -tcp -c10.0.0.2:8888 </tt>

On 10.0.0.2: <tt>./goperf -tcp -c10.0.0.2:8888 </TT>
