package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/rand/v2"
	"os"
	"slices"
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
		},
		help: help.New(),
		ping: probing.New(target),
		spin: spinner.New(spinner.WithSpinner(spinner.Dot)),

		speedX: -1, // Init signals this to start at 0
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
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Fast, k.Slow},
		{k.Reset},
		{k.Help, k.Quit},
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

	// running statistics
	// https://en.wikipedia.org/wiki/Algorithms_for_calculating_variance#Welford's_online_algorithm
	recv uint64        // number of packets received
	mean time.Duration // average rtt
	dem2 time.Duration // sum of squared differences from the mean (used to calculate standard deviation)
}

type pingPoint struct {
	Rtt time.Duration
	Seq int
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		rescale(0),                        // start pinging
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
			m.quitting = true
			m.ping.Stop()
			// TODO: wait for a window for any outstanding pings
			return m, tea.Quit
		case key.Matches(msg, m.keys.Reset):
			m.recv = 0
			m.mean = 0
			m.dem2 = 0
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
			m.data = append(m.data, pingPoint{Rtt: 0, Seq: msg.Seq})
			if m.w > 0 && len(m.data) > m.w {
				m.data = m.data[len(m.data)-m.w:]

				// once we fill the width... let's rescale to a more reasonable interval
				if !m.changed {
					m.changed = true
					return m, rescale(len(intervals) - 2) // not a snail, but not a rabbit
				}
			}
			return m, nil //printf("send: id: %d; seq: %d", msg.ID, msg.Seq)
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
	const buffer = 3 /* precision */ + 1 /* padding */ + 2 /* axis */
	maxPoints := m.w - buffer

	line := fmt.Sprintf(`width: %d, buffer: %d, maxPoints: %d`, m.w, buffer, maxPoints)

	head := lipgloss.Place(m.w, 1, lipgloss.Center, lipgloss.Center, line)

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
	line = fmt.Sprintf(`recv: %6d, avg: %.3fms, sd: %.3fms, 1sd: %.3fms, 2sd: %.3fms, 3sd: %.3fms`, m.recv, avg, sd, sd1, sd2, sd3)
	head += "\n" + lipgloss.Place(m.w, 1, lipgloss.Center, lipgloss.Center, line)

	// remove NaNs from the data (duplicate points slice as delete func modifies the slice)
	nanLessPoints := slices.DeleteFunc(slices.Clone(points), math.IsNaN)
	if len(nanLessPoints) == 0 {
		return head + "\n" + m.help.View(m.keys)
	}

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
		asciigraph.LowerBound(math.Floor(min(slices.Min(nanLessPoints), avg))),
		asciigraph.UpperBound(math.Ceil(max(slices.Max(nanLessPoints), sd3))),
	)

	maximum := slices.Max(nanLessPoints)
	if maximum < 50 {
		return head + "\n" + chart + "\n" + m.help.View(m.keys)
	}

	color := lipgloss.Color(`#FF0000`)
	message := `WARNING: high latency detected (>100ms)`
	if maximum < 100 {
		color = lipgloss.Color(`#FFA500`)
		message = `WARNING: elevated latency detected (>50ms)`
	}

	var warning = lipgloss.NewStyle().
		Border(lipgloss.DoubleBorder()).
		BorderForeground(color).
		Foreground(color).
		Bold(true).
		Align(lipgloss.Center, lipgloss.Center).
		Margin(1).
		Width(m.w - 2 - 2). // 2 for border; 2 for margin
		Height(3).
		Blink(maximum >= 100).
		Render(message)

	return warning + "\n" + head + "\n" + chart + "\n" + m.help.View(m.keys)
}

func dur2ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
