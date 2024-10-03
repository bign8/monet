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
