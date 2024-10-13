package main

import (
	"errors"
	"fmt"
	"log/slog"
	"math"
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
				key.WithKeys("q", "esc", "ctrl+c"),
				key.WithHelp("q", "quit"),
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
	Fast key.Binding
	Slow key.Binding
	Help key.Binding
	Quit key.Binding
}

func (k keyMap) ShortHelp() []key.Binding {
	return []key.Binding{k.Help, k.Quit}
}

func (k keyMap) FullHelp() [][]key.Binding {
	return [][]key.Binding{
		{k.Fast, k.Slow},
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
}

type pingPoint struct {
	Rtt float64 // in ms
	Seq int
}

type startCmd struct{}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		func() tea.Msg {
			return startCmd{}
		},
		m.spin.Tick,                       // start spinner
		tea.SetWindowTitle(`Checking...`), // get a fun window title going!
	)
}

type wrappedMsg struct {
	more chan tea.Msg
	this tea.Msg
}

func (m *model) rescale(next time.Duration) tea.Cmd {
	pinger := probing.New(m.ping.Addr())
	pinger.Interval = next
	// pinger.SetID(m.ping.ID()) // DO NOT WANT THIS
	m.ping.Stop()
	m.ping = pinger

	events := make(chan tea.Msg, 20)
	const timeFormat = `2006-01-02 15:04:05.000`

	pinger.OnSend = func(ping *probing.Packet) {
		events <- ping
	}
	pinger.OnSendError = func(ping *probing.Packet, err error) {
		events <- tea.Printf(`%s: on-send-err: %#v; %s`, time.Now().Format(timeFormat), ping, err.Error())
	}
	pinger.OnRecv = func(ping *probing.Packet) {
		events <- ping
	}
	pinger.OnRecvError = func(err error) {
		// TODO: ignore i/o timeouts (it's a read loop)
		if errors.Is(err, os.ErrDeadlineExceeded) {
			return
		}
		events <- tea.Printf(`%s: on-recv-err: %s`, time.Now().Format(timeFormat), err.Error())
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
	case startCmd:
		cmd := m.rescale(100 * time.Millisecond)
		return m, cmd
	case tea.KeyMsg:
		switch {
		case key.Matches(msg, m.keys.Fast):
			cmd := m.rescale(m.ping.Interval / 2) // TODO: have this be a little more reasonable [100, 250, 500, 1000, 2000, 5000]
			return m, cmd
		case key.Matches(msg, m.keys.Slow):
			cmd := m.rescale(m.ping.Interval * 2) // TODO: have this be a little more reasonable [100, 250, 500, 1000, 2000, 5000]
			return m, cmd
		case key.Matches(msg, m.keys.Help):
			m.help.ShowAll = !m.help.ShowAll
		case key.Matches(msg, m.keys.Quit):
			m.quitting = true
			m.ping.Stop()
			// TODO: wait for final statistics?
			return m, tea.Quit
		default:
			return m, tea.Printf(`unknown key: %v`, msg)
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
			m.data = append(m.data, pingPoint{Rtt: math.NaN(), Seq: msg.Seq})
			if m.w > 0 && len(m.data) > m.w {
				m.data = m.data[len(m.data)-m.w:]
			}
			return m, tea.Printf("send: id: %d; seq: %d", msg.ID, msg.Seq)
		} else {
			// TODO: use slices.BinarySearchFunc to find the right index
			var found bool
			// NOTE: going backwards as sequence values are re-used when rescaling the pinger
			// This'll ensure previous values aren't updated, only the newest sequence value
			// TODO: this is fragile AF! (but it works for now)
			for i := len(m.data) - 1; i >= 0; i-- {
				p := m.data[i]
				if p.Seq == msg.Seq {
					found = true
					m.data[i].Rtt = dur2ms(msg.Rtt)
					break
				}
			}
			if !found {
				return m, tea.Printf("recv: id: %d; seq: %d; not found", msg.ID, msg.Seq)
			}
			return m, tea.Printf("recv: id: %d; seq: %d", msg.ID, msg.Seq)
		}
	case tea.Cmd:
		// hacky work-around to get pinger to send commands to the model
		return m, msg
	default:
		if _, allowed := allowedMessages[fmt.Sprintf(`%T`, msg)]; !allowed {
			return m, tea.Printf(`unhandled message: %T(%#v)`, msg, msg)
		}
	}
	return m, nil
}

var allowedMessages = map[string]struct{}{
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

	if len(m.data) < 1 || m.w == 0 {
		return head
	}

	stream := m.data

	if len(stream) > maxPoints {
		stream = stream[len(stream)-maxPoints:]
	}

	points := make([]float64, len(stream))
	for i, d := range stream {
		v := d.Rtt
		// keep data in an interesting range (TODO: make this configurable + smarter)
		points[i] = min(max(v, 0), 99.9)
		if v != points[i] {
			// TODO: signify truncated value
		}
	}

	// gather + print some of those running statistics
	stats := m.ping.Statistics()
	avg := dur2ms(stats.AvgRtt)
	sd := dur2ms(stats.StdDevRtt)
	sd1 := sd*1 + avg
	sd2 := sd*2 + avg
	sd3 := sd*3 + avg
	line = fmt.Sprintf(`avg: %.3fms, sd: %.3fms, 1sd: %.3fms, 2sd: %.3fms, 3sd: %.3fms`, avg, sd, sd1, sd2, sd3)
	head += "\n" + lipgloss.Place(m.w, 1, lipgloss.Center, lipgloss.Center, line)

	// remove NaNs from the data
	nanLessPoints := make([]float64, 0, len(points))
	for _, v := range points {
		if !math.IsNaN(v) {
			nanLessPoints = append(nanLessPoints, v)
		}
	}
	if len(nanLessPoints) == 0 {
		return head + "\n" + m.help.View(m.keys)
	}

	// TODO: manually cap data to exist within a reasonable range

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

	// if NaNs are present, assume the data just hasn't gotten back yet; no alert
	// TODO: make this a little more robust (e.g. if NaNs are present for a long time, alert)
	if slices.ContainsFunc(points, math.IsNaN) {
		return head + "\n" + chart + "\n" + m.help.View(m.keys)
	}

	maximum := slices.Max(points)
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
