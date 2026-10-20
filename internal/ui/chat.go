package ui

import (
	"context"
	"errors"
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
	"github.com/dedsfe/tars/internal/i18n"
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
	cancel context.CancelFunc // para só essa fala (enviar agora)
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
	budget  map[int]int  // rodada -> quantas vezes as IAs já falaram
	stopped map[int]bool // rodadas em que o limite de falas já foi avisado
	root    int
	ctx     context.Context
	stop    context.CancelFunc
	cut     map[string]bool // quem foi interrompido pra ler sua mensagem nova
	spoke   bool            // alguma IA respondeu desde que o grupo ficou parado
	unseen  bool            // chegou mensagem enquanto você lia lá em cima

	act       chan tea.Msg                  // o que as IAs fazem, chegando ao vivo
	listening bool                          // já tem alguém escutando o canal
	acts      map[string][]engines.Activity // o que cada uma fez na fala atual
	expand    bool                          // ctrl+o: mostra todas as ações na conversa
	hist      string                        // conversa já desenhada (muda só com mensagem nova)
	histW     int                           // largura em que hist foi desenhada
	mine      []int                         // linha onde começa cada mensagem sua
	escAt     time.Time                     // último esc: outro logo depois sai do chat

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
	c := &chatScreen{st: st, g: g, engines: list, running: map[string]job{}, budget: map[int]int{}, stopped: map[int]bool{}, cut: map[string]bool{},
		acts: map[string][]engines.Activity{}, act: make(chan tea.Msg, 512)}
	c.ctx, c.stop = context.WithCancel(context.Background())

	msgs, err := st.Messages(g.ID)
	if err != nil {
		c.err = i18n.T("Could not read the chat: %v", err)
	}
	c.msgs = msgs

	c.input = textarea.New()
	c.input.Placeholder = i18n.T("Message the group")
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
	case activityMsg:
		return c, c.onActivity(msg), false
	case escExpiredMsg:
		if time.Since(c.escAt) >= leaveWindow {
			c.escAt = time.Time{}
		}
		return c, nil, false
	case voiceToolsDoneMsg:
		if msg.err != nil || len(voice.MissingTools()) > 0 {
			c.err = i18n.T("Setup did not finish. Try again with %s.", c.cfg.Keys.Voice)
			return c, nil, false
		}
		// o tea.ExecProcess desliga o mouse e não religa: sem isso a rolagem vai pro Terminal
		return c, tea.Batch(c.toggleVoice(), tea.EnableMouseCellMotion), false
	case modelProgressMsg:
		c.dlDone, c.dlTotal = msg.done, msg.total
		return c, waitFor(c.dl), false
	case modelDoneMsg:
		c.dl = nil
		if msg.err != nil {
			c.err = i18n.T("Could not download the model: %v", msg.err)
			return c, nil, false
		}
		c.err = ""
		return c, c.toggleVoice(), false
	case voiceDoneMsg:
		c.transcribing = false
		if msg.err != nil {
			c.err = i18n.T("Could not transcribe: %v", msg.err)
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
		if msg.Action == tea.MouseActionPress && msg.Button == tea.MouseButtonLeft && c.clickedBack(msg) {
			c.backToMine()
			return c, nil, false
		}
		// a rolagem do trackpad mexe na conversa, não na janela do Terminal
		var cmd tea.Cmd
		c.vp, cmd = c.vp.Update(msg)
		c.seen()
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
		case c.cfg.Keys.Stop, "esc":
			// primeiro fecha o que estiver aberto
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
			if msg.String() != c.cfg.Keys.Stop {
				return c, nil, false
			}
			// igual nas IAs de terminal: uma vez para quem está trabalhando, duas vezes sai
			if !c.escAt.IsZero() && time.Since(c.escAt) < leaveWindow {
				c.escAt = time.Time{}
				return c, nil, true
			}
			c.escAt = time.Now()
			c.stopAll()
			return c, tea.Tick(leaveWindow, func(time.Time) tea.Msg { return escExpiredMsg{} }), false
		case c.cfg.Keys.Leave:
			return c, nil, true // eles continuam trabalhando
		case c.cfg.Keys.GroupInfo:
			c.showInfo = !c.showInfo
			return c, nil, false
		case c.cfg.Keys.Activity:
			c.toggleExpand()
			return c, nil, false
		case c.cfg.Keys.MyMessage:
			c.backToMine()
			return c, nil, false
		case "pgup", "pgdown":
			var cmd tea.Cmd
			c.vp, cmd = c.vp.Update(msg)
			c.seen()
			return c, cmd, false
		case "up", "down":
			// com a caixa vazia, as setas rolam a conversa
			if c.input.Value() == "" {
				if msg.String() == "up" {
					c.vp.ScrollUp(3)
				} else {
					c.vp.ScrollDown(3)
				}
				c.seen()
				return c, nil, false
			}
		case "alt+up", "shift+up":
			c.vp.ScrollUp(3) // rolagem fina pelo teclado, igual ao trackpad
			return c, nil, false
		case "alt+down", "shift+down":
			c.vp.ScrollDown(3)
			c.seen()
			return c, nil, false
		case c.cfg.Keys.Paste:
			// cola um print copiado como imagem anexada
			path, err := attach.Clipboard(c.st.Attachments(c.g.ID))
			if err != nil {
				c.err = i18n.T("No image copied. Take a screenshot with cmd+ctrl+shift+4 or drag a file here.")
				return c, nil, false
			}
			c.input.InsertString(path + " ")
			return c, nil, false
		case c.cfg.Keys.Complete:
			c.complete()
			return c, nil, false
		case "enter", c.cfg.Keys.SendNow:
			text := strings.TrimSpace(c.input.Value())
			if text == "" {
				return c, nil, false
			}
			if slashRe.MatchString(text) {
				c.input.Reset()
				cmd, leave := c.command(text)
				return c, cmd, leave
			}
			c.input.Reset()
			return c, c.send(text, msg.String() == c.cfg.Keys.SendNow), false
		}
	}
	var cmd tea.Cmd
	c.input, cmd = c.input.Update(msg)
	return c, cmd, false
}

