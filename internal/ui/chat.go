package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/dedsfe/tars/internal/attach"
	"github.com/dedsfe/tars/internal/chat"
	"github.com/dedsfe/tars/internal/config"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/store"
	"github.com/dedsfe/tars/internal/sys"
	"github.com/dedsfe/tars/internal/usage"
	"github.com/dedsfe/tars/internal/voice"

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
	engine  string
	text    string
	limits  *usage.Limits // limite do plano, quando a IA informa
	tokens  int64
	err     error
}

type voiceDoneMsg struct {
	groupID string
	text    string
	err     error
}

// Mensagens da preparação da voz (instalar ferramentas e baixar o modelo).
type (
	voiceToolsDoneMsg struct {
		groupID string
		err     error
	}
	modelProgressMsg struct {
		groupID     string
		done, total int64
	}
	modelDoneMsg struct {
		groupID string
		err     error
	}
)

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
	visible  bool // você está com esse chat aberto
	pinged   bool // alguma IA te marcou enquanto você estava fora

	disk       uint64       // espaço livre, atualizado pelo App
	limits     *usage.Cache // limite de cada IA, compartilhado com o App
	diskPaused bool
	stopWhy    string // motivo de ter parado todo mundo, quando não foi você

	cfg          config.Config
	voiceSetup   string // "" | "tools" | "model": oferta esperando seu enter
	dl           chan tea.Msg
	dlDone       int64
	dlTotal      int64
	rec          *voice.Recording // gravação da sua voz em andamento
	transcribing bool
	width        int
	height       int
	err          string
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
	// título(3) + status(1) + entrada(2) + sugestão(1) + ajuda(2) + padding(2),
	// mais uma linha de folga pra nunca passar da altura da janela
	c.vp.Height = max(3, height-12)
	c.refresh(true)
}

