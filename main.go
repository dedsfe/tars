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

const version = "0.1.0"

const help = `bot ` + version + `: suas IAs de terminal trabalhando juntas num grupo.

Uso:
  bot            abre o bot
  bot --help     mostra esta ajuda
  bot --version  mostra a versão

Arquivos:
  ~/.bot/config.toml   teclas, quem responde sem @, voz e limites de disco
  ~/.bot/grupos/       seus grupos e conversas
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(help)
			return
		case "-v", "--version", "version":
			fmt.Println("bot", version)
			return
		}
		fmt.Fprintf(os.Stderr, "bot: não conheço %q. Veja bot --help\n", os.Args[1])
		os.Exit(2)
	}

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
