package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/sys"
	"github.com/dedsfe/tars/internal/usage"

	"github.com/charmbracelet/lipgloss"
)

// Cores que cada IA pode ter no chat.
var palette = []struct{ Name, Hex string }{
	{"orange", "#F5A97F"},
	{"blue", "#8AADF4"},
	{"green", "#A6DA95"},
	{"pink", "#F5BDE6"},
	{"yellow", "#EED49F"},
	{"purple", "#C6A0F6"},
	{"cyan", "#8BD5CA"},
	{"red", "#ED8796"},
}

var (
	cText   = lipgloss.AdaptiveColor{Light: "#1E1E2E", Dark: "#E6E6F0"}
	cMuted  = lipgloss.AdaptiveColor{Light: "#7A7A8C", Dark: "#7F849C"}
	cFaint  = lipgloss.AdaptiveColor{Light: "#B5B5C3", Dark: "#45475A"}
	cAccent = lipgloss.AdaptiveColor{Light: "#4C6EF5", Dark: "#8AADF4"}
	cError  = lipgloss.AdaptiveColor{Light: "#D20F39", Dark: "#ED8796"}
	cWarn   = lipgloss.AdaptiveColor{Light: "#DF8E1D", Dark: "#EED49F"}

	sPage   = lipgloss.NewStyle().Padding(1, 2)
	sTitle  = lipgloss.NewStyle().Bold(true).Foreground(cText)
	sStep   = lipgloss.NewStyle().Foreground(cMuted)
	sLabel  = lipgloss.NewStyle().Foreground(cMuted)
	sActive = lipgloss.NewStyle().Foreground(cAccent).Bold(true)
	sText   = lipgloss.NewStyle().Foreground(cText)
	sMuted  = lipgloss.NewStyle().Foreground(cMuted)
	sFaint  = lipgloss.NewStyle().Foreground(cFaint)
	sMine   = lipgloss.NewStyle().Foreground(cAccent) // barra do lado das suas mensagens
	sError  = lipgloss.NewStyle().Foreground(cError)
	sWarn   = lipgloss.NewStyle().Foreground(cWarn)
	sHelp   = lipgloss.NewStyle().Foreground(cMuted).MarginTop(1)
)

func handleStyle(hex string) lipgloss.Style {
	return lipgloss.NewStyle().Foreground(lipgloss.Color(hex)).Bold(true)
}

func at(handle, hex string) string { return handleStyle(hex).Render("@" + handle) }

// pointer marca a linha selecionada.
func pointer(on bool) string {
	if on {
		return sActive.Render("› ")
	}
	return "  "
}

func check(on bool) string {
	if on {
		return sActive.Render("[x]")
	}
	return sMuted.Render("[ ]")
}

// diskBadge é o espaço livre no topo, com cor conforme o risco.
func diskBadge(free uint64) string {
	if free == 0 {
		return ""
	}
	text := i18n.T("disk %s free", sys.Size(free))
	switch {
	case free < sys.DiskPause:
		return sError.Render(text)
	case free < sys.DiskLow:
		return sWarn.Render(text)
	}
	return sFaint.Render(text)
}

// usageBadge mostra o limite usado de cada IA que informa isso.
func usageBadge(c *usage.Cache, list []engines.Engine) string {
	now := time.Now()
	var parts []string
	for _, e := range list {
		l, ok := c.By[e.ID]
		if !ok {
			if n := c.Today(e.ID); n > 0 {
				parts = append(parts, sFaint.Render(i18n.T("%s %s tokens today", engineName(e.Name), tokens(n))))
			}
			continue
		}
		w, ok := l.Worst(now)
		if !ok {
			continue
		}
		text := fmt.Sprintf("%s %.0f%% %s", engineName(e.Name), w.Used, windowText(w.Name))
		switch {
		case w.Used >= 95:
			parts = append(parts, sError.Render(i18n.T("%s, resets %s", text, when(w.ResetsAt))))
		case w.Used >= 80:
			parts = append(parts, sWarn.Render(i18n.T("%s, resets %s", text, when(w.ResetsAt))))
		default:
			parts = append(parts, sFaint.Render(text))
		}
	}
	return strings.Join(parts, sFaint.Render(" · "))
}

var weekdays = []string{"Sun", "Mon", "Tue", "Wed", "Thu", "Fri", "Sat"}

// when escreve um horário curto: "15:30" hoje, "qui 14:00" em outro dia.
func when(t time.Time) string {
	if t.IsZero() {
		return i18n.T("later")
	}
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return i18n.T(weekdays[t.Weekday()]) + " " + t.Format("15:04")
}

// statusBar junta disco e limites numa linha só, pro topo de toda tela.
func statusBar(disk uint64, c *usage.Cache, list []engines.Engine) string {
	var parts []string
	if d := diskBadge(disk); d != "" {
		parts = append(parts, d)
	}
	if u := usageBadge(c, list); u != "" {
		parts = append(parts, u)
	}
	return strings.Join(parts, sFaint.Render("  ·  "))
}

// tokens escreve contagens grandes de um jeito curto: "850", "12 mil", "1,2 mi".
func tokens(n int64) string {
	switch {
	case n >= 1_000_000:
		return i18n.T("%sM", strings.Replace(fmt.Sprintf("%.1f", float64(n)/1e6), ".", i18n.Decimal(), 1))
	case n >= 1000:
		return i18n.T("%sk", fmt.Sprint(n/1000))
	}
	return fmt.Sprint(n)
}

func modelText(model string) string {
	if model == engines.Default {
		return i18n.T("default")
	}
	return model
}

// engineName e engineNote traduzem os textos do catálogo de IAs.
func engineName(name string) string { return i18n.T(name) }

func engineNote(note string) string { return i18n.T(note) }

func windowText(name string) string {
	if name == "semana" {
		return i18n.T("week")
	}
	return name
}
