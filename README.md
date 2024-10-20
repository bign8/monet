# Monet - Monitor Network

A small CLI to monitor network traffic to an external host.

## Context

Currently, I Work From Home (WFH) and as such are on many video calls.
I noticed that my network connection was not always stable and wanted to monitor it.
Usually, I have a constant ping running to my favorite DNS provider.
But, this requires actively monitoring the ping output to determine if a response hasn't come back.
While this process works, I've found it quite distracting to:

1. recognize I might be lagging,
1. switching context to a terminal to monitor the ping, and
1. waiting for a second or so of no output to recognize I'm probably lagging.

So, my goal is to write a small CLI that can either wrap `ping` or do something similar and report back to me if a few pings have been missed.
This way, I can keep my focus on my work and only be alerted if something is wrong.

## Notes

### Charting Libraries

- [`ntcharts`](github.com/NimbleMarkets/ntcharts) - had way too many options and was a bit overwhelming
- [`asciigraph`](github.com/guptarohit/asciigraph) - simple enough to get things started

### Errors

1. Left running a long time and saw a bunch of `on-send-err: &probing.Packet{Rtt:0, IPAddr:(*net.IPAddr)(0xc0000a4de0), Addr:"2600:6c66:0:4::6:c", Nbytes:32, Seq:16777, TTL:0, ID:2383}; write udp [::]:181->[2600:6c66:0:4::6:c]:0: sendto: network is unreachable` errors
2. Left running a long time and came back to graph with crazy axis values (really small IIRC, like huge negative numbers).
    - reproduce by pinging a docker container, kill container, restore container... shows up occasionally in a few seconds.
    - avg is fine, sd is way off when printing.  -9223372036854.775808 is the number that is being printed.
    - number is most negative float you can have https://stackoverflow.com/a/56989911
    - Probably want to check and manage our own ping data + handle this timeout cleaner
3. After correcting ^^ by keeping track of stats myself (allbeit very simply), I noticed some other errors
    - graph disappears if all values are NANs (happens when there is a long outage)
    - we get a ton of `recv: id: 6223; seq: 752; not found` errors as we start getting packets outside our plotted window
    - after a few data-points and/or out of order packets, the statistics are way off from probing
    ```
    2024-10-15 21:39:08.368: recv: id: 6223; seq: 754; negative stddev: -2562047h47m16.854775808s
    2024-10-15 21:39:08.368: count: 539, pmean: 86.369ms, mean: 108.090ms, rtt: 11793.686ms, delta: 11707.317ms, delta2: 11685.596ms, m.dem2: 545495962.867ms
    2024-10-15 21:39:08.369: unhandled message: tea.sequenceMsg(tea.sequenceMsg{(tea.Cmd)(0x5c3300), (tea.Cmd)(0x5c3300), (tea.Cmd)(0x5363a0)})
                                            width: 119, buffer: 6, maxPoints: 113
    via-lib: avg: 6148.325ms, sd: -9223372036854.775ms, 1sd: -9223372030706.451ms, 2sd: -18446744067561.227ms, 3sd: -276701
                non-lib: avg: 108.090ms, sd: 1006.008ms, 1sd: 1114.097ms, 2sd: 2120.105ms, 3sd: 3126.113ms
              ```

### Ideas

- [i] Replace asciigraph with home rolled solution to be able to "XXX" out a column where we are expecting a response but didn't get one.
- [x] Slow down to a "reasonable" rate once the screen is filled with data.
- [x] Show a warning if we haven't seen a response or two in an expected time window.
- [x] Use a different intervals to make more human sense: 50ms, 100ms 250ms 500ms 1s
- [ ] Add a screen to search/choose from a known list of hosts to monitor.
- [?] Look into charm-bracelet's Tape library for testing + demo recording
- [ ] Look into not using a ping library to implement the ping functionality
- [i] Look into non-charm-bracelet UI library to reduce dependencies (low priority)
- [x] Include a histogram of the ping times
- [ ] Show p90, p95, p99, p995, p999, p9995 latencies
- [ ] Keep a "window" for ~1000 pings and compute more "recent" statistics
- [!] Fix negative standard deviation issue


Key:

- `[ ]` - Not Started
- `[x]` - Done
- `[~]` - In Progress
- `[?]` - Not Sure
- `[!]` - Problem
- `[i]` - Icebox
