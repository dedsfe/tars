package ui

import (
	"context"
	"fmt"
	"os"
	"regexp"
	"slices"
	"strings"
	"time"

	"bot/internal/chat"
	"bot/internal/engines"
	"bot/internal/store"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// job é uma vez que alguém precisa falar.
type job struct {
	handle string
	by     string // como a IA lê quem chamou: "você", "@apelido"...
	caller string // apelido da IA que chamou; vazio quando foi você
	reply  bool   // é a volta de um resultado; não chama ninguém de volta
	root   int    // mensagem sua que começou essa rodada
	start  time.Time
}

type turnDoneMsg struct {
	groupID string
	handle  string
	text    string
	err     error
}

type chatScreen struct {
	st      *store.Store
	g       store.Group
	engines []engines.Engine

	msgs    []store.Message
	running map[string]job // quem está trabalhando agora
	queue   []job
	budget  map[int]int // rodada -> quantas vezes as IAs já falaram
	root    int
	ctx     context.Context
	stop    context.CancelFunc

	vp       viewport.Model
	input    textarea.Model
	spin     spinner.Model
	spinning bool
	showInfo bool
	width    int
	height   int
	err      string
}

func newChat(st *store.Store, g store.Group, list []engines.Engine, width, height int) *chatScreen {
	c := &chatScreen{st: st, g: g, engines: list, running: map[string]job{}, budget: map[int]int{}}
	c.ctx, c.stop = context.WithCancel(context.Background())

	msgs, err := st.Messages(g.ID)
	if err != nil {
		c.err = "Não consegui ler a conversa: " + err.Error()
	}
	c.msgs = msgs

	c.input = textarea.New()
	c.input.Placeholder = "Mensagem pro grupo"
	c.input.ShowLineNumbers = false
	c.input.SetPromptFunc(2, func(line int) string {
		if line == 0 {
			return "› "
		}
		return "  "
	})
	c.input.FocusedStyle.Prompt = sActive
	c.input.CharLimit = 8000
	c.input.SetHeight(2)
	c.input.FocusedStyle.CursorLine = lipgloss.NewStyle()
	c.input.KeyMap.InsertNewline.SetKeys("alt+enter", "ctrl+j")
	c.input.Focus()

	c.spin = spinner.New(spinner.WithSpinner(spinner.MiniDot), spinner.WithStyle(sActive))
	c.vp = viewport.New(0, 0)
	c.vp.KeyMap = viewport.KeyMap{
		PageUp:   c.vp.KeyMap.PageUp,
		PageDown: c.vp.KeyMap.PageDown,
	}
	c.resize(width, height)
	return c
}

func (c *chatScreen) isRunning(h string) bool { _, ok := c.running[h]; return ok }

func (c *chatScreen) busy() bool { return len(c.running) > 0 || len(c.queue) > 0 }

func (c *chatScreen) resize(width, height int) {
	c.width, c.height = width, height
	inner := max(20, width-4) // padding da página
	c.input.SetWidth(inner)
	c.vp.Width = inner
	// título(2) + status(1) + entrada(2) + sugestão(1) + ajuda(2) + padding(2)
	c.vp.Height = max(3, height-10)
	c.refresh(true)
}

func (c *chatScreen) Update(msg tea.Msg) (*chatScreen, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case turnDoneMsg:
		return c, c.finish(msg), false
	case spinner.TickMsg:
		if !c.spinning {
			return c, nil, false
		}
		if len(c.running) == 0 {
			c.spinning = false
			return c, nil, false
		}
		var cmd tea.Cmd
		c.spin, cmd = c.spin.Update(msg)
		c.refresh(false)
		return c, cmd, false
	case tea.KeyMsg:
		c.err = ""
		switch msg.String() {
		case "esc":
			if c.showInfo {
				c.showInfo = false
				return c, nil, false
			}
			return c, nil, true
		case "ctrl+g":
			c.showInfo = !c.showInfo
			return c, nil, false
		case "ctrl+x":
			return c, c.stopAll(), false
		case "pgup", "pgdown":
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(msg)
			return c, cmd, false
		case "tab":
			c.complete()
			return c, nil, false
		case "enter":
			text := strings.TrimSpace(c.input.Value())
			if text == "" {
				return c, nil, false
			}
			c.input.Reset()
			return c, c.send(text), false
		}
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd, false
}

// --- quem fala ---

func (c *chatScreen) send(text string) tea.Cmd {
	if info, err := os.Stat(c.g.Folder); err != nil || !info.IsDir() {
		c.err = "A pasta do grupo não existe mais: " + tildify(c.g.Folder)
		return nil
	}
	c.post(store.Message{Text: text})
	c.root++
	for _, h := range chat.Targets(text, c.g) {
		c.enqueue(job{handle: h, by: "você", root: c.root})
	}
	return c.pump()
}

func (c *chatScreen) enqueue(j job) {
	for _, q := range c.queue {
		if q.handle == j.handle && q.root == j.root {
			return
		}
	}
	c.queue = append(c.queue, j)
}

// pump acorda quem está na fila e não está ocupado.
func (c *chatScreen) pump() tea.Cmd {
	var cmds []tea.Cmd
	rest := c.queue[:0]
	stopped := map[int]bool{}
	for _, j := range c.queue {
		switch {
		case c.budget[j.root] >= chat.MaxAgentTurns:
			if !stopped[j.root] {
				stopped[j.root] = true
				c.post(store.Message{From: store.System, Text: fmt.Sprintf(
					"As IAs já falaram %d vezes seguidas. Parei aqui pra não virar loop. Manda uma mensagem pra continuar.",
					chat.MaxAgentTurns)})
			}
		case c.isRunning(j.handle):
			rest = append(rest, j)
		default:
			j.start = time.Now()
			c.running[j.handle] = j
			c.budget[j.root]++
			cmds = append(cmds, c.turn(j))
		}
	}
	c.queue = rest
	if len(c.running) > 0 && !c.spinning {
		c.spinning = true
		cmds = append(cmds, c.spin.Tick)
	}
	c.refresh(true)
	return tea.Batch(cmds...)
}

func (c *chatScreen) turn(j job) tea.Cmd {
	me, _ := chat.Member(c.g, j.handle)
	t := engines.Turn{
		Engine:     me.Engine,
		Model:      me.Model,
		Folder:     c.g.Folder,
		FullAccess: me.FullAccess,
		Prompt:     chat.Prompt(c.g, me, j.by, append([]store.Message(nil), c.msgs...)),
	}
	ctx, groupID := c.ctx, c.g.ID
	return func() tea.Msg {
		text, err := engines.Run(ctx, t)
		return turnDoneMsg{groupID: groupID, handle: j.handle, text: text, err: err}
	}
}

func (c *chatScreen) finish(m turnDoneMsg) tea.Cmd {
	done := c.running[m.handle]
	delete(c.running, m.handle)
	switch {
	case m.err != nil && m.err.Error() == "parado":
		c.post(store.Message{From: store.System, Text: "Você parou @" + m.handle + "."})
	case m.err != nil:
		c.post(store.Message{From: m.handle, Text: m.err.Error(), Error: true})
	default:
		c.post(store.Message{From: m.handle, Text: m.text})
		called := chat.Mentions(m.text, c.g, m.handle)
		for _, h := range called {
			c.enqueue(job{handle: h, by: "@" + m.handle, caller: m.handle, root: done.root})
		}
		// quem pediu algo pra outra IA é chamado de volta com o resultado
		if done.caller != "" && !done.reply && !slices.Contains(called, done.caller) {
			c.enqueue(job{handle: done.caller, by: "@" + m.handle + " (respondendo ao que você pediu)",
				caller: m.handle, reply: true, root: done.root})
		}
	}
	return c.pump()
}

func (c *chatScreen) stopAll() tea.Cmd {
	if !c.busy() {
		return nil
	}
	c.queue = nil
	c.stop()
	c.ctx, c.stop = context.WithCancel(context.Background())
	return nil
}

func (c *chatScreen) post(m store.Message) {
	m.At = time.Now()
	c.msgs = append(c.msgs, m)
	if err := c.st.Append(c.g.ID, m); err != nil {
		c.err = "Não consegui salvar a conversa: " + err.Error()
	}
	c.refresh(true)
}

// --- @ ---

var lastWordRe = regexp.MustCompile(`@([a-z0-9_-]*)$`)

// suggestions são os apelidos que combinam com o @ sendo digitado.
func (c *chatScreen) suggestions() []store.Member {
	m := lastWordRe.FindStringSubmatch(strings.ToLower(c.input.Value()))
	if m == nil {
		return nil
	}
	var out []store.Member
	for _, mem := range c.g.Members {
		if strings.HasPrefix(mem.Handle, m[1]) {
			out = append(out, mem)
		}
	}
	if strings.HasPrefix(chat.Everyone, m[1]) {
		out = append(out, store.Member{Handle: chat.Everyone, Color: string(cMuted.Dark)})
	}
	return out
}

func (c *chatScreen) complete() {
	s := c.suggestions()
	if len(s) == 0 {
		return
	}
	v := c.input.Value()
	i := strings.LastIndex(v, "@")
	c.input.SetValue(v[:i] + "@" + s[0].Handle + " ")
	c.input.CursorEnd()
}

// --- tela ---

func (c *chatScreen) refresh(follow bool) {
	atBottom := c.vp.AtBottom()
	c.vp.SetContent(c.renderMessages())
	if follow || atBottom {
		c.vp.GotoBottom()
	}
}

func (c *chatScreen) renderMessages() string {
	if len(c.msgs) == 0 {
		lead, _ := chat.Member(c.g, c.g.Lead)
		return sMuted.Render("Manda a primeira mensagem. Sem @ quem responde é ") +
			at(lead.Handle, lead.Color) + sMuted.Render(". Use @todos pra chamar todo mundo.")
	}
	width := max(10, c.vp.Width-2)
	var b strings.Builder
	for i, m := range c.msgs {
		if i > 0 {
			b.WriteString("\n")
		}
		b.WriteString(c.speaker(m.From) + "  " + sFaint.Render(m.At.Format("15:04")) + "\n")
		body := lipgloss.NewStyle().Width(width).Render(m.Text)
		switch {
		case m.Error:
			body = sError.Render(lipgloss.NewStyle().Width(width).Render("não consegui responder: " + m.Text))
		case m.From == store.System:
			body = sMuted.Italic(true).Render(body)
		default:
			body = c.paintMentions(body)
		}
		b.WriteString(indent(body, "  ") + "\n")
	}
	return b.String()
}

func (c *chatScreen) speaker(from string) string {
	switch from {
	case "":
		return sTitle.Render("você")
	case store.System:
		return sMuted.Render("bot")
	}
	if m, ok := chat.Member(c.g, from); ok {
		return at(m.Handle, m.Color)
	}
	return sMuted.Render("@" + from)
}

var mentionRe = regexp.MustCompile(`@[A-Za-z0-9][A-Za-z0-9_-]*`)

func (c *chatScreen) paintMentions(s string) string {
	return mentionRe.ReplaceAllStringFunc(s, func(w string) string {
		h := strings.ToLower(w[1:])
		if m, ok := chat.Member(c.g, h); ok {
			return at(m.Handle, m.Color)
		}
		if h == chat.Everyone {
			return sActive.Render(w)
		}
		return w
	})
}

func (c *chatScreen) status() string {
	var parts []string
	for _, m := range c.g.Members {
		if j, ok := c.running[m.Handle]; ok {
			d := time.Since(j.start).Round(time.Second)
			parts = append(parts, c.spin.View()+" "+at(m.Handle, m.Color)+
				sMuted.Render(fmt.Sprintf(" trabalhando %d:%02d", int(d.Minutes()), int(d.Seconds())%60)))
		}
	}
	for _, j := range c.queue {
		if m, ok := chat.Member(c.g, j.handle); ok {
			parts = append(parts, at(m.Handle, m.Color)+sMuted.Render(" na fila"))
		}
	}
	return strings.Join(parts, sFaint.Render("  ·  "))
}

func (c *chatScreen) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(c.g.Name) + "  " + handles(c.g) + "\n\n")
	if c.showInfo {
		b.WriteString(groupSummary(c.g, c.engines, c.vp.Width))
		b.WriteString(sHelp.Render("esc volta pra conversa"))
		return b.String()
	}
	b.WriteString(lipgloss.NewStyle().Height(c.vp.Height).Render(c.vp.View()) + "\n")
	b.WriteString(c.status() + "\n")
	b.WriteString(c.input.View() + "\n")

	hint := ""
	if s := c.suggestions(); len(s) > 0 {
		names := make([]string, len(s))
		for i, m := range s {
			names[i] = at(m.Handle, m.Color)
		}
		hint = sMuted.Render("tab completa  ") + strings.Join(names, " ")
	}
	if c.err != "" {
		hint = sError.Render(c.err)
	}
	b.WriteString(hint + "\n")

	help := "enter envia · @ marca alguém · pgup/pgdn rola · ctrl+g ver grupo · esc sai"
	if c.busy() {
		help = "enter envia · ctrl+x para todo mundo · ctrl+g ver grupo · esc sai (eles continuam)"
	}
	b.WriteString(sHelp.Render(help))
	return b.String()
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
