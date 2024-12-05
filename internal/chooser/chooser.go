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
	sortMode uint8 // 0 = name, 1 = duration, moar?
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

	// FWIW, this code is dripping with new special sauce from the go team
	// - ranging over an integer, pretty cool
	// - loop capturing the range variable, pretty cool
	// Just a lot of cool stuff here that could be bad in earlier versions of go!
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
			return m, nil
		}
		m.table[msg].status = "........."
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
		return m, func() tea.Msg {
			return pleasePing(msg.index + 1)
		}
	case tea.Cmd:
		return m, msg // allows please ping to send tea.Printf messages
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			m.quitting = true
			return m, tea.Quit
		case "s":
			if m.workers != 0 {
				return m, tea.Printf(`Can't sort while workers are running`)
			}
			m.sortMode = (m.sortMode + 1) % 2 // currently only 2 modes
			switch m.sortMode {
			case 0:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].owner < m.table[j].owner
				})
			case 1:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].duration < m.table[j].duration
				})
			}
		case "g":
			if m.workers != 0 {
				return m, tea.Printf(`Can't group while workers are running`)
			}
			table := make([]row, len(m.table))
			copy(table, m.table)
			return newGrouper(table, m), nil
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
	if m.workers != 0 {
		buff.WriteString(fmt.Sprintf("Working: %d\n", m.workers))
	} else {
		buff.WriteString("Press 's' to sort; 'g' to group\n")
	}
	return buff.String() + fmt.Sprintf("Press 'q' to quit (%d hosts)", len(m.table))
}

func maxLength(s []string) (max int) {
	for _, v := range s {
		if len(v) > max {
			max = len(v)
		}
	}
	return max
}

func newGrouper(table []row, revert tea.Model) *Grouper {

	owners := make(map[string]groupedRow)
	for _, row := range table {
		data := owners[row.owner]
		data.total += row.duration
		data.count++
		data.name = row.owner
		owners[row.owner] = data
	}

	names := slices.Collect(maps.Keys(owners))
	sort.Strings(names)

	myTable := make([]groupedRow, 0, len(names))
	for _, name := range names {
		myTable = append(myTable, owners[name])
	}

	return &Grouper{table: myTable, revert: revert}
}

type groupedRow struct {
	name  string
	total time.Duration
	count int
}

func (row groupedRow) avg() float64 {
	return row.total.Seconds() * 1000 / float64(row.count)
}

type Grouper struct {
	table    []groupedRow
	sortMode uint8 // name, total, count, average, moar?
	revert   tea.Model
}

func (m *Grouper) Init() tea.Cmd {
	return nil
}

func (m *Grouper) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.Cmd:
		return m, msg // allows please ping to send tea.Printf messages
	case tea.KeyMsg:
		switch msg.String() {
		case "q":
			return m, tea.Quit
		case "g":
			return m.revert, nil
		case "s":
			m.sortMode = (m.sortMode + 1) % 4 // currently only 2 modes
			switch m.sortMode {
			case 0:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].name < m.table[j].name
				})
			case 1:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].total < m.table[j].total
				})
			case 2:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].count < m.table[j].count
				})
			case 3:
				sort.Slice(m.table, func(i, j int) bool {
					return m.table[i].avg() < m.table[j].avg()
				})
			}
		}
	}
	return m, nil
}

func (m *Grouper) View() string {

	var buff strings.Builder
	for _, row := range m.table {
		buff.WriteString(fmt.Sprintf("%s: %.3fms (ips: %d)\n", row.name, row.avg(), row.count))
	}

	return buff.String() + "\npress 'g' to revert; 's' to sort; 'q' to quit"
}
