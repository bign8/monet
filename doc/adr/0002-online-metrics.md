# 2. Online Metrics

Date: 2024-12-02

## Status

Accepted

## Context

After running `monet` for a while, I noticed that the statistics produced by the `github.com/prometheus-community/pro-bing` library (and my mirrored implementation) were not always accurate.
When I left a ping running throughout the night, I would come back to standard-deviations in the negative-billions.
Clearly, this isn't correct.

Both implementations use [Welford's online method for stdev](https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance#Welford's_online_algorithm) which has been well studied and should be relatively stable/correct.
Roughly speaking, this algorithm allows keeping a running standard-deviation of a set of numbers without needing to store all the numbers.
This is useful for keeping memory usage low and is a common technique in monitoring systems.
This algorithm keeps track of 3 numbers on the fly:

1. `count` - the number of values we've seen
1. `mean` - the running average of the values
1. `m2` - the sum of the squares of the "residuals" (the difference between the value and the mean)

Then the standard deviation is calculated as `sqrt(m2 / count)` and the mean is kept around for computing the residuals.

Implementations also don't use a single residual, they compute 1 residual BEFORE accounting for the new value, and once after resulting in the following update logic:

```go
count++
delta1 := new_value - mean
mean += delta1 / count
delta2 := new_value - mean
m2 += delta1 * delta2
```

Looking at that last statement, we can start to find the crux of the issue.
If `delta1` and `delta2` are a `time.Duration`, then that multiplication can overflow the `int64` that a `time.Duration` is stored in.
But, clearly, that shouldn't be possible in the "Real world".

Well, it turns out that time.Duration stores nanoseconds, which is a small unit of time.
So, what deltas could cause this overflow?
Let's reverse this equation to find out:

```go
time.Duration(math.Sqrt(float64(math.MaxInt64))) // 3s
```

That's right, a single data-point with a delta of more than a 3 second residual will overflow the `int64` that `time.Duration` is stored in.
Resulting in a very large negative number being persisted in `m2`, kinda ruining the statistics from that point on.

Overall, that isn't really awesome!

## Decision

~~Don't use time.Duration to keep track of residuals and averages, instead use another unit of time and store those in a int64 (like time.Duration does).~~

1. ~~Micro-seconds (1e-6) would allow residuals up to 1m35s~~
1. ~~1/10th of a milliseconds (1e-4) would allow residuals up to 16m~~

~~For our use case, a delay of a minute will affect any broadcast, so that'll be sufficient.~~

Looks like the community has already fixed this :facepalm: (by using a float64 instead of a time.Duration)
https://github.com/prometheus-community/pro-bing/pull/130

## Consequences

1. ~~Statistics should be more consistent if my ISP has a hiccup (which is a common occurrence in my area)~~
1. ~~It'd be nice to let the `pro-bing` folks know about this issue, but I'm not sure if they'd be interested in changing the library (and frankly task coordination is my day-job, not what I want to do in my free time).~~
1. ~~We'll want to exclude any data-points that could cause this overflow with a 1m timeout.~~
1. Use the community libraries dummy
