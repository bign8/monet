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

	table := make([]row, 0, len(allIPs))
	for _, owner := range owners {
		for _, ip := range providers[owner] {
			table = append(table, row{
				owner:    owner,
				ip:       ip,
				status:   "pending",
				duration: time.Hour,
			})
		}
	}

	return &Chooser{
		table:    table,
		template: fmt.Sprintf("| %%%ds | %%-%ds | %%9s |\n", ownerPad, ipPad), // len(xxx.xxxms) = 9
		ownerPad: ownerPad,
		ipPad:    ipPad,
		workers:  10,
	}
}

var _ tea.Model = (*Chooser)(nil)

type Chooser struct {
	table    []row
	template string
	ownerPad int
	ipPad    int
	workers  int
	quitting bool
}

type row struct {
	owner, ip string
	status    string
	duration  time.Duration
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
		if int(msg) >= len(m.table) || m.table[msg].status != "pending" {
			m.workers--
			if m.workers == 0 {
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].duration < m.table[j].duration
				})
			}
			return m, nil
		}
		m.table[msg].status = "..pinging"
		pinger := probing.New(m.table[msg].ip)
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
			m.table[msg.index].status = msg.err.Error()
		} else {
			d := msg.stats.AvgRtt.Round(time.Microsecond)
			m.table[msg.index].duration = d
			m.table[msg.index].status = fmt.Sprintf(`%.3fms`, d.Seconds()*1000)
		}
		msg.index++
		return m, func() tea.Msg {
			return pleasePing(msg.index)
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
	line := func() {
		write(
			strings.Repeat(`-`, m.ownerPad),
			strings.Repeat(`-`, m.ipPad),
			strings.Repeat(`-`, 9), // len(xxx.xxxms) = 9
		)
	}

	line()
	write(`Owner`, `IP`, `Status`)
	line()
	for _, row := range m.table {
		write(row.owner, row.ip, row.status)
	}
	line()
	buff.WriteRune('\n')
	if m.workers != 0 {
		buff.WriteString(fmt.Sprintf("Working: %d\n", m.workers))
	}
	return buff.String() + "Press 'q' to quit"
}

func maxLength(s []string) (max int) {
	for _, v := range s {
		if len(v) > max {
			max = len(v)
		}
	}
	return max
}
