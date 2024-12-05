package chooser

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	probing "github.com/prometheus-community/pro-bing"
)

//go:embed providers.json
var providersJSON []byte

func New() *Chooser {
	var providers map[string][]string
	if err := json.Unmarshal(providersJSON, &providers); err != nil {
		panic(`invalid json providers: ` + err.Error())
	}

	// pre-compute table rendering data
	owners := slices.Collect(maps.Keys(providers))
	sort.Strings(owners)

	ownerPad := maxLength(owners)

	allIPs := make([]string, 0, len(providers)*4)
	for _, ips := range providers {
		allIPs = append(allIPs, ips...)
	}
	ipPad := maxLength(allIPs)

	table := make([][3]string, 0, len(allIPs))
	for _, owner := range owners {
		for _, ip := range providers[owner] {
			table = append(table, [3]string{owner, ip, "pending"})
		}
	}

	return &Chooser{
		table:    table,
		template: fmt.Sprintf("%%%ds : %%-%ds : %%s\n", ownerPad, ipPad),
		workers:  10,
	}
}

var _ tea.Model = (*Chooser)(nil)

type Chooser struct {
	table    [][3]string
	template string
	workers  int
	quitting bool
}

type pleasePing int

func (m *Chooser) Init() tea.Cmd {

	// the integer equivalent of math.Ceil(len(m.table) / m.workers)
	// rounding up so the last worker doesn't get over worked by the remainder
	n := len(m.table) + m.workers - 1
	n /= m.workers

	batch := make([]tea.Cmd, m.workers)
	for i := range m.workers {
		batch[i] = func() tea.Msg {
			return pleasePing(n * i)
		}
	}
	return tea.Batch(batch...)
}

type pingResult struct {
	index int
	err   error
	stats *probing.Statistics
}

func (m *Chooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pleasePing:
		if m.table[msg][2] != "pending" {
			return m, nil
		}
		m.table[msg][2] = "pinging..."
		pinger := probing.New(m.table[msg][1])
		pinger.Count = 3
		pinger.Interval = time.Millisecond * 50
		pinger.Timeout = time.Second
		return m, func() tea.Msg {
			err := pinger.Run()
			return pingResult{
				index: int(msg),
				err:   err,
				stats: pinger.Statistics(),
			}
		}
	case pingResult:
		if msg.err != nil {
			m.table[msg.index][2] = msg.err.Error()
		} else {
			m.table[msg.index][2] = msg.stats.AvgRtt.Round(time.Microsecond).String()
		}
		msg.index++
		if msg.index < len(m.table) {
			return m, func() tea.Msg {
				return pleasePing(msg.index)
			}
		}
	case tea.Cmd:
		return m, msg // allows please ping to send tea.Printf messages
	case tea.KeyMsg:
		if msg.String() == "q" {
			m.quitting = true
			return m, tea.Quit
		}
	}
	return m, nil
}

func (m *Chooser) View() string {
	if m.quitting {
		return "Quitting..." // line is overwritten by new terminal line
	}

	var buff strings.Builder
	write := func(owner, ip, status string) {
		buff.WriteString(fmt.Sprintf(m.template, owner, ip, status))
	}
	write(`-----`, `--`, `------`)
	write(`Owner`, `IP`, `Status`)
	write(`-----`, `--`, `------`)
	for _, row := range m.table {
		write(row[0], row[1], row[2])
	}
	write(`-----`, `--`, `------`)
	return buff.String()
}

func maxLength(s []string) (max int) {
	for _, v := range s {
		if len(v) > max {
			max = len(v)
		}
	}
	return max
}