// --- quem fala ---

// send manda sua mensagem. Com now, quem você chamou e está no meio de uma fala
// para na hora e recomeça já lendo o que você escreveu, em vez de esperar a vez.
func (c *chatScreen) send(text string, now bool) tea.Cmd {
	if info, err := os.Stat(c.g.Folder); err != nil || !info.IsDir() {
		c.err = i18n.T("The group folder is gone: %s", tildify(c.g.Folder))
		return nil
	}
	clean, paths := attach.Extract(text)
	var images []string
	for _, p := range paths {
		saved, err := attach.Save(p, c.st.Attachments(c.g.ID))
		if err != nil {
			c.err = i18n.T("Could not attach %s: %v", filepath.Base(p), err)
			return nil
		}
		images = append(images, saved)
	}
	c.post(store.Message{Text: clean, Images: images})
	c.root++
	targets := chat.Targets(clean, c.g, c.cfg.NoMention == config.NoMentionAll)
	if now {
		c.interrupt(targets)
	}
	for _, h := range targets {
		c.enqueue(job{handle: h, by: "@" + chat.Me() + " (the human)", root: c.root})
	}
	return c.pump()
}

// interrupt para a fala atual de cada um e tira da fila os pedidos antigos dele:
// a próxima vez que falar já lê tudo, incluindo sua mensagem nova.
func (c *chatScreen) interrupt(handles []string) {
	rest := c.queue[:0]
	for _, j := range c.queue {
		if !slices.Contains(handles, j.handle) {
			rest = append(rest, j)
		}
	}
	c.queue = rest
	for _, h := range handles {
		if j, ok := c.running[h]; ok && j.cancel != nil {
			c.cut[h] = true
			j.cancel()
		}
	}
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
			text := i18n.T("The disk has %s free. AIs are paused until at least %s is free, to keep your Mac running. They resume when space is freed.",
				sys.Size(c.disk), sys.Size(sys.DiskPause))
			if sys.HasMole() {
				text += i18n.T(" Press %s to clean with Mole.", c.cfg.Keys.CleanDisk)
			}
			c.post(store.Message{From: store.System, Text: text})
			chat.Notify(c.g.Name, i18n.T("AIs paused"), i18n.T("Disk has %s free.", sys.Size(c.disk)))
		}
		c.refresh(true)
		return nil
	}
	var cmds []tea.Cmd
	rest := c.queue[:0]
	for _, j := range c.queue {
		switch {
		case c.budget[j.root] >= c.cfg.MaxAgentTurns:
			if !c.stopped[j.root] {
				c.stopped[j.root] = true
				c.post(store.Message{From: store.System, Text: i18n.T(
					"AIs have spoken %d times in a row. Stopped here to avoid a loop. Send a message to continue.",
					c.cfg.MaxAgentTurns)})
			}
		case c.isRunning(j.handle):
			rest = append(rest, j)
		case c.overLimit(j.handle):
			rest = append(rest, j) // espera o limite do plano voltar
		default:
			j.start = time.Now()
			ctx, cancel := context.WithCancel(c.ctx)
			j.cancel = cancel
			c.running[j.handle] = j
			c.budget[j.root]++
			cmds = append(cmds, c.turn(j, ctx))
		}
	}
	c.queue = rest
	if len(c.running) > 0 && !c.listening {
		c.listening = true
		cmds = append(cmds, waitFor(c.act))
	}
	if len(c.running) > 0 && !c.spinning {
		c.spinning = true
		cmds = append(cmds, c.spin.Tick)
	}
	c.refresh(false)
	return tea.Batch(cmds...)
}

