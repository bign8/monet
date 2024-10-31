package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/help"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/guptarohit/asciigraph"
	probing "github.com/prometheus-community/pro-bing"
)

func chk(name string, err error) {
	if err != nil {
		slog.Error(name, `error`, err.Error())
		os.Exit(1)
	}
}

func main() {
	// TODO: start pinging some well known IPs 1.0.0.1, 1.1.1.1

	target := `2606:4700:4700::1111`
	if len(os.Args) > 1 {
		target = os.Args[1]
	}

	// clockwise spinning dots
	slices.Reverse(spinner.Dot.Frames)

	m := model{
		keys: keyMap{
			Fast: key.NewBinding(
				key.WithKeys(`f`),
				key.WithHelp(`f`, `Fast mode`),
			),
			Slow: key.NewBinding(
				key.WithKeys(`s`),
				key.WithHelp(`s`, `Slow mode`),
			),
			Help: key.NewBinding(
				key.WithKeys(`?`),
				key.WithHelp(`?`, `Help`),
			),
			Quit: key.NewBinding(
				key.WithKeys(`q`, `esc`, `ctrl+c`),
				key.WithHelp(`q`, `quit`),
			),
			Reset: key.NewBinding(
				key.WithKeys(`r`),
				key.WithHelp(`r`, `Reset Stats`),
			),
			Debug: key.NewBinding(
				key.WithKeys(`d`),
				key.WithHelp(`d`, `Toggle Debug`),
			),
			Warn: key.NewBinding(
				key.WithKeys(`w`),
				key.WithHelp(`w`, `Toggle Warning`),
			),
			Fail: key.NewBinding(
				key.WithKeys(`e`),
				key.WithHelp(`e`, `Toggle Error`),
			),
			ClearWarn: key.NewBinding(
				key.WithKeys(`W`),
				key.WithHelp(`W`, `Clear Warning`),
			),
			ClearFail: key.NewBinding(
				key.WithKeys(`E`),
				key.WithHelp(`E`, `Clear Error`),
			),
		},
		help: help.New(),
		ping: probing.New(target),
		spin: spinner.New(spinner.WithSpinner(spinner.Dot)),
	}

	p := tea.NewProgram(m)

	_, err := p.Run()
	chk(`Error running program`, err)
}

type keyMap struct {
	Fast  key.Binding
	Slow  key.Binding
	Help  key.Binding
	Quit  key.Binding
	Reset key.Binding
	Debug key.Binding

	Warn      key.Binding
	Fail      key.Binding
	ClearWarn key.Binding
	ClearFail key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Fast, k.Slow},
		{k.Reset, k.Debug},
		{k.Help, k.Quit},
		{k.Warn, k.ClearWarn},
		{k.Fail, k.ClearFail},
	}
}

type model struct {
	keys     keyMap          // key bindings
	help     help.Model      // help indicators
	ping     *probing.Pinger // actual thing doing the pinging
	spin     spinner.Model   // indicator to ensure we're still alive
	data     []pingPoint     // stream of fired and potentially received packets
	quitting bool            // TODO: rename `quit` (why not have all state be 4 chars long?)
	w, h     int             // world size

	speedX  int  // index into `intervals` slice
	changed bool // have we slowed down since starting (we start fast to fill the screen, but slow to a reasonable interval)

	warn  uint // high latency warning semaphore
	debug bool

	// running statistics
	// https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance#Welford's_online_algorithm
	recv uint64        // number of packets received
	mean time.Duration // average rtt
	dem2 time.Duration // sum of squared differences from the mean (used to calculate standard deviation)
}

type pingPoint struct {
	Rtt time.Duration
	ID  int
	Seq int
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		rescale(1),                        // start pinging
		m.spin.Tick,                       // start spinner
		tea.SetWindowTitle(`Checking...`), // get a fun window title going!
	)
}

type wrappedMsg struct {
	more chan tea.Msg
	this tea.Msg
}

