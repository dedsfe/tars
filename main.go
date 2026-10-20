// tars: grupos de IAs de terminal que trabalham juntas.
package main

import (
	"fmt"
	"os"

	"github.com/dedsfe/tars/internal/config"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/store"
	"github.com/dedsfe/tars/internal/sys"
	"github.com/dedsfe/tars/internal/ui"
	"github.com/dedsfe/tars/internal/voice"

	tea "github.com/charmbracelet/bubbletea"
)

const version = "0.1.0"

const help = `tars %s: your terminal AIs working together in a group.

Usage:
  tars            opens tars
  tars --help     shows this help
  tars --version  shows the version

Files:
  ~/.tars/config.toml   keys, who replies without @, voice and disk limits
  ~/.tars/grupos/       your groups and conversations
`

func main() {
	i18n.Set(i18n.Detect(""))
	if len(os.Args) > 1 {
		cfg := config.Default()
		if home, err := os.UserHomeDir(); err == nil {
			dir := home + "/.tars"
			if _, err := os.Stat(config.Path(dir)); err == nil {
				cfg, _ = config.Load(dir)
			}
		}
		i18n.Set(i18n.Detect(cfg.Language))

		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(i18n.T(help, version))
			return
		case "-v", "--version", "version":
			fmt.Println("tars", version)
			return
		}
		fmt.Fprint(os.Stderr, i18n.T("tars: unknown argument %q. See tars --help\n", os.Args[1]))
		os.Exit(2)
	}

	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tars:", err)
		os.Exit(1)
	}
	cfg, err := config.Load(st.Root())
	i18n.Set(i18n.Detect(cfg.Language))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T("tars: using defaults because %v", err))
	}
	sys.SetLimits(cfg.Disk.WarnGB, cfg.Disk.PauseGB, cfg.Disk.StopMB)
	voice.Microphone = cfg.Voice.Microphone

	p := tea.NewProgram(ui.New(st, engines.Detect(), cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tars:", err)
		os.Exit(1)
	}
}