func (c *chatScreen) turn(j job, ctx context.Context) tea.Cmd {
	me, _ := chat.Member(c.g, j.handle)
	t := engines.Turn{
		Engine:     me.Engine,
		Model:      me.Model,
		Folder:     c.g.Folder,
		FullAccess: me.FullAccess,
		Prompt:     chat.Prompt(c.g, me, j.by, append([]store.Message(nil), c.msgs...)),
		Images:     chat.Images(c.msgs),
	}
	groupID, ch := c.g.ID, c.act
	t.OnActivity = func(a engines.Activity) {
		select {
		case ch <- activityMsg{groupID: groupID, handle: j.handle, act: a, at: time.Now()}:
		default: // tela atrasada: perde uma linha do painel, mas nunca trava a IA
		}
	}
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
	if done.cancel != nil {
		done.cancel()
	}
	acts := toActions(c.acts[m.handle])
	delete(c.acts, m.handle)
	mentioned := false
	switch {
	case errors.Is(m.err, engines.ErrStopped) && c.cut[m.handle]:
		delete(c.cut, m.handle)
		c.post(store.Message{From: store.System, Text: i18n.T("@%s stopped to read your new message.", m.handle), Actions: acts})
	case errors.Is(m.err, engines.ErrStopped):
		c.spoke = false // você parou: não avisa que terminou
		if c.stopWhy != "" {
			c.post(store.Message{From: store.System, Text: i18n.T("Stopped @%s %s.", m.handle, c.stopWhy), Actions: acts})
		} else {
			c.post(store.Message{From: store.System, Text: i18n.T("You stopped @%s.", m.handle), Actions: acts})
		}
		if len(c.running) == 0 {
			c.stopWhy = ""
		}
	case m.err != nil:
		c.post(store.Message{From: m.handle, Text: m.err.Error(), Error: true, Actions: acts})
	default:
		c.post(store.Message{From: m.handle, Text: m.text, Actions: acts})
		c.spoke = true
		if chat.MentionsMe(m.text) {
			mentioned = true
			chat.Notify(c.g.Name, i18n.T("@%s mentioned you", m.handle), m.text)
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
			c.enqueue(job{handle: done.caller, by: "@" + m.handle + " (replying to your request)",
				caller: m.handle, reply: true, root: done.root})
		}
	}
	cmd := c.pump()
	// todo mundo parou de falar: avisa uma vez (se já te marcaram agora, essa notificação basta)
	if !c.busy() {
		if c.spoke && !mentioned && !c.diskPaused {
			chat.Notify(c.g.Name, i18n.T("Everyone finished"), lastText(c.msgs))
			if !c.visible {
				c.pinged = true
			}
		}
		c.spoke = false
	}
	return cmd
}