func printf(format string, args ...interface{}) tea.Cmd {
	const timeFormat = `2006-01-02 15:04:05.000`
	return tea.Printf(time.Now().Format(timeFormat)+`: `+format, args...)
}

// message to modify the interval of the pinger
// value is an index into the intervals slice
type rescaleMessage int

func rescale(i int) tea.Cmd {
	return func() tea.Msg {
		return rescaleMessage(i)
	}
}

var intervals = []time.Duration{
	// while the stackOverflow answer provides subtle math, this list seems easier to read
	// https://stackoverflow.com/a/53760271
	// for local services, it's fun to go faster, but... let's be nice to the internet
	25 * time.Millisecond,
	50 * time.Millisecond,
	100 * time.Millisecond,
	250 * time.Millisecond,
	500 * time.Millisecond,
	time.Second,
	// Let's be real, nobodies going above this!
	// 2 * time.Second,
	// 5 * time.Second,
	// 10 * time.Second,
	// 20 * time.Second,
	// 30 * time.Second,
	// time.Minute,
}

func (m *model) rescale(next time.Duration) tea.Cmd {
	pinger := probing.New(m.ping.Addr())
	pinger.Interval = next

	// don't record RTTs (we're only interested in the last m.w points)
	pinger.RecordRtts = false

	// ensure we have a different ID when rescaling (to avoid sequence collisions)
	for pinger.ID() == m.ping.ID() {
		pinger.SetID(rand.IntN(math.MaxUint16))
	}

	m.ping.Stop()
	m.ping = pinger

	events := make(chan tea.Msg, 20)

	pinger.OnSend = func(ping *probing.Packet) {
		events <- ping
	}
	pinger.OnSendError = func(ping *probing.Packet, err error) {
		events <- printf(`on-send-err: %#v; %s`, ping, err.Error())
	}
	pinger.OnRecv = func(ping *probing.Packet) {
		events <- ping
	}
	pinger.OnRecvError = func(err error) {
		// TODO: ignore i/o timeouts (it's a read loop)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return
		}
		events <- printf(`on-recv-err: %s`, err.Error())
	}

	return tea.Batch(
		func() tea.Msg {
			return wrappedMsg{
				more: events,
				this: <-events,
			}
		},
		func() tea.Msg {
			return m.ping.Run()
		},
	)
}

// message to check the status of a specific ping, if we can't see it, sound the alarm!!!
type howAreYaNow struct {
	// this is currently complicated because during re-scales, the pinger changes IDs and re-uses sequence numbers
	ID  int
	Seq int
}

