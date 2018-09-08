# Description

## goperf

An application, in the command-line style of iperf, written in Go, for testing the setup of TCP and UDP data connections, monitoring and reporting of a connection's data rate, and verification that the received data matches the sent.

For TCP connections, the data rate can be restrained by specifying a data rate at which the client transmits (with the <tt>-rate</tt> flag. The default is to transmit as fast as it can.

For UDP connections, two (and only rwo) of the following three parameters are specified to control tjhe data rate:

* packet size (<tt>-psize</tt> flag)
* packets per second (<tt>-pps</tt> flag)
* data rate Mbps (<tt>-Mbps</tt> flag

When testing UDP, *goperf* also calculates and displays jitter, as well as dropped and out of order packets.

# Installation

go get github.com/jspiegler/goperf/goperf

# Usage

*goperf* is controlled via command line flags, and generates its output to the console from which it was run. All available flags are described below:

| Flag       | Parameter Type | Description                                                                             |
|------------|----------------|-----------------------------------------------------------------------------------------|
| -Mbps      | integer        | -Mbps nnn: for UDP connections, specify udp rate in megabits per second                 |
| -c         | string         | -c host:port: run as client, making connection to IP address *host*, port number *port* |
| -nb        | integer        | -nb nnn: send/receive *nnn* bytes, then quit (default: no byte limit)                   |
| -ns        | integer        | -ns nnn: send/receive for *nnn* seconds, then quit (default: no time limit)             |
| -pps       | integer        | -pps nnn: for UDP connections, send *nnn* packets per second                            |
| -psize     | integer        | -psize nnn: for UDP, send *nnn* bytes per packet (+ IP/UDP headers)                     |
| -qocc      | boolean        | -qocc: for server operation, quit on closed connection (default: go back to listening)  |
| -qode      | boolean        | -qode: for server operation, quit on data error (default: go back to listening)         |
| -rate      | string         | -rate nnn[X]: specify rate in bps, with an optional multiplier X (K, M, or G)           |
| -s         | string         | -s N, for server operation, listen on port *N* (all interfaces)                         |
| -scroll    | boolean        | -scroll: make output scroll (default: no scroll)                                        |
| -tcp       | boolean        | -tcp: use TCP                                                                           |
| -ts        | boolean        | -ts: display timestamp on each line of output                                           |
| -udp       | booelan        | -udp: use UDP                                                                           |
| -v         | boolean        | -v: display version and quit                                                            |

# Examples

Examples assume 2 machines with IP addresses 10.0.0.1 and 10.0.0.2

* TCP example 1:

| 10.0.0.1 Command | 10.0.0.2 Command | Notes |
|:---------------- |:---------------- |:----- |
|<tt>./goperf -tcp -s 8800</tt>|      | Run server on 10.0.0.1, listening on port 8800|
|                  |<tt>./goperf -tcp -c 10.0.0.1:8800| Run client on 10.0.0.2, connecting to 10.0.0.1 on port 8800|

* TCP example 2:

| 10.0.0.1 Command | 10.0.0.2 Command | Notes |
|:---------------- |:---------------- |:----- |
|<tt>./goperf -tcp -s 8800 -scroll -ts</tt>|      | Run server on 10.0.0.1, listening on port 8800, scroll the output and disply timestamp|
|                  |<tt>./goperf -tcp -c 10.0.0.1:8800| Run client on 10.0.0.2, connecting to 10.0.0.1 on port 8800|

* UDP example 1:

| 10.0.0.1 Command | 10.0.0.2 Command | Notes |
|:---------------- |:---------------- |:----- |
|<tt>./goperf -udp -s 8810</tt>|      | Run server on 10.0.0.1, listening on port 8810|
|                  |<tt>./goperf -udp -c 10.0.0.1:8810 -pps 100 -psize 1000|Run client on 10.0.0.2, connection to 10.0.0.1 om port 8810|

* UDP example 2:

| 10.0.0.1 Command | 10.0.0.2 Command | Notes |
|:---------------- |:---------------- |:----- |
|<tt>./goperf -udp -s 8810</tt>|      | Run server on 10.0.0.1, listening on port 8810|
|                  |<tt>./goperf -udp -c 10.0.0.1:8810 -pps 100 -psize 1000 -nb 10000000|Run client on 10.0.0.2, connection to 10.0.0.1 om port 8810, send 10M bytes then quit|

