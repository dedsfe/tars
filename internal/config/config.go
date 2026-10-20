// Package config lê ~/.tars/config.toml. Se o arquivo não existe, ele é
// criado com os valores padrão e comentários explicando cada opção.
package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/BurntSushi/toml"
)

// Quem responde quando você manda mensagem sem marcar ninguém.
const (
	NoMentionLead = "lead" // só o líder, que divide o trabalho
	NoMentionAll  = "all"  // todo mundo do grupo
)

type Config struct {
	Language  string `toml:"language"`
	NoMention string `toml:"no_mention"`
	// MaxAgentTurns limita quantas vezes as IAs falam seguidas depois de
	// uma mensagem sua, pra conversa não virar loop.
	MaxAgentTurns int   `toml:"max_agent_turns"`
	Voice         Voice `toml:"voice"`
	Disk          Disk  `toml:"disk"`
	Keys          Keys  `toml:"keys"`
}

type Voice struct {
	Enabled    bool   `toml:"enabled"`
	Microphone string `toml:"microphone"` // vazio = microfone do Mac
}

type Disk struct {
	WarnGB  float64 `toml:"warn_gb"`
	PauseGB float64 `toml:"pause_gb"`
	StopMB  float64 `toml:"stop_mb"`
	Mole    string  `toml:"mole"` // "inside" ou "window"
}

// Onde a limpeza do Mole roda.
const (
	MoleInside = "inside" // no lugar do tars, que volta quando o Mole fecha
	MoleWindow = "window" // numa janela nova do terminal, com o tars aberto
)

// Keys são os atalhos do chat, no formato do Bubble Tea ("ctrl+r", "alt+v"...).
type Keys struct {
	Voice     string `toml:"voice"`
	Paste     string `toml:"paste_image"`
	StopAll   string `toml:"stop_all"`
	GroupInfo string `toml:"group_info"`
	CleanDisk string `toml:"clean_disk"`
	Complete  string `toml:"complete_mention"`
}

func Default() Config {
	return Config{
		Language:      "auto",
		NoMention:     NoMentionLead,
		MaxAgentTurns: 20,
		Voice:         Voice{Enabled: true},
		Disk:          Disk{WarnGB: 5, PauseGB: 2, StopMB: 500, Mole: MoleInside},
		Keys: Keys{
			Voice:     "ctrl+r",
			Paste:     "ctrl+v",
			StopAll:   "ctrl+x",
			GroupInfo: "ctrl+g",
			CleanDisk: "ctrl+l",
			Complete:  "tab",
		},
	}
}

const template = `# Language: auto, en, pt, es, fr, de, it, ja, zh.
# language = "auto"

# Configuração do tars. Apague uma linha pra voltar ao padrão.
# tars settings. Delete a line to go back to the default.

# Quem responde quando você não marca ninguém: "lead" (só o líder) ou "all" (todos).
# Who answers when you mention nobody: "lead" or "all".
no_mention = "lead"

# Quantas vezes as IAs podem falar seguidas depois de uma mensagem sua.
# How many times the AIs may talk in a row after one message from you.
max_agent_turns = 20

[voice]
# ctrl+r grava sua voz e transcreve no Mac (Whisper, ~550 MB na primeira vez).
# Record your voice and transcribe it locally (Whisper, ~550 MB on first use).
enabled = true
# Vazio = microfone do Mac. Ou o número/nome que aparece em:
#   ffmpeg -f avfoundation -list_devices true -i ""
microphone = ""

[disk]
warn_gb = 5    # barra fica amarela / bar turns yellow
pause_gb = 2   # IAs param de começar trabalho novo / agents stop starting work
stop_mb = 500  # quem está trabalhando é parado / running agents are stopped
# Onde a limpeza do Mole abre: "inside" (no lugar do tars, que volta quando
# você sai do Mole) ou "window" (janela nova, o tars continua aberto).
# Where Mole cleanup opens: "inside" (tars comes back after) or "window".
mole = "inside"

[keys]
voice = "ctrl+r"
paste_image = "ctrl+v"
stop_all = "ctrl+x"
group_info = "ctrl+g"
clean_disk = "ctrl+l"
complete_mention = "tab"
`

// Load lê a configuração de dir/config.toml, criando o arquivo se faltar.
// Valores ausentes ou inválidos ficam com o padrão.
func Load(dir string) (Config, error) {
	cfg := Default()
	path := filepath.Join(dir, "config.toml")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		_ = os.MkdirAll(dir, 0o755)
		_ = os.WriteFile(path, []byte(template), 0o644)
		return cfg, nil
	}
	if _, err := toml.DecodeFile(path, &cfg); err != nil {
		return Default(), fmt.Errorf("config.toml: %w", err)
	}
	def := Default()
	if cfg.NoMention != NoMentionAll {
		cfg.NoMention = NoMentionLead
	}
	fill := func(v *string, d string) {
		if strings.TrimSpace(*v) == "" {
			*v = d
		}
	}
	fill(&cfg.Keys.Voice, def.Keys.Voice)
	fill(&cfg.Keys.Paste, def.Keys.Paste)
	fill(&cfg.Keys.StopAll, def.Keys.StopAll)
	fill(&cfg.Keys.GroupInfo, def.Keys.GroupInfo)
	fill(&cfg.Keys.CleanDisk, def.Keys.CleanDisk)
	fill(&cfg.Keys.Complete, def.Keys.Complete)
	if cfg.MaxAgentTurns <= 0 {
		cfg.MaxAgentTurns = def.MaxAgentTurns
	}
	if cfg.Disk.WarnGB <= 0 {
		cfg.Disk.WarnGB = def.Disk.WarnGB
	}
	if cfg.Disk.PauseGB <= 0 {
		cfg.Disk.PauseGB = def.Disk.PauseGB
	}
	if cfg.Disk.Mole != MoleWindow {
		cfg.Disk.Mole = MoleInside
	}
	if cfg.Disk.StopMB <= 0 {
		cfg.Disk.StopMB = def.Disk.StopMB
	}
	return cfg, nil
}

// Path é onde o arquivo de configuração fica.
func Path(dir string) string { return filepath.Join(dir, "config.toml") }
