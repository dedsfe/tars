package ui

import (
	"hash/fnv"
	"path/filepath"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/store"
)

// activityMsg é uma coisa que uma IA acabou de fazer, chegando ao vivo.
type activityMsg struct {
	groupID string
	handle  string
	act     engines.Activity
	at      time.Time
}

// logLine é uma linha do painel "o que eles estão fazendo".
type logLine struct {
	handle string
	at     time.Time
	act    engines.Activity
}

const maxLog = 400

// onActivity guarda a ação da IA pra mostrar ao vivo e pra anexar na resposta dela.
func (c *chatScreen) onActivity(m activityMsg) tea.Cmd {
	if _, ok := c.running[m.handle]; ok {
		c.acts[m.handle] = append(c.acts[m.handle], m.act)
	}
	c.log = append(c.log, logLine{handle: m.handle, at: m.at, act: m.act})
	if len(c.log) > maxLog {
		c.log = c.log[len(c.log)-maxLog:]
	}
	return waitFor(c.act)
}

// thinking são as palavrinhas que aparecem enquanto a IA trabalha.
var thinking = map[string][]string{
	"en": {"thinking", "pondering", "cogitating", "interpreting", "mulling it over", "scheming", "tinkering", "noodling", "brewing", "connecting the dots", "ruminating", "puzzling"},
	"pt": {"pensando", "cogitando", "interpretando", "matutando", "maquinando", "ruminando", "fuçando", "tramando", "destrinchando", "ponderando", "ligando os pontos", "caraminholando"},
	"es": {"pensando", "cavilando", "interpretando", "rumiando", "maquinando", "tramando", "trasteando", "atando cabos", "meditando", "ponderando"},
	"fr": {"réfléchit", "cogite", "interprète", "rumine", "mijote", "trame", "bricole", "relie les points", "médite", "pèse le pour et le contre"},
	"de": {"denkt nach", "grübelt", "interpretiert", "tüftelt", "brütet", "heckt etwas aus", "sinniert", "verbindet die Punkte", "wägt ab", "knobelt"},
	"it": {"pensando", "cogitando", "interpretando", "rimuginando", "macchinando", "tramando", "armeggiando", "unendo i puntini", "meditando", "ponderando"},
	"ja": {"考え中", "思案中", "解釈中", "熟考中", "企み中", "いじり中", "点と点をつなぎ中", "ひらめき待ち", "練り中", "整理中"},
	"zh": {"思考中", "琢磨中", "理解中", "酝酿中", "盘算中", "捣鼓中", "串联线索", "推敲中", "沉思中", "梳理中"},
}

// thinkingWord troca a cada 3 segundos, e cada IA começa numa palavra diferente.
func thinkingWord(handle string, since time.Duration) string {
	words := thinking[i18n.Lang()]
	if len(words) == 0 {
		words = thinking["en"]
	}
	h := fnv.New32a()
	h.Write([]byte(handle))
	return words[(int(h.Sum32())+int(since.Seconds()/3))%len(words)] + "…"
}

// actLabel é a ação ao vivo, no gerúndio: "editando index.html".
func (c *chatScreen) actLabel(a engines.Activity, width int) string {
	detail := truncate(oneLine(c.shortPath(a)), max(12, width))
	switch a.Kind {
	case engines.ActEdit:
		return i18n.T("editing %s", detail)
	case engines.ActWrite:
		return i18n.T("creating %s", detail)
	case engines.ActDelete:
		return i18n.T("deleting %s", detail)
	case engines.ActRead:
		return i18n.T("reading %s", detail)
	case engines.ActRun:
		return i18n.T("running %s", detail)
	case engines.ActSearch:
		return i18n.T("searching %s", detail)
	case engines.ActWeb:
		return i18n.T("browsing %s", detail)
	}
	return i18n.T("using %s", detail)
}

// actMark é o símbolo de cada ação no histórico da conversa.
func actMark(kind string) string {
	switch kind {
	case engines.ActEdit:
		return "✎"
	case engines.ActWrite:
		return "+"
	case engines.ActDelete:
		return "−"
	case engines.ActRead:
		return "◦"
	case engines.ActRun:
		return "$"
	case engines.ActSearch:
		return "⌕"
	case engines.ActWeb:
		return "↗"
	}
	return "•"
}

// shortPath mostra arquivos da pasta do grupo sem o caminho todo.
func (c *chatScreen) shortPath(a engines.Activity) string {
	switch a.Kind {
	case engines.ActEdit, engines.ActWrite, engines.ActDelete, engines.ActRead:
		if rel, err := filepath.Rel(c.g.Folder, a.Detail); err == nil && !strings.HasPrefix(rel, "..") && filepath.IsAbs(a.Detail) {
			return rel
		}
		return tildify(a.Detail)
	}
	return a.Detail
}

func toActions(acts []engines.Activity) []store.Action {
	out := make([]store.Action, len(acts))
	for i, a := range acts {
		out[i] = store.Action{Kind: a.Kind, Detail: a.Detail}
	}
	return out
}

// renderActions resume, embaixo da fala, o que a IA mexeu: arquivos sem repetir e comandos.
// Leituras ficam só no painel, pra não poluir a conversa.
func (c *chatScreen) renderActions(acts []store.Action, width int) string {
	seen := map[string]bool{}
	var parts []string
	for _, a := range acts {
		if a.Kind == engines.ActRead {
			continue
		}
		item := actMark(a.Kind) + " " + truncate(oneLine(c.shortPath(engines.Activity{Kind: a.Kind, Detail: a.Detail})), 48)
		if seen[item] {
			continue
		}
		seen[item] = true
		parts = append(parts, item)
	}
	if len(parts) == 0 {
		return ""
	}
	const most = 10
	if len(parts) > most {
		parts = append(parts[:most], i18n.T("+%d more", len(parts)-most))
	}
	text := lipgloss.NewStyle().Width(width).Render(strings.Join(parts, "  "))
	return sFaint.Render(text)
}

// viewLog é o painel que mostra tudo que eles estão fazendo, do mais antigo pro mais novo.
func (c *chatScreen) viewLog(height int) string {
	var b strings.Builder
	b.WriteString(sLabel.Render(i18n.T("What they are doing")) + sMuted.Render("  "+i18n.T("%s back to chat", c.cfg.Keys.Activity)) + "\n\n")
	if len(c.log) == 0 {
		b.WriteString(sMuted.Render(i18n.T("Nothing yet. Files they touch and commands they run show up here as it happens.")) + "\n")
		return b.String()
	}
	rows := c.log
	if room := height - 2; len(rows) > room {
		rows = rows[len(rows)-room:]
	}
	for _, l := range rows {
		color := ""
		if m, ok := memberOf(c.g, l.handle); ok {
			color = m.Color
		}
		line := sFaint.Render(l.at.Format("15:04:05")) + "  " + at(l.handle, color) + "  " +
			sText.Render(actMark(l.act.Kind)+" ") + sText.Render(c.actLabel(l.act, c.vp.Width-30))
		b.WriteString(truncateANSI(line, c.vp.Width) + "\n")
	}
	return b.String()
}

func memberOf(g store.Group, handle string) (store.Member, bool) {
	for _, m := range g.Members {
		if m.Handle == handle {
			return m, true
		}
	}
	return store.Member{}, false
}

func truncateANSI(s string, width int) string {
	if lipgloss.Width(s) <= width {
		return s
	}
	return lipgloss.NewStyle().MaxWidth(width).Render(s)
}
