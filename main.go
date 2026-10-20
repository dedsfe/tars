// bot: grupos de IAs de terminal que trabalham juntas.
package main

import (
	"fmt"
	"os"

	"bot/internal/engines"
	"bot/internal/store"
	"bot/internal/ui"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
	p := tea.NewProgram(ui.New(st, engines.Detect()), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
}