// message to clear the warning semaphore
type goodAndYou struct{}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case rescaleMessage:
		desired := min(max(int(msg), 0), len(intervals)-1)
		if m.speedX != desired {
			m.speedX = desired
			return m, m.rescale(intervals[desired])
		}

	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Fast):
			m.changed = true
			return m, rescale(m.speedX - 1)
		case key.Matches(msg, m.keys.Slow):
			m.changed = true
			return m, rescale(m.speedX + 1)
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true // TODO: print final statistics on quitting
			m.ping.Stop()
			// TODO: wait for a window for any outstanding pings
			return m, tea.Quit
		case key.Matches(msg, m.keys.Reset):
			m.recv = 0
			m.mean = 0
			m.dem2 = 0
		case key.Matches(msg, m.keys.Debug):
			m.debug = !m.debug
		case key.Matches(msg, m.keys.Warn):
			for i := range m.data {
				m.data[i].Rtt += 50 * time.Millisecond
			}
		case key.Matches(msg, m.keys.Fail):
			if m.warn == 0 {
				m.warn++
			}
		case key.Matches(msg, m.keys.ClearWarn):
			for i := range m.data {
				m.data[i].Rtt -= 50 * time.Millisecond
			}
		case key.Matches(msg, m.keys.ClearFail):
			if m.warn > 0 {
				m.warn--
			}
		default:
			return m, printf(`unknown key: %v`, msg)
		}

	case wrappedMsg:
		m, cmd := m.Update(msg.this)
		return m, tea.Batch(cmd, func() tea.Msg {
			return wrappedMsg{
				more: msg.more,
				this: <-msg.more,
			}
		})

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd

	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.help.Width = msg.Width

	case *probing.Packet:
		if msg.Rtt == 0 {
			m.data = append(m.data, pingPoint{
				ID:  msg.ID,
				Seq: msg.Seq,
			})
			if m.w > 0 && len(m.data) > m.w {
				m.data = m.data[len(m.data)-m.w:]

				// once we fill the width... let's rescale to a more reasonable interval
				if !m.changed {
					m.changed = true
					return m, rescale(len(intervals) - 2) // not a snail, but not a rabbit
				}
			}
			// return m, printf("send: id: %d; seq: %d", msg.ID, msg.Seq)
			return m, func() tea.Msg {
				time.Sleep(time.Second)
				return howAreYaNow{ID: msg.ID, Seq: msg.Seq}
			}
		}

		// TODO: use slices.BinarySearchFunc to find the right index
		myIndex := -1
		// NOTE: going backwards as sequence values are re-used when rescaling the pinger
		// This'll ensure previous values aren't updated, only the newest sequence value
		// TODO: this is fragile AF! (but it works for now)
		for i := len(m.data) - 1; i >= 0; i-- {
			p := m.data[i]
			if p.Seq == msg.Seq {
				myIndex = i
				break
			}
		}
		if myIndex < 0 {
			return m, printf("recv: id: %d; seq: %d; not found", msg.ID, msg.Seq)
		}

		m.data[myIndex].Rtt = msg.Rtt
		m.recv++
		// welford's online method for stddev
		// https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance#Welford's_online_algorithm
		pmean := m.mean
		delta := msg.Rtt - m.mean
		m.mean += delta / time.Duration(m.recv)
		delta2 := msg.Rtt - m.mean
		m.dem2 += delta * delta2

		experimentalStdDev := m.dem2 / time.Duration(m.recv)
		if experimentalStdDev < 0 {
			// 2024-10-15 21:31:35.055: count: 1145, pmean: 39.438ms, mean: 48.620ms, rtt: 10552.479ms, delta: 10513.041ms, delta2: 10503.859ms, m.dem2: 331884019.482ms
			return m, tea.Sequence(
				printf("recv: id: %d; seq: %d; negative stddev: %s", msg.ID, msg.Seq, experimentalStdDev),
				printf(
					"count: %d, pmean: %.3fms, mean: %.3fms, rtt: %.3fms, delta: %.3fms, delta2: %.3fms, m.dem2: %.3fms",
					m.recv,
					pmean,
					m.mean,
					msg.Rtt,
					delta,
					delta2,
					m.dem2,
				),
				// tea.Quit,
			)
		}
		return m, nil // printf("recv: id: %d; seq: %d", msg.ID, msg.Seq)

	case howAreYaNow:
		myPrecious := -1
		for i, p := range m.data {
			if p.ID == msg.ID && p.Seq == msg.Seq {
				myPrecious = i
				break
			}
		}
		if myPrecious < 0 {
			return m, printf("how-are-ya-now: id: %d; seq: %d; not found", msg.ID, msg.Seq)
		}
		if m.data[myPrecious].Rtt != 0 {
			return m, nil // all good, we've received the packed
		}

		m.warn++
		return m, func() tea.Msg {
			time.Sleep(20 * time.Second)
			return goodAndYou{}
		}

	case goodAndYou:
		m.warn--

	case tea.Cmd:
		// hacky work-around to get pinger to send commands to the model
		return m, msg

	default:
		if _, allowed := allowedMessages[fmt.Sprintf(`%T`, msg)]; !allowed {
			return m, printf(`unhandled message: %T(%#v)`, msg, msg)
		}
	}
	return m, nil
}

var allowedMessages = map[string]struct{}{
	`tea.sequenceMsg`:       {},
	`tea.printLineMessage`:  {},
	`tea.setWindowTitleMsg`: {},
}

