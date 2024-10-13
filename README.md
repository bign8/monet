# Monet - Monitor Network

A small CLI to monitor network traffic to an external host.

## Context

Currently, I Work From Home (WFH) and as such are on many video calls.
I noticed that my network connection was not always stable and wanted to monitor it.
Usually, I have a constant ping running to my favorite DNS provider.
But, this requires actively monitoring the ping output to determine if a response hasn't come back.
While this process works, I've found it quite distracting to 1) recognize I might be lagging, 2) switching context to a terminal to monitor the ping, and 3) waiting for a second or so of no output to recognize I'm probably lagging.

So, my goal is to write a small CLI that can either wrap `ping` or do something similar and report back to me if a few pings have been missed.
This way, I can keep my focus on my work and only be alerted if something is wrong.

## Requirements

- [ ] The CLI should be able to monitor a network connection to an external host.
- [ ] The CLI should be able to alert the user if a few pings have been missed.
- [ ] IDK, AI wrote these
- [ ] Single binary distribution (nobody likes managing dependencies).

## Notes

### Charting Libraries

- [`ntcharts`](github.com/NimbleMarkets/ntcharts) - had way too many options and was a bit overwhelming
- [`asciigraph`](github.com/guptarohit/asciigraph) - simple enough to get things started

### Errors

1. Left running a long time and saw a bunch of `on-send-err: &probing.Packet{Rtt:0, IPAddr:(*net.IPAddr)(0xc0000a4de0), Addr:"2600:6c66:0:4::6:c", Nbytes:32, Seq:16777, TTL:0, ID:2383}; write udp [::]:181->[2600:6c66:0:4::6:c]:0: sendto: network is unreachable` errors
2. Left running a long time and came back to graph with crazy axis values (really small IIRC, like huge negative numbers).
    - reproduce by pinging a docker container, kill container, restore container... shows up occasionally in a few seconds.
    - ![garbage](big-number-issue-deviations-way-off.png)
    - avg is fine, sd is way off when printing.  -9223372036854.775808 is the number that is being printed.
    - number is most negative float you can have https://stackoverflow.com/a/56989911
    - Probably want to check and manage our own ping data + handle this timeout cleaner

### Ideas

- [ ] Replace asciigraph with home rolled solution to be able to "XXX" out a column where we are expecting a response but didn't get one.
