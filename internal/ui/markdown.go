package ui

import (
	"regexp"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// As IAs respondem em markdown. No terminal a gente mostra o que importa:
// **negrito** vira negrito, `código` ganha cor, [texto](link) vira só o texto
// e "### título" perde os #.

var (
	linkRe    = regexp.MustCompile(`\[([^\]\n]+)\]\(([^)\s]+)\)`)
	headingRe = regexp.MustCompile(`(?m)^#{1,6}\s+`)

	sBold = lipgloss.NewStyle().Foreground(cText).Bold(true)
	sCode = lipgloss.NewStyle().Foreground(lipgloss.AdaptiveColor{Light: "#7287FD", Dark: "#B7BDF8"})
)

// plainMarkdown tira o que não precisa aparecer, antes de quebrar as linhas.
func plainMarkdown(s string) string {
	s = linkRe.ReplaceAllString(s, "$1")
	return headingRe.ReplaceAllString(s, "")
}

// styleMarkdown aplica negrito e código depois que o texto já foi quebrado
// em linhas. O estado atravessa as linhas: um negrito pode começar numa e
// terminar na outra.
func styleMarkdown(wrapped string) string {
	bold, code := false, false
	lines := strings.Split(wrapped, "\n")
	for i, line := range lines {
		var out, seg strings.Builder
		flush := func() {
			if seg.Len() == 0 {
				return
			}
			switch {
			case code:
				out.WriteString(sCode.Render(seg.String()))
			case bold:
				out.WriteString(sBold.Render(seg.String()))
			default:
				out.WriteString(seg.String())
			}
			seg.Reset()
		}
		for j := 0; j < len(line); j++ {
			switch {
			case !code && strings.HasPrefix(line[j:], "**"):
				flush()
				bold = !bold
				j++
			case line[j] == '`':
				flush()
				code = !code
			default:
				seg.WriteByte(line[j])
			}
		}
		flush()
		lines[i] = out.String()
	}
	return strings.Join(lines, "\n")
}
