package main

import (
	"bufio"
	"bytes"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"slices"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
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

	m := model{
		spin: spinner.New(spinner.WithSpinner(spinner.Dot)),
		cmd:  exec.Command(`ping`, `1.1.1.1`),
	}

	// https://www.yellowduck.be/posts/reading-command-output-line-by-line
	buff, err := m.cmd.StdoutPipe()
	chk(`Error getting stdout pipe`, err)
	m.cmd.Stderr = m.cmd.Stdout
	defer buff.Close()

	p := tea.NewProgram(m, tea.WithAltScreen())

	chk(`Error starting ping`, m.cmd.Start())

	go func() {
		s := bufio.NewScanner(buff)
		for s.Scan() {
			p.Send(s.Text())
		}
		if err := s.Err(); err != nil {
			slog.Error(`done doing something?`, `error`, s.Err().Error())
		} else {
			slog.Info(`done doing something?`)
		}
	}()

	_, err = p.Run()
	chk(`Error running program`, err)

	chk(`error signaling ping`, m.cmd.Process.Signal(os.Interrupt))

	bits, err := io.ReadAll(buff)
	chk(`Error reading from buffer`, err)
	slog.Info(`Final output`, `output`, string(bits))
}

type model struct {
	spin     spinner.Model
	cmd      *exec.Cmd
	buff     bytes.Buffer
	quitting bool
}

func (m model) Init() tea.Cmd {
	return m.spin.Tick
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// TODO: send ctrl+C to the ping command + output the output as the final message
		return m, tea.Batch(tea.ExitAltScreen, func() tea.Msg {
			return exitSequence(0) // set quitting once we exit the alt screen (so final result is visible)
		}, tea.Quit)
	case exitSequence:
		m.quitting = true
	case string:
		m.buff.Reset()
		m.buff.WriteString(msg + "\n")
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spin, cmd = m.spin.Update(msg)
		return m, cmd
	}
	return m, nil
}

type exitSequence int // TODO: custom signals if this gets any more complicated :shurg:

func (m model) View() string {
	if m.quitting {
		return `Bye-bye` + "\n" // newline needed to not replace content on final terminal
	}
	return m.spin.View() + ` PROCESSING` + "\n" + m.buff.String()
}