// lastText é a última fala de uma IA, pra notificação.
func lastText(msgs []store.Message) string {
	for i := len(msgs) - 1; i >= 0; i-- {
		if m := msgs[i]; m.From != "" && m.From != store.System && !m.Error {
			return "@" + m.From + ": " + m.Text
		}
	}
	return ""
}

// setDisk recebe o espaço livre: pausa, para ou retoma as IAs.
func (c *chatScreen) setDisk(free uint64) tea.Cmd {
	c.disk = free
	if free == 0 {
		return nil
	}
	if free < sys.DiskKill && len(c.running) > 0 && c.stopWhy == "" {
		c.stopWhy = i18n.T("because the disk has %s free", sys.Size(free))
		for _, j := range c.running {
			c.enqueue(j) // volta pra fila e continua quando liberar
		}
		c.stop()
		c.ctx, c.stop = context.WithCancel(context.Background())
		return nil
	}
	if c.diskPaused && free >= sys.DiskPause {
		c.diskPaused = false
		c.post(store.Message{From: store.System, Text: i18n.T("Space freed (%s). AIs are back.", sys.Size(free))})
		return c.pump()
	}
	return nil
}

func (c *chatScreen) stopAll() tea.Cmd {
	if !c.busy() {
		return nil
	}
	c.queue = nil
	c.spoke = false
	c.stop()
	c.ctx, c.stop = context.WithCancel(context.Background())
	return nil
}

func (c *chatScreen) post(m store.Message) {
	m.At = time.Now()
	c.msgs = append(c.msgs, m)
	if err := c.st.Append(c.g.ID, m); err != nil {
		c.err = i18n.T("Could not save the chat: %v", err)
	}
	// sua mensagem desce a conversa; a das IAs só desce se você já estava no fim,
	// pra não arrancar você do que está lendo lá em cima
	mine := m.From == ""
	if !mine && !c.vp.AtBottom() {
		c.unseen = true
	}
	c.redraw(mine)
	c.seen()
}

// seen apaga o aviso de mensagens novas quando você chega no fim.
func (c *chatScreen) seen() {
	if c.vp.AtBottom() {
		c.unseen = false
	}
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
	if all := chat.EveryoneHandle(); strings.HasPrefix(all, m[1]) {
		out = append(out, store.Member{Handle: all, Color: string(cMuted.Dark)})
	}
	return out
}

func (c *chatScreen) complete() {
	if cmds := c.slashSuggestions(); len(cmds) > 0 {
		c.input.SetValue(cmds[0].name + " ")
		c.input.CursorEnd()
		return
	}
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
	if c.hist == "" || c.histW != c.vp.Width {
		c.hist, c.histW = c.renderMessages(), c.vp.Width
	}
	c.vp.SetContent(c.hist + c.liveBlocks(max(10, c.vp.Width-2)))
	if follow || atBottom {
		c.vp.GotoBottom()
	}
}

// redraw desenha a conversa de novo (mensagem nova, ctrl+o).
func (c *chatScreen) redraw(follow bool) {
	c.hist = ""
	c.refresh(follow)
}

func (c *chatScreen) toggleExpand() {
	c.expand = !c.expand
	c.redraw(false)
}

// backToMine leva a conversa até sua última mensagem; se ela já está na tela, até a anterior.
func (c *chatScreen) backToMine() {
	if len(c.mine) == 0 {
		return
	}
	target := -1
	if c.mineHidden() != 0 {
		target = c.mine[len(c.mine)-1]
	} else {
		for _, at := range c.mine {
			if at < c.vp.YOffset {
				target = at
			}
		}
	}
	if target < 0 {
		return
	}
	c.vp.SetYOffset(target)
	c.seen()
}

// mineHidden diz onde está sua última mensagem quando ela não aparece na tela:
// -1 lá em cima, 1 lá embaixo, 0 visível (ou nenhuma).
func (c *chatScreen) mineHidden() int {
	if len(c.mine) == 0 {
		return 0
	}
	switch last := c.mine[len(c.mine)-1]; {
	case last < c.vp.YOffset:
		return -1
	case last >= c.vp.YOffset+c.vp.Height:
		return 1
	}
	return 0
}