func (m model) View() string {
	if m.quitting {
		return `Bye-bye` + "\n" // newline needed to not replace content on final terminal
	}

	// TODO: width is based on the number of characters in the axis title
	// 23.3  = 4 characters
	// 192   = 3 characters
	// 12345 = 5 characters
	// [space][number][space][y-axis] - typically seeing 4 character numbers, making buffer 6
	// y-axis itself counts as the 1st data-point (cause it's drawn with ┤ and ┼ characters)
	const buffer = 3 /* precision */ + 1 /* padding */ + 2 /* axis */ + 10 /* histogram */ + 2 /* border */
	maxPoints := m.w - buffer

	var head string
	if m.debug {
		line := fmt.Sprintf(`width: %d, buffer: %d, maxPoints: %d`, m.w, buffer, maxPoints)
		head = lipgloss.Place(m.w, 1, lipgloss.Center, lipgloss.Center, line)
	}

	if len(m.data) < 1 || m.w == 0 || m.recv == 0 {
		return head
	}

	stream := m.data

	if len(stream) > maxPoints {
		stream = stream[len(stream)-maxPoints:]
	}

	points := make([]float64, len(stream))
	for i, d := range stream {
		if d.Rtt == 0 {
			points[i] = math.NaN()
			continue
		}
		v := dur2ms(d.Rtt)
		// keep data in an interesting range (TODO: make this configurable + smarter)
		points[i] = min(max(v, 0), 99.9)
		if v != points[i] {
			// TODO: signify truncated value
		}
	}

	// perform non-pro-bing statistics
	// TODO: keep this math as time.Duration once we don't care about comparing to ^^ (the pro-bing stats)
	sd := dur2ms(time.Duration(math.Sqrt(float64(m.dem2 / time.Duration(m.recv)))))
	avg := dur2ms(m.mean)
	sd1 := sd*1 + avg
	sd2 := sd*2 + avg
	sd3 := sd*3 + avg
	if m.debug {
		line := fmt.Sprintf(`recv: %6d, avg: %.3fms, sd: %.3fms, 1sd: %.3fms, 2sd: %.3fms, 3sd: %.3fms`, m.recv, avg, sd, sd1, sd2, sd3)
		head += "\n" + lipgloss.Place(m.w, 1, lipgloss.Center, lipgloss.Center, line)
	}

	// remove NaNs from the data (duplicate points slice as delete func modifies the slice)
	nanLessPoints := slices.DeleteFunc(slices.Clone(points), math.IsNaN)
	if len(nanLessPoints) == 0 {
		return head + "\n" + m.help.View(m.keys)
	}

	minimum := math.Floor(min(slices.Min(nanLessPoints), avg))
	maximum := math.Ceil(max(slices.Max(nanLessPoints), sd3))
	chart := asciigraph.PlotMany(
		[][]float64{
			slices.Repeat([]float64{avg}, maxPoints),
			slices.Repeat([]float64{sd1}, maxPoints),
			slices.Repeat([]float64{sd2}, maxPoints),
			slices.Repeat([]float64{sd3}, maxPoints),
			points,
		},
		asciigraph.Precision(1), // decimals
		// asciigraph.Width(m.w-buffer), // chart area (not counting labels, axis, etc) // NOTE: controlled by maxPoints instead
		asciigraph.Height(20), //m.h-4-6 /* caption */), // -4 for spinner, title padding, and something else
		asciigraph.SeriesColors(
			asciigraph.Green,
			asciigraph.Yellow,
			asciigraph.Orange,
			asciigraph.Red,
			asciigraph.Blue,
		),
		asciigraph.SeriesLegends(
			"average",
			"1 deviation",
			"2 deviations",
			"3 deviations",
			m.ping.Addr(),
		),
		asciigraph.Caption(m.spin.View()+" Ping every "+m.ping.Interval.String()),

		// prevent axis from changing rapidly
		// TODO: ensure there are HEIGHT unique axis values (with 2 decimal places)
		asciigraph.LowerBound(minimum),
		asciigraph.UpperBound(maximum),
	)

	// histogram logic has a real bad day if interval is 0, which will require > 1 data point
	if len(nanLessPoints) < 2 {
		return head + "\n" + chart + "\n" + m.help.View(m.keys)
	}

	// create a really rough histogram given the current data's range
	{
		// maximum := max(slices.Max(nanLessPoints), sd3)
		// minimum := min(slices.Min(nanLessPoints), avg)
		interval := maximum - minimum
		ratio := float64(20) / interval
		min2 := math.Round(minimum * ratio) // not the same rounding algorithm as asciigraph

		buckets := make([]int, 21)
		for _, v := range nanLessPoints {
			y := int(math.Round(v*ratio) - min2)
			buckets[y]++
		}

		bucketMax := slices.Max(buckets)
		const maxBucketHeight = 7 /* characters */ * 8 /* block character is divided into 8 parts */
		if bucketMax > maxBucketHeight {
			// scale down the bars
			for i := range buckets {
				buckets[i] = int(float64(buckets[i]) / float64(bucketMax) * maxBucketHeight)
			}
		}

		histogram := make([]string, 21)

		// block characters can be divided into 8 parts (going left to right; right to left characters have fallen out of favor)
		blocks := []rune(`▉▊▋▌▍▎▏ `)
		slices.Reverse(blocks)

		for i := 0; i < 21; i++ {
			if buckets[i] == maxBucketHeight {
				histogram[i] = strings.Repeat(`█`, 7)
				continue
			}
			tip := buckets[i] % 8
			tail := buckets[i] / 8

			histogram[i] = strings.Repeat(`█`, tail) + string(blocks[tip])
		}
		for i := 0; i < 21; i++ {
			histogram[i] = fmt.Sprintf(` %-7s ├`, histogram[i])
		}
		slices.Reverse(histogram) // bottom is the smaller number

		// add total to bottom of histogram
		histogram = append(histogram, ``, fmt.Sprintf(` %8d `, m.recv))

		// prepend a histogram to the chart
		chart = lipgloss.JoinHorizontal(lipgloss.Top, strings.Join(histogram, "\n"), chart)
	}

	screen := head + "\n" + chart + "\n" + m.help.View(m.keys) // TODO: join vertical

	var frame = lipgloss.NewStyle().
		Border(lipgloss.HiddenBorder()).
		Align(lipgloss.Center, lipgloss.Center).
		Width(m.w - 2)

	{
		maximum := slices.Max(nanLessPoints)
		if maximum > 100 {
			color := lipgloss.Color(`#FF0000`)
			frame = frame.BorderForeground(color).
				Foreground(color).
				Border(lipgloss.DoubleBorder())
			line := "PINGS EXCEEDING 100ms" // TODO: colorize
			line = lipgloss.Place(m.w-2, 1, lipgloss.Center, lipgloss.Center, line)
			screen = lipgloss.JoinVertical(lipgloss.Top, line, screen)
		} else if maximum > 50 {
			color := lipgloss.Color(`#FFA500`)
			frame = frame.BorderForeground(color).
				Foreground(color).
				Border(lipgloss.DoubleBorder())
			line := "PINGS EXCEEDING 50ms" // TODO: colorize
			line = lipgloss.Place(m.w-2, 1, lipgloss.Center, lipgloss.Center, line)
			screen = lipgloss.JoinVertical(lipgloss.Top, line, screen)
		}
	}

	if m.warn > 0 {
		color := lipgloss.Color(`#FF0000`)
		frame = frame.BorderForeground(color).
			Foreground(color).
			Border(lipgloss.DoubleBorder())
		line := "PINGS EXCEEDING 1s" // TODO: colorize
		line = lipgloss.Place(m.w-2, 1, lipgloss.Center, lipgloss.Center, line)
		screen = lipgloss.JoinVertical(lipgloss.Top, line, screen)
	}

	return frame.Render(screen)
}

func dur2ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
