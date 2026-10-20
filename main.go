// tars: grupos de IAs de terminal que trabalham juntas.
package main

import (
	"fmt"
	"os"

	"github.com/dedsfe/tars/internal/config"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/store"
	"github.com/dedsfe/tars/internal/sys"
	"github.com/dedsfe/tars/internal/ui"
	"github.com/dedsfe/tars/internal/voice"

	tea "github.com/charmbracelet/bubbletea"
)

const version = "0.1.0"

const help = `tars ` + version + `: suas IAs de terminal trabalhando juntas num grupo.

Uso:
  tars            abre o tars
  tars --help     mostra esta ajuda
  tars --version  mostra a versão

Arquivos:
  ~/.tars/config.toml   teclas, quem responde sem @, voz e limites de disco
  ~/.tars/grupos/       seus grupos e conversas
`

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			fmt.Print(help)
			return
		case "-v", "--version", "version":
			fmt.Println("tars", version)
			return
		}
		fmt.Fprintf(os.Stderr, "tars: não conheço %q. Veja tars --help\n", os.Args[1])
		os.Exit(2)
	}

	st, err := store.Open()
	if err != nil {
		fmt.Fprintln(os.Stderr, "tars:", err)
		os.Exit(1)
	}
	cfg, err := config.Load(st.Root())
	if err != nil {
		fmt.Fprintln(os.Stderr, "tars: usando o padrão porque", err)
	}
	sys.SetLimits(cfg.Disk.WarnGB, cfg.Disk.PauseGB, cfg.Disk.StopMB)
	voice.Microphone = cfg.Voice.Microphone

	p := tea.NewProgram(ui.New(st, engines.Detect(), cfg), tea.WithAltScreen(), tea.WithMouseCellMotion())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "tars:", err)
		os.Exit(1)
	}
}