func (c *chatScreen) renderMessages() string {
	if len(c.msgs) == 0 {
		lead, _ := chat.Member(c.g, c.g.Lead)
		return sMuted.Render(i18n.T("Send the first message. Without @, %s replies. Use %s to call everyone.", at(lead.Handle, lead.Color), "@"+chat.EveryoneHandle()))
	}
	width := max(10, c.vp.Width-2)
	var b strings.Builder
	c.mine = c.mine[:0]
	for i, m := range c.msgs {
		if i > 0 {
			b.WriteString("\n")
		}
		pad := "  "
		if m.From == "" {
			// sua mensagem: barra colorida do lado, pra achar no meio da conversa
			c.mine = append(c.mine, strings.Count(b.String(), "\n"))
			pad = sMine.Render("▌") + " "
			b.WriteString(pad + c.speaker(m.From) + "  " + sFaint.Render(m.At.Format("15:04")) + "\n")
		} else {
			b.WriteString(c.speaker(m.From) + "  " + sFaint.Render(m.At.Format("15:04")) + "\n")
		}
		text := m.Text
		if m.From != "" && m.From != store.System && !m.Error {
			text = plainMarkdown(text) // resposta de IA: tira links e títulos antes de quebrar
		}
		body := lipgloss.NewStyle().Width(width).Render(text)
		if m.Text == "" {
			body = ""
		}
		switch {
		case m.Error:
			body = sError.Render(lipgloss.NewStyle().Width(width).Render(i18n.T("could not reply: %s", m.Text)))
		case m.From == store.System:
			body = sMuted.Italic(true).Render(body)
		case m.From == "":
			body = c.paintMentions(body)
		default:
			body = c.paintMentions(styleMarkdown(body))
		}
		if body != "" {
			b.WriteString(indent(body, pad) + "\n")
		}
		if a := c.renderActions(m.Actions, width); a != "" {
			b.WriteString(indent(a, pad) + "\n")
		}
		for _, img := range m.Images {
			b.WriteString(pad + sMuted.Render(i18n.T("image ")) + sText.Render(filepath.Base(img)) + "\n")
		}
	}
	return b.String()
}

func (c *chatScreen) speaker(from string) string {
	switch from {
	case "":
		return sTitle.Render(i18n.T("you")) + sFaint.Render(" @"+chat.Me())
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
			c.err = i18n.T("Could not record: %v", err)
			return nil
		}
		c.rec = rec
		return c.startSpin()
	}
	wav, err := c.rec.Stop()
	c.rec = nil
	if err != nil {
		c.err = i18n.T("Could not record: %v", err)
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
			c.err = i18n.T("Homebrew (brew.sh) is needed to install: %s", strings.Join(voice.MissingTools(), " "))
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
		parts = append(parts, sError.Render(i18n.T("● recording %d:%02d", int(d.Minutes()), int(d.Seconds())%60))+
			sMuted.Render(i18n.T("  %s finishes · esc discards", c.cfg.Keys.Voice)))
	}
	if c.transcribing {
		parts = append(parts, c.spin.View()+sMuted.Render(i18n.T(" transcribing your voice")))
	}
	if c.dl != nil {
		parts = append(parts, c.spin.View()+sMuted.Render(i18n.T(" downloading the voice model "))+
			progressBar(c.dlDone, c.dlTotal, 20)+sMuted.Render(" "+sys.Size(uint64(c.dlDone))))
	}
	switch c.voiceSetup {
	case "tools":
		parts = append(parts, sActive.Render(i18n.T("To hear you, install %s with Homebrew.", strings.Join(voice.MissingTools(), i18n.T(" and "))))+sMuted.Render(i18n.T("  enter install · esc cancel")))
	case "model":
		parts = append(parts, sActive.Render(i18n.T("To hear you, tars downloads Whisper (~550 MB, runs only on your Mac)."))+
			sMuted.Render(i18n.T("  enter download · esc cancel")))
	}
	for _, m := range c.g.Members {
		if j, ok := c.running[m.Handle]; ok {
			if c.vp.AtBottom() {
				continue // o bloco ao vivo já está no fim da conversa
			}
			since := time.Since(j.start)
			d := since.Round(time.Second)
			parts = append(parts, c.spin.View()+" "+at(m.Handle, m.Color)+" "+sActive.Render(thinkingWord(m.Handle, since))+
				sFaint.Render(fmt.Sprintf(" %d:%02d", int(d.Minutes()), int(d.Seconds())%60)))
		}
	}
	for _, j := range c.queue {
		m, ok := chat.Member(c.g, j.handle)
		if !ok {
			continue
		}
		if reset, over := c.limitReset(j.handle); over {
			parts = append(parts, at(m.Handle, m.Color)+sWarn.Render(i18n.T(" waiting for the limit to reset (%s)", when(reset))))
		} else {
			parts = append(parts, at(m.Handle, m.Color)+sMuted.Render(i18n.T(" queued")))
		}
	}
	if len(c.running) > 0 {
		parts = append(parts, sWarn.Render(i18n.T("%s stop all", c.cfg.Keys.Stop)))
	}
	if c.unseen {
		parts = append([]string{sActive.Render(i18n.T("↓ new messages"))}, parts...)
	}
	if c.mineHidden() != 0 {
		parts = append([]string{c.backLabel()}, parts...)
	}
	if c.diskPaused {
		parts = append([]string{sError.Render(i18n.T("paused: disk full"))}, parts...)
	}
	return strings.Join(parts, sFaint.Render("  ·  "))
}

