package main

import (
	"github.com/bign8/monet/internal/chooser"
	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	model := chooser.New()
	_, err := tea.NewProgram(model).Run()
	if err != nil {
		panic(err)
	}
}
