package ui

import (
	"bot/internal/sys"

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
