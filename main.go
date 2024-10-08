package main

import (
	"fmt"
	"log/slog"
	"os"
	"slices"
	"time"

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
	// monitor the response time and print the active status

	// clockwise spinning dots
	slices.Reverse(spinner.Dot.Frames)

	// pinger, err := probing.NewPinger(`1.1.1.1`)
	pinger, err := probing.NewPinger(`2606:4700:4700::1111`)
	chk(`Error creating pinger`, err)
	pinger.Interval = 100 * time.Millisecond

	m := model{
		ping: pinger,
		spin: spinner.New(spinner.WithSpinner(spinner.Dot)),
	}

	p := tea.NewProgram(m)

	pinger.OnSend = func(ping *probing.Packet) {
		p.Send(ping)
	}
	pinger.OnSendError = func(ping *probing.Packet, err error) {
		p.Send(tea.Printf(`on-send-err: %#v; %s`, ping, err.Error()))
	}
	pinger.OnRecv = func(ping *probing.Packet) {
		p.Send(ping)
	}
	pinger.OnFinish = func(s *probing.Statistics) {
		p.Send(tea.Printf(`%#v`, s))
	}
	// pinger.OnRecvError = func(err error) {
	// 	// TODO: ignore i/o timeouts (it's a read loop)
	// 	p.Send(tea.Printf(`on-recv-err: %s`, err.Error()))
	// }

	go func() {
		chk(`string go pinger`, m.ping.Run())
	}()

	_, err = p.Run()
	chk(`Error running program`, err)

}

type model struct {
	ping     *probing.Pinger
	spin     spinner.Model
	line     string
	quitting bool
	w, h     int
}

func (m model) Init() tea.Cmd {
	return tea.Batch(
		m.spin.Tick,                       // start spinner
		tea.SetWindowTitle(`Checking...`), // get a fun window title going!
	)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// TODO: send ctrl+C to the ping command + output the output as the final message
		m.quitting = true
		m.ping.Stop()
		return m, tea.Quit
	case string:
		m.line = msg
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
	case *probing.Packet:
		if msg.Rtt == 0 {
			m.line = msg.Addr + `: ??.???ms`
		} else {
			m.line = fmt.Sprintf(`%s: %.3fms`, msg.Addr, dur2ms(msg.Rtt))
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
	head := lipgloss.Place(m.w, 2, lipgloss.Center, lipgloss.Center, line)

	stats := m.ping.Statistics()
	if len(stats.Rtts) < 2 || m.w == 0 {
		return head
	}

	maxPoints := m.w - 3 /* precision */ - 1 /* padding */ - 1 /* axis */
	if len(stats.Rtts) > maxPoints {
		stats.Rtts = stats.Rtts[len(stats.Rtts)-maxPoints:]
	}

	points := make([]float64, len(stats.Rtts))
	for i, d := range stats.Rtts {
		points[i] = dur2ms(d)
	}

	chart := asciigraph.PlotMany(
		[][]float64{
			slices.Repeat([]float64{dur2ms(stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt + stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt*2 + stats.AvgRtt)}, maxPoints),
			slices.Repeat([]float64{dur2ms(stats.StdDevRtt*3 + stats.AvgRtt)}, maxPoints),
			points,
		},
		asciigraph.Precision(1), // decimals
		asciigraph.Width(m.w-3 /* precision */ -1 /* padding */ -1 /* axis */),
		asciigraph.Height(20), //m.h-4-6 /* caption */), // -4 for spinner, title padding, and something else
		asciigraph.SeriesColors(
			asciigraph.Green,
			asciigraph.Yellow,
			asciigraph.Orange,
			asciigraph.Red,
			asciigraph.Blue,
		),
		asciigraph.SeriesLegends(
			"mean",
			"1 deviation",
			"2 deviations",
			"3 deviations",
			"1.1.1.1",
		),
		asciigraph.Caption(m.spin.View()+" Ping delay in ms"), // is this needed?
	)

	return head + "\n" + chart
}

func dur2ms(d time.Duration) float64 {
	return float64(d.Microseconds()) / 1000
}
