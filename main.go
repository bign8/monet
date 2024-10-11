package main

import (
	"fmt"
	"log/slog"
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
	keys     keyMap
	help     help.Model
	ping     *probing.Pinger
	wait     bool // are we waiting for a response??
	spin     spinner.Model
	line     string
	quitting bool // TODO: rename `quit` (why not have all state be 4 chars long?)
	w, h     int
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
	m.ping.Stop()
	m.ping = pinger

	// TODO: retain the existing statistics

	events := make(chan tea.Msg, 20)

	pinger.OnSend = func(ping *probing.Packet) {
		events <- ping
	}
	pinger.OnSendError = func(ping *probing.Packet, err error) {
		events <- tea.Printf(`on-send-err: %#v; %s`, ping, err.Error())
	}
	pinger.OnRecv = func(ping *probing.Packet) {
		events <- ping
	}
	// pinger.OnRecvError = func(err error) {
	// 	if errors.Is(err, os.ErrDeadlineExceeded) {
	// 		return
	// 	}
	// 	// TODO: ignore i/o timeouts (it's a read loop)
	// 	p.Send(tea.Printf(`on-recv-err: %s`, err.Error()))
	// }

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
			if m.wait {
				// TODO: queue a key to be processed after the response is received
			}
			cmd := m.rescale(m.ping.Interval / 2) // TODO: have this be a little more reasonable [100, 250, 500, 1000, 2000, 5000]
			return m, cmd
		case key.Matches(msg, m.keys.Slow):
			if m.wait {
				// TODO: queue a key to be processed after the response is received
			}
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
	case string:
		m.line = msg
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		m.help.Width = msg.Width
	case *probing.Packet:
		if msg.Rtt == 0 {
			m.line = msg.Addr + `: ??.???ms`
			m.wait = true
		} else {
			m.line = fmt.Sprintf(`%s: %.3fms`, msg.Addr, dur2ms(msg.Rtt))
			m.wait = false // what about errors?
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
	line := m.line + "\n"

	const buffer = 3 /* precision */ + 1 /* padding */ + 1 /* axis */ + 3 /* offset? */

	maxPoints := m.w - buffer - 1 /* off by one? */

	line += fmt.Sprintf(`width: %d, buffer: %d, maxPoints: %d`, m.w, buffer, maxPoints)

	head := lipgloss.Place(m.w, 2, lipgloss.Center, lipgloss.Center, line)

	stats := m.ping.Statistics()
	if len(stats.Rtts) < 2 || m.w == 0 {
		return head
	}

	if len(stats.Rtts) > maxPoints {
		stats.Rtts = stats.Rtts[len(stats.Rtts)-maxPoints:]
	}

	points := make([]float64, len(stats.Rtts))
	for i, d := range stats.Rtts {
		// cap the data between 99.9 and 10.0 to ensure axis is constant width
		v := dur2ms(d)
		points[i] = min(max(v, 10), 99.9)
		if v != points[i] {
			// TODO: signify truncated value
		}
	}

	// TODO: width is based on the number of characters in the axis title
	// 23.3  = 4 characters
	// 192   = 3 characters
	// 12345 = 5 characters

	// TODO: manually cap data to exist within a reasonable range

	chart := asciigraph.PlotMany(
		[][]float64{
			slices.Repeat([]float64{dur2ms(stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt + stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt*2 + stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt*3 + stats.AvgRtt)}, maxPoints),
			points,
		},
		asciigraph.Precision(1), // decimals
		// asciigraph.Width(m.w-buffer), // chart area (not counting labels, axis, etc)
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
			stats.Addr,
		),
		asciigraph.Caption(m.spin.View()+" Ping every "+m.ping.Interval.String()),
	)

	var style = lipgloss.NewStyle().
		BorderStyle(lipgloss.Border{
			// useful for measuring height/width of resulting chart
			Top:         `-`,
			Bottom:      `-`,
			Left:        `|`,
			Right:       `|`,
			TopLeft:     `+`,
			TopRight:    `+`,
			BottomLeft:  `+`,
			BottomRight: `+`,
		}).
		BorderStyle(lipgloss.NormalBorder()).
		BorderForeground(lipgloss.Color(`#FF0000`))

	chart = style.Render(chart)

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