func (c *chatScreen) Update(msg tea.Msg) (*chatScreen, tea.Cmd, bool) {
	switch msg := msg.(type) {
	case turnDoneMsg:
		return c, c.finish(msg), false
	case voiceToolsDoneMsg:
		if msg.err != nil || len(voice.MissingTools()) > 0 {
			c.err = "A instalação não terminou. Tenta de novo com " + c.cfg.Keys.Voice + "."
			return c, nil, false
		}
		return c, c.toggleVoice(), false
	case modelProgressMsg:
		c.dlDone, c.dlTotal = msg.done, msg.total
		return c, waitFor(c.dl), false
	case modelDoneMsg:
		c.dl = nil
		if msg.err != nil {
			c.err = "Não consegui baixar o modelo: " + msg.err.Error()
			return c, nil, false
		}
		c.err = ""
		return c, c.toggleVoice(), false
	case voiceDoneMsg:
		c.transcribing = false
		if msg.err != nil {
			c.err = "Não consegui transcrever: " + msg.err.Error()
			return c, nil, false
		}
		v := c.input.Value()
		if v != "" && !strings.HasSuffix(v, " ") {
			c.input.InsertString(" ")
		}
		c.input.InsertString(msg.text)
		return c, nil, false
	case spinner.TickMsg:
		if !c.spinning {
			return c, nil, false
		}
		if len(c.running) == 0 && c.rec == nil && !c.transcribing && c.dl == nil {
			c.spinning = false
			return c, nil, false
		}
		var cmd tea.Cmd
		c.spin, cmd = c.spin.Update(msg)
		c.refresh(false)
		return c, cmd, false
	case tea.MouseMsg:
		// a rolagem do trackpad mexe na conversa, não na janela do Terminal
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(msg)
		return c, cmd, false
	case tea.KeyMsg:
		c.err = ""
		switch msg.String() {
		case "enter":
			if c.voiceSetup != "" {
				return c, c.confirmVoiceSetup(), false
			}
		}
		switch msg.String() {
		case c.cfg.Keys.Voice:
			if !c.cfg.Voice.Enabled {
				break
			}
			return c, c.toggleVoice(), false
		case "esc":
			if c.voiceSetup != "" {
				c.voiceSetup = ""
				return c, nil, false
			}
			if c.rec != nil {
				c.rec.Cancel()
				c.rec = nil
				return c, nil, false
			}
			if c.showInfo {
				c.showInfo = false
				return c, nil, false
			}
			return c, nil, true
		case c.cfg.Keys.GroupInfo:
			c.showInfo = !c.showInfo
			return c, nil, false
		case c.cfg.Keys.StopAll:
			return c, c.stopAll(), false
		case "pgup", "pgdown":
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(msg)
			return c, cmd, false
		case c.cfg.Keys.Paste:
			// cola um print copiado como imagem anexada
			path, err := attach.Clipboard(c.st.Attachments(c.g.ID))
			if err != nil {
				c.err = "Não tem imagem copiada. Tira um print com cmd+ctrl+shift+4 ou arrasta o arquivo pra cá."
				return c, nil, false
			}
			c.input.InsertString(path + " ")
			return c, nil, false
		case c.cfg.Keys.Complete:
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
	clean, paths := attach.Extract(text)
	var images []string
	for _, p := range paths {
		saved, err := attach.Save(p, c.st.Attachments(c.g.ID))
		if err != nil {
			c.err = "Não consegui anexar " + filepath.Base(p) + ": " + err.Error()
			return nil
		}
		images = append(images, saved)
	}
	c.post(store.Message{Text: clean, Images: images})
	c.root++
	for _, h := range chat.Targets(clean, c.g, c.cfg.NoMention == config.NoMentionAll) {
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
	if c.disk != 0 && c.disk < sys.DiskPause && len(c.queue) > 0 {
		if !c.diskPaused {
			c.diskPaused = true
			text := fmt.Sprintf("O disco está com %s livre. Pausei as IAs até ter pelo menos %s, pra não travar o Mac. Elas voltam sozinhas quando liberar espaço.",
				sys.Size(c.disk), sys.Size(sys.DiskPause))
			if sys.HasMole() {
				text += " Aperta " + c.cfg.Keys.CleanDisk + " pra limpar com o Mole."
			}
			c.post(store.Message{From: store.System, Text: text})
			chat.Notify(c.g.Name, "IAs pausadas", "Disco com "+sys.Size(c.disk)+" livre.")
		}
		c.refresh(true)
		return nil
	}
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
		case c.overLimit(j.handle):
			rest = append(rest, j) // espera o limite do plano voltar
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
		Images:     chat.Images(c.msgs),
	}
	ctx, groupID := c.ctx, c.g.ID
	return func() tea.Msg {
		res, err := engines.Run(ctx, t)
		return turnDoneMsg{groupID: groupID, handle: j.handle, engine: t.Engine, text: res.Text, limits: res.Limits, tokens: res.Tokens, err: err}
	}
}

// limitReset diz se a IA desse membro bateu o limite do plano e quando volta.
func (c *chatScreen) limitReset(handle string) (time.Time, bool) {
	m, ok := chat.Member(c.g, handle)
	if !ok || c.limits == nil {
		return time.Time{}, false
	}
	l, ok := c.limits.By[m.Engine]
	if !ok {
		return time.Time{}, false
	}
	return l.Exhausted(time.Now())
}

func (c *chatScreen) overLimit(handle string) bool {
	_, over := c.limitReset(handle)
	return over
}

func (c *chatScreen) finish(m turnDoneMsg) tea.Cmd {
	done := c.running[m.handle]
	delete(c.running, m.handle)
	switch {
	case m.err != nil && m.err.Error() == "parado":
		if c.stopWhy != "" {
			c.post(store.Message{From: store.System, Text: "Parei @" + m.handle + " " + c.stopWhy + "."})
		} else {
			c.post(store.Message{From: store.System, Text: "Você parou @" + m.handle + "."})
		}
		if len(c.running) == 0 {
			c.stopWhy = ""
		}
	case m.err != nil:
		c.post(store.Message{From: m.handle, Text: m.err.Error(), Error: true})
	default:
		c.post(store.Message{From: m.handle, Text: m.text})
		if chat.MentionsMe(m.text) {
			chat.Notify(c.g.Name, "@"+m.handle+" te marcou", m.text)
			if !c.visible {
				c.pinged = true
			}
		}
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

// setDisk recebe o espaço livre: pausa, para ou retoma as IAs.
func (c *chatScreen) setDisk(free uint64) tea.Cmd {
	c.disk = free
	if free == 0 {
		return nil
	}
	if free < sys.DiskKill && len(c.running) > 0 && c.stopWhy == "" {
		c.stopWhy = "porque o disco ficou com " + sys.Size(free) + " livre"
		for _, j := range c.running {
			c.enqueue(j) // volta pra fila e continua quando liberar
		}
		c.stop()
		c.ctx, c.stop = context.WithCancel(context.Background())
		return nil
	}
	if c.diskPaused && free >= sys.DiskPause {
		c.diskPaused = false
		c.post(store.Message{From: store.System, Text: "Espaço liberado (" + sys.Size(free) + "). As IAs voltaram."})
		return c.pump()
	}
	return nil
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
		if m.Text == "" {
			body = ""
		}
		switch {
		case m.Error:
			body = sError.Render(lipgloss.NewStyle().Width(width).Render("não consegui responder: " + m.Text))
		case m.From == store.System:
			body = sMuted.Italic(true).Render(body)
		default:
			body = c.paintMentions(body)
		}
		if body != "" {
			b.WriteString(indent(body, "  ") + "\n")
		}
		for _, img := range m.Images {
			b.WriteString("  " + sMuted.Render("imagem ") + sText.Render(filepath.Base(img)) + "\n")
		}
	}
	return b.String()
}

func (c *chatScreen) speaker(from string) string {
	switch from {
	case "":
		return sTitle.Render("você") + sFaint.Render(" @"+chat.Me())
	case store.System:
		return sMuted.Render("tars")
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
		if h == chat.Everyone || h == chat.EveryoneEN || h == chat.Me() {
			return sActive.Render(w)
		}
		return w
	})
}

// toggleVoice começa a gravar ou, se já está gravando, para e transcreve.
func (c *chatScreen) toggleVoice() tea.Cmd {
	root := c.st.Root()
	if c.transcribing {
		return nil
	}
	if c.rec == nil {
		if c.dl != nil {
			return nil // ainda baixando o modelo
		}
		if len(voice.MissingTools()) > 0 {
			c.voiceSetup = "tools"
			return nil
		}
		if !voice.HasModel(root) {
			c.voiceSetup = "model"
			return nil
		}
		rec, err := voice.Record(filepath.Join(root, "audio"))
		if err != nil {
			c.err = "Não consegui gravar: " + err.Error()
			return nil
		}
		c.rec = rec
		return c.startSpin()
	}
	wav, err := c.rec.Stop()
	c.rec = nil
	if err != nil {
		c.err = "Não consegui gravar: " + err.Error()
		return nil
	}
	c.transcribing = true
	groupID := c.g.ID
	hints := []string{chat.Me()}
	for _, m := range c.g.Members {
		hints = append(hints, m.Handle)
	}
	return tea.Batch(c.startSpin(), func() tea.Msg {
		text, err := voice.Transcribe(context.Background(), root, wav, hints)
		return voiceDoneMsg{groupID: groupID, text: text, err: err}
	})
}

// confirmVoiceSetup roda o passo oferecido: instalar pelo Homebrew ou baixar o modelo.
func (c *chatScreen) confirmVoiceSetup() tea.Cmd {
	step := c.voiceSetup
	c.voiceSetup = ""
	groupID := c.g.ID
	switch step {
	case "tools":
		if _, err := exec.LookPath("brew"); err != nil {
			c.err = "Precisa do Homebrew (brew.sh) pra instalar: " + strings.Join(voice.MissingTools(), " ")
			return nil
		}
		args := append([]string{"install"}, voice.MissingTools()...)
		return tea.ExecProcess(exec.Command("brew", args...), func(err error) tea.Msg {
			return voiceToolsDoneMsg{groupID: groupID, err: err}
		})
	case "model":
		ch := make(chan tea.Msg, 1)
		c.dl, c.dlDone, c.dlTotal = ch, 0, 0
		root := c.st.Root()
		go func() {
			last := int64(-1)
			err := voice.Download(context.Background(), root, func(done, total int64) {
				if pct := done * 100 / max(total, 1); pct != last {
					last = pct
					ch <- modelProgressMsg{groupID: groupID, done: done, total: total}
				}
			})
			ch <- modelDoneMsg{groupID: groupID, err: err}
			close(ch)
		}()
		return tea.Batch(waitFor(ch), c.startSpin())
	}
	return nil
}

func waitFor(ch chan tea.Msg) tea.Cmd {
	return func() tea.Msg { return <-ch }
}

func (c *chatScreen) startSpin() tea.Cmd {
	if c.spinning {
		return nil
	}
	c.spinning = true
	return c.spin.Tick
}

func (c *chatScreen) status() string {
	var parts []string
	if c.rec != nil {
		d := time.Since(c.rec.Start).Round(time.Second)
		parts = append(parts, sError.Render(fmt.Sprintf("● gravando %d:%02d", int(d.Minutes()), int(d.Seconds())%60))+
			sMuted.Render("  "+c.cfg.Keys.Voice+" termina · esc descarta"))
	}
	if c.transcribing {
		parts = append(parts, c.spin.View()+sMuted.Render(" transcrevendo sua voz"))
	}
	if c.dl != nil {
		parts = append(parts, c.spin.View()+sMuted.Render(" baixando o modelo de voz ")+
			progressBar(c.dlDone, c.dlTotal, 20)+sMuted.Render(" "+sys.Size(uint64(c.dlDone))))
	}
	switch c.voiceSetup {
	case "tools":
		parts = append(parts, sActive.Render("Pra ouvir sua voz falta instalar "+strings.Join(voice.MissingTools(), " e ")+
			" pelo Homebrew.")+sMuted.Render("  enter instala · esc cancela"))
	case "model":
		parts = append(parts, sActive.Render("Pra ouvir sua voz o tars baixa o Whisper (~550 MB, roda só no seu Mac).")+
			sMuted.Render("  enter baixa · esc cancela"))
	}
	for _, m := range c.g.Members {
		if j, ok := c.running[m.Handle]; ok {
			d := time.Since(j.start).Round(time.Second)
			parts = append(parts, c.spin.View()+" "+at(m.Handle, m.Color)+
				sMuted.Render(fmt.Sprintf(" trabalhando %d:%02d", int(d.Minutes()), int(d.Seconds())%60)))
		}
	}
	for _, j := range c.queue {
		m, ok := chat.Member(c.g, j.handle)
		if !ok {
			continue
		}
		if reset, over := c.limitReset(j.handle); over {
			parts = append(parts, at(m.Handle, m.Color)+sWarn.Render(" esperando o limite voltar ("+when(reset)+")"))
		} else {
			parts = append(parts, at(m.Handle, m.Color)+sMuted.Render(" na fila"))
		}
	}
	if c.diskPaused {
		parts = append([]string{sError.Render("pausado: disco cheio")}, parts...)
	}
	return strings.Join(parts, sFaint.Render("  ·  "))
}

func (c *chatScreen) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(c.g.Name) + "  " + handles(c.g) + "\n")
	b.WriteString(statusBar(c.disk, c.limits, c.engines) + "\n\n")
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
	if _, imgs := attach.Extract(c.input.Value()); len(imgs) > 0 && hint == "" {
		word := "imagem pronta"
		if len(imgs) > 1 {
			word = "imagens prontas"
		}
		hint = sActive.Render(fmt.Sprintf("%d %s pra enviar", len(imgs), word))
	}
	if c.err != "" {
		hint = sError.Render(c.err)
	}
	b.WriteString(hint + "\n")

	k := c.cfg.Keys
	keys := []string{"enter envia", "@ marca alguém"}
	if c.busy() {
		keys = []string{"enter envia", k.StopAll + " para todo mundo"}
	} else {
		if c.cfg.Voice.Enabled {
			keys = append(keys, k.Voice+" fala")
		}
		keys = append(keys, k.Paste+" cola print")
	}
	keys = append(keys, k.GroupInfo+" ver grupo")
	if c.busy() {
		keys = append(keys, "esc sai (eles continuam)")
	} else {
		keys = append(keys, "esc sai")
	}
	help := strings.Join(keys, " · ")
	if c.disk != 0 && c.disk < sys.DiskLow && sys.HasMole() {
		help = sWarn.Render(k.CleanDisk+" limpa o disco com o Mole") + sMuted.Render(" · ") + help
	}
	b.WriteString(sHelp.Render(help))
	return b.String()
}

func progressBar(done, total int64, width int) string {
	if total <= 0 {
		return ""
	}
	filled := int(done * int64(width) / total)
	return sActive.Render(strings.Repeat("━", filled)) + sFaint.Render(strings.Repeat("━", width-filled)) +
		sMuted.Render(fmt.Sprintf(" %d%%", done*100/total))
}

func indent(s, pad string) string {
	return pad + strings.ReplaceAll(s, "\n", "\n"+pad)
}
