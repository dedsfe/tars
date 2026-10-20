// bot: grupos de IAs de terminal que trabalham juntas.
package main

import (
	"fmt"
	"os"

	"bot/internal/config"
	"bot/internal/engines"
	"bot/internal/store"
	"bot/internal/sys"
	"bot/internal/ui"
	"bot/internal/voice"

	tea "github.com/charmbracelet/bubbletea"
)

func main() {
	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
	cfg, err := config.Load(st.Root())
	if err != nil {
		fmt.Fprintln(os.Stderr, "bot: usando o padrão porque", err)
	}
	sys.SetLimits(cfg.Disk.WarnGB, cfg.Disk.PauseGB, cfg.Disk.StopMB)
	voice.Microphone = cfg.Voice.Microphone

	p := tea.NewProgram(ui.New(st, engines.Detect(), cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "bot:", err)
		os.Exit(1)
	}
}
