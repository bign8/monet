package chooser

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
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
		owners:   owners,
		template: fmt.Sprintf("%%%ds : %%-%ds : %%s\n", ownerPad, ipPad),
	}
}

var _ tea.Model = (*Chooser)(nil)

type Chooser struct {
	table    [][3]string
	owners   []string
	template string
	quitting bool
}

type letsGo struct{}

func (m *Chooser) Init() tea.Cmd {
	return func() tea.Msg {
		return letsGo{}
	}
}

func (m *Chooser) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case letsGo:
		// pick some number of pingers to start (3?)
		// start their work queues and ping away
		return m, tea.Printf(`what would you like to do? (q to quit) %d`, len(m.table))
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