func (c *chatScreen) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(c.g.Name) + "  " + handles(c.g) + "\n")
	b.WriteString(statusBar(c.disk, c.limits, c.engines) + "\n\n")
	if c.showInfo {
		b.WriteString(groupSummary(c.g, c.engines, c.vp.Width))
		b.WriteString(sHelp.Render(i18n.T("esc back to chat")))
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
		hint = sMuted.Render(i18n.T("tab complete  ")) + strings.Join(names, " ")
	}
	if cmds := c.slashSuggestions(); len(cmds) > 0 {
		parts := make([]string, len(cmds))
		for i, cm := range cmds {
			parts[i] = sActive.Render(cm.name) + sMuted.Render(" "+i18n.T(cm.desc))
		}
		hint = sMuted.Render(i18n.T("tab complete  ")) + strings.Join(parts, sFaint.Render(" · "))
	}
	if !c.escAt.IsZero() && time.Since(c.escAt) < leaveWindow {
		hint = sActive.Render(i18n.T("%s again to leave", c.cfg.Keys.Stop))
	}
	if _, imgs := attach.Extract(c.input.Value()); len(imgs) > 0 && hint == "" {
		word := "%d image ready to send"
		if len(imgs) > 1 {
			word = "%d images ready to send"
		}
		hint = sActive.Render(i18n.T(word, len(imgs)))
	}
	if c.err != "" {
		hint = sError.Render(c.err)
	}
	b.WriteString(hint + "\n")

	// do mais importante pro menos: o que não couber na largura fica de fora
	k := c.cfg.Keys
	var keys []string
	mole := c.disk != 0 && c.disk < sys.DiskLow && sys.HasMole()
	if c.busy() {
		keys = append(keys, i18n.T("%s stop all", k.Stop), i18n.T("%s leave, they keep working", k.Leave),
			i18n.T("%s send now", k.SendNow), i18n.T("%s shows everything they did", k.Activity))
		if mole {
			keys = append(keys, sWarn.Render(i18n.T("/clean frees disk (Mole)")))
		}
	} else {
		if mole {
			keys = append(keys, sWarn.Render(i18n.T("/clean frees disk (Mole)")))
		}
		keys = append(keys, i18n.T("enter send"), i18n.T("@ mention"), i18n.T("/ commands"), i18n.T("%s twice leaves", k.Stop))
		if c.cfg.Voice.Enabled {
			keys = append(keys, i18n.T("%s talk", k.Voice))
		}
		keys = append(keys, i18n.T("%s paste screenshot", k.Paste), i18n.T("%s shows everything they did", k.Activity))
	}
	help := fitKeys(keys, c.vp.Width)
	b.WriteString(sHelp.Render(help))
	return b.String()
}

// fitKeys junta os atalhos com " · " até onde a largura deixa.
func fitKeys(keys []string, width int) string {
	out := ""
	for _, k := range keys {
		next := k
		if out != "" {
			next = out + sMuted.Render(" · ") + k
		}
		if lipgloss.Width(next) > width {
			break
		}
		out = next
	}
	return out
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
