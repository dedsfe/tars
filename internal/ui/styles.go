package ui

import (
	"fmt"
	"strings"
	"time"

	"bot/internal/engines"
	"bot/internal/sys"
	"bot/internal/usage"

	"github.com/charmbracelet/lipgloss"
)

// Cores que cada IA pode ter no chat.
var palette = []struct{ Name, Hex string }{
	{"laranja", "#F5A97F"},
	{"azul", "#8AADF4"},
	{"verde", "#A6DA95"},
	{"rosa", "#F5BDE6"},
	{"amarelo", "#EED49F"},
	{"roxo", "#C6A0F6"},
	{"ciano", "#8BD5CA"},
	{"vermelho", "#ED8796"},
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
	text := "disco " + sys.Size(free) + " livre"
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
				parts = append(parts, sFaint.Render(e.Name+" "+tokens(n)+" tokens hoje"))
			}
			continue
		}
		w, ok := l.Worst(now)
		if !ok {
			continue
		}
		text := fmt.Sprintf("%s %.0f%% %s", e.Name, w.Used, w.Name)
		switch {
		case w.Used >= 95:
			parts = append(parts, sError.Render(text+", volta "+when(w.ResetsAt)))
		case w.Used >= 80:
			parts = append(parts, sWarn.Render(text+", volta "+when(w.ResetsAt)))
		default:
			parts = append(parts, sFaint.Render(text))
		}
	}
	return strings.Join(parts, sFaint.Render(" · "))
}

var weekdays = []string{"dom", "seg", "ter", "qua", "qui", "sex", "sáb"}

// when escreve um horário curto: "15:30" hoje, "qui 14:00" em outro dia.
func when(t time.Time) string {
	if t.IsZero() {
		return "depois"
	}
	now := time.Now()
	if t.YearDay() == now.YearDay() && t.Year() == now.Year() {
		return t.Format("15:04")
	}
	return weekdays[t.Weekday()] + " " + t.Format("15:04")
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
		return strings.Replace(fmt.Sprintf("%.1f mi", float64(n)/1e6), ".", ",", 1)
	case n >= 1000:
		return fmt.Sprintf("%d mil", n/1000)
	}
	return fmt.Sprint(n)
}
