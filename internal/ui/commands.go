package ui

import (
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dedsfe/tars/internal/i18n"
)

// leaveWindow é o tempo pra apertar esc de novo e sair do chat.
const leaveWindow = 1500 * time.Millisecond

type escExpiredMsg struct{}

// cleanDiskMsg pede pro App abrir a limpeza do Mole (o /clean).
type cleanDiskMsg struct{}

// slashRe é um comando com barra: uma palavra só, pra não confundir com caminho de arquivo.
var slashRe = regexp.MustCompile(`^/[a-z]*$`)

type slashCommand struct{ name, desc string }

// Comandos com barra, igual nas IAs de terminal: o que é raro não precisa de atalho.
var slashCommands = []slashCommand{
	{"/info", "group info"},
	{"/activity", "show or hide everything they did"},
	{"/stop", "stop everyone"},
	{"/clean", "clean disk (Mole)"},
	{"/leave", "back to your groups"},
}

// slashSuggestions são os comandos que combinam com o que você está digitando.
func (c *chatScreen) slashSuggestions() []slashCommand {
	v := c.input.Value()
	if !slashRe.MatchString(v) {
		return nil
	}
	return c.slashSuggestionsFor(v)
}

// command roda um comando com barra. leave = voltar pra lista de grupos.
func (c *chatScreen) command(text string) (tea.Cmd, bool) {
	name := text
	if s := c.slashSuggestionsFor(text); len(s) == 1 {
		name = s[0].name // "/inf" vale como /info
	}
	switch name {
	case "/info":
		c.showInfo = true
	case "/activity":
		c.toggleExpand()
	case "/stop":
		c.stopAll()
	case "/clean":
		return func() tea.Msg { return cleanDiskMsg{} }, false
	case "/leave":
		return nil, true
	default:
		names := make([]string, len(slashCommands))
		for i, cm := range slashCommands {
			names[i] = cm.name
		}
		c.err = i18n.T("Unknown command %s. Try: %s", text, strings.Join(names, " "))
	}
	return nil, false
}

func (c *chatScreen) slashSuggestionsFor(text string) []slashCommand {
	var out []slashCommand
	for _, cm := range slashCommands {
		if strings.HasPrefix(cm.name, text) {
			out = append(out, cm)
		}
	}
	return out
}

func (c *chatScreen) backLabel() string {
	label := i18n.T("↑ your last message (%s)", c.cfg.Keys.MyMessage)
	if c.mineHidden() > 0 {
		label = strings.Replace(label, "↑", "↓", 1)
	}
	return sActive.Render(label)
}

// clickedBack diz se o clique caiu no "↑ sua última mensagem" da linha de status.
// A linha fica depois da margem (1), título, barra de status, linha vazia (3) e da conversa.
func (c *chatScreen) clickedBack(m tea.MouseMsg) bool {
	if c.mineHidden() == 0 || c.showInfo {
		return false
	}
	row := 1 + 3 + c.vp.Height
	left := 2 // margem da página
	return m.Y == row && m.X >= left && m.X < left+lipgloss.Width(c.backLabel())
}
