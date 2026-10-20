// Package ui é a tela do tars no terminal.
package ui

import (
	"fmt"
	"strings"
	"time"

	"github.com/dedsfe/tars/internal/chat"
	"github.com/dedsfe/tars/internal/config"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/store"
	"github.com/dedsfe/tars/internal/sys"
	"github.com/dedsfe/tars/internal/usage"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

const (
	screenHome = iota
	screenWizard
	screenChat
)

type App struct {
	store   *store.Store
	engines []engines.Engine
	groups  []store.Group
	screen  int
	cursor  int // 0 = novo grupo, 1.. = grupos
	wiz     *wizard
	chats   map[string]*chatScreen // ficam vivos pra IAs seguirem trabalhando
	open    *chatScreen
	err     string
	width   int
	height  int
	disk    uint64
	cfg     config.Config
	limits  *usage.Cache
	warned  map[string]bool // avisos de limite já mostrados
	flash   string          // aviso rápido no rodapé, some na próxima tecla
	before  uint64          // espaço livre antes de rodar o Mole
}

type usageMsg struct{}

type moleDoneMsg struct{ err error }

// watchUsage relê os limites a cada 30 segundos.
func watchUsage(wait time.Duration) tea.Cmd {
	return tea.Tick(wait, func(time.Time) tea.Msg { return usageMsg{} })
}

type diskMsg uint64

// watchDisk lê o espaço livre agora e depois a cada 5 segundos.
func watchDisk(wait time.Duration) tea.Cmd {
	return tea.Tick(wait, func(time.Time) tea.Msg { return diskMsg(sys.FreeDisk()) })
}

func New(st *store.Store, list []engines.Engine, cfg config.Config) *App {
	a := &App{store: st, engines: list, cfg: cfg, chats: map[string]*chatScreen{}, width: 80, height: 24,
		limits: usage.OpenCache(st.Root()), warned: map[string]bool{}}
	a.reload()
	return a
}

func (a *App) reload() {
	groups, err := a.store.List()
	if err != nil {
		a.err = "Não consegui ler os grupos: " + err.Error()
	}
	a.groups = groups
}

func (a *App) Init() tea.Cmd { return tea.Batch(watchDisk(0), watchUsage(0)) }

// readCodex atualiza o limite do Codex a partir dos registros dele.
func (a *App) readCodex() {
	if l, ok := usage.Codex(); ok {
		a.limits.Set("codex", l)
	}
}

// checkLimits avisa uma vez quando uma IA passa de 90% de alguma janela,
// e acorda quem estava esperando o limite voltar.
func (a *App) checkLimits() tea.Cmd {
	now := time.Now()
	for id, l := range a.limits.By {
		w, ok := l.Worst(now)
		if !ok || w.Used < 90 {
			continue
		}
		key := fmt.Sprintf("%s|%s|%d", id, w.Name, w.ResetsAt.Unix())
		if a.warned[key] {
			continue
		}
		a.warned[key] = true
		name := id
		if e, ok := engines.Find(a.engines, id); ok {
			name = e.Name
		}
		text := fmt.Sprintf("%s já usou %.0f%% do limite (%s). Volta %s.", name, w.Used, w.Name, when(w.ResetsAt))
		chat.Notify("tars", "Limite perto do fim", text)
		for _, c := range a.chats {
			for _, m := range c.g.Members {
				if m.Engine == id {
					c.post(store.Message{From: store.System, Text: text})
					break
				}
			}
		}
	}
	var cmds []tea.Cmd
	for _, c := range a.chats {
		if len(c.queue) > 0 {
			cmds = append(cmds, c.pump())
		}
	}
	return tea.Batch(cmds...)
}

func (a *App) stopAll() {
	for _, c := range a.chats {
		c.stop()
	}
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		if a.wiz != nil {
			a.wiz.resize(msg.Width)
		}
		for _, c := range a.chats {
			c.resize(msg.Width, msg.Height)
		}
		return a, nil
	case tea.KeyMsg:
		a.flash = ""
		switch msg.String() {
		case "ctrl+c":
			a.stopAll()
			return a, tea.Quit
		case a.cfg.Keys.CleanDisk:
			if a.screen != screenWizard && sys.HasMole() {
				if a.cfg.Disk.Mole == config.MoleInside {
					// o tars dá lugar ao Mole e volta quando você sai dele
					a.before = sys.FreeDisk()
					return a, tea.ExecProcess(sys.MoleClean(), func(err error) tea.Msg { return moleDoneMsg{err} })
				}
				// o Mole abre numa janela nova e o tars segue aqui, medindo o espaço
				if err := sys.OpenMole(); err != nil {
					a.flash = sError.Render("Não consegui abrir o Mole: " + err.Error())
					return a, nil
				}
				a.before = sys.FreeDisk()
				a.flash = sMuted.Render("O Mole abriu numa janela nova. Conforme ele limpar, o espaço aparece aqui.")
				return a, nil
			}
		}
	case moleDoneMsg:
		after := sys.FreeDisk()
		switch {
		case msg.err != nil:
			a.flash = sError.Render("O Mole fechou com erro: " + msg.err.Error())
		case after > a.before:
			a.flash = sActive.Render("De volta ao tars. O Mole liberou " + sys.Size(after-a.before) + ".")
		default:
			a.flash = sMuted.Render("De volta ao tars. O espaço livre não mudou.")
		}
		a.before = 0
		return a, watchDisk(0)
	case wizardCancelMsg:
		a.screen, a.wiz = screenHome, nil
		return a, nil
	case wizardDoneMsg:
		g := msg.group
		if err := a.store.Save(&g); err != nil {
			a.wiz.err = "Não consegui salvar: " + err.Error()
			return a, nil
		}
		a.reload()
		a.wiz = nil
		a.cursor = a.indexOf(g.ID) + 1
		return a, a.openChat(g)
	case diskMsg:
		a.disk = uint64(msg)
		if a.before != 0 && a.disk > a.before+100*sys.MB {
			a.flash = sActive.Render("O Mole já liberou " + sys.Size(a.disk-a.before) + ".")
		}
		cmds := []tea.Cmd{watchDisk(5 * time.Second)}
		for _, c := range a.chats {
			cmds = append(cmds, c.setDisk(a.disk))
		}
		return a, tea.Batch(cmds...)
	case voiceDoneMsg, voiceToolsDoneMsg, modelProgressMsg, modelDoneMsg:
		if c, ok := a.chats[groupOf(msg)]; ok {
			_, cmd, _ := c.Update(msg)
			return a, cmd
		}
		return a, nil
	case usageMsg:
		a.readCodex()
		return a, tea.Batch(watchUsage(30*time.Second), a.checkLimits())
	case turnDoneMsg:
		if msg.limits != nil {
			a.limits.Set(msg.engine, *msg.limits)
		}
		a.limits.AddTokens(msg.engine, msg.tokens)
		if msg.engine == "codex" {
			a.readCodex()
		}
		cmds := []tea.Cmd{a.checkLimits()}
		// a resposta vai pro chat certo, mesmo com você em outra tela
		if c, ok := a.chats[msg.groupID]; ok {
			_, cmd, _ := c.Update(msg)
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)
	case spinner.TickMsg:
		var cmds []tea.Cmd
		for _, c := range a.chats {
			_, cmd, _ := c.Update(msg)
			cmds = append(cmds, cmd)
		}
		return a, tea.Batch(cmds...)
	}

	switch a.screen {
	case screenWizard:
		var cmd tea.Cmd
		a.wiz, cmd = a.wiz.Update(msg)
		return a, cmd
	case screenChat:
		_, cmd, leave := a.open.Update(msg)
		if leave {
			a.open.visible = false
			a.screen, a.open = screenHome, nil
		}
		return a, cmd
	}
	return a, a.updateHome(msg)
}

func (a *App) updateHome(msg tea.Msg) tea.Cmd {
	k, ok := msg.(tea.KeyMsg)
	if !ok {
		return nil
	}
	switch k.String() {
	case "q", "esc":
		a.stopAll()
		return tea.Quit
	case "up", "k":
		a.cursor = max(a.cursor-1, 0)
	case "down", "j":
		a.cursor = min(a.cursor+1, len(a.groups))
	case "n":
		return a.startWizard()
	case "enter":
		if a.cursor == 0 {
			return a.startWizard()
		}
		return a.openChat(a.groups[a.cursor-1])
	}
	return nil
}

func (a *App) openChat(g store.Group) tea.Cmd {
	c, ok := a.chats[g.ID]
	if !ok {
		c = newChat(a.store, g, a.engines, a.width, a.height)
		c.disk, c.limits, c.cfg = a.disk, a.limits, a.cfg
		a.chats[g.ID] = c
	}
	c.refresh(true)
	c.visible, c.pinged = true, false
	a.open, a.screen = c, screenChat
	return c.input.Focus()
}

func (a *App) startWizard() tea.Cmd {
	a.wiz = newWizard(a.engines, a.width)
	a.screen = screenWizard
	return a.wiz.name.Focus()
}

func (a *App) indexOf(id string) int {
	for i, g := range a.groups {
		if g.ID == id {
			return i
		}
	}
	return 0
}

func (a *App) View() string {
	var v string
	switch a.screen {
	case screenWizard:
		v = a.wiz.View()
	case screenChat:
		v = a.open.View()
	default:
		v = a.viewHome()
	}
	if a.flash != "" {
		v += "\n" + a.flash
	}
	return sPage.Render(v)
}

func (a *App) viewHome() string {
	var b strings.Builder
	b.WriteString(sTitle.Render("tars") + sMuted.Render("  suas IAs trabalhando juntas") + "\n")
	b.WriteString(statusBar(a.disk, a.limits, a.engines) + "\n\n")

	b.WriteString(pointer(a.cursor == 0))
	if a.cursor == 0 {
		b.WriteString(sActive.Render("+ Novo grupo"))
	} else {
		b.WriteString(sText.Render("+ Novo grupo"))
	}
	b.WriteString("\n")

	if len(a.groups) == 0 {
		b.WriteString("\n" + sMuted.Render("Nenhum grupo ainda. Monta o primeiro e escolhe quem entra.") + "\n")
	} else {
		b.WriteString("\n" + sLabel.Render("Seus grupos") + "\n")
	}
	for i, g := range a.groups {
		on := a.cursor == i+1
		name := sText.Render(g.Name)
		if on {
			name = sActive.Render(g.Name)
		}
		line := pointer(on) + name + "  " + handles(g)
		if c, ok := a.chats[g.ID]; ok {
			if c.pinged {
				line += sActive.Render("  te marcaram")
			} else if c.busy() {
				line += sMuted.Render("  trabalhando")
			}
		}
		b.WriteString(line + "\n")
		b.WriteString("  " + sFaint.Render(truncate(oneLine(g.Goal), max(20, a.width-8))) + "\n")
	}
	if a.err != "" {
		b.WriteString("\n" + sError.Render(a.err))
	}
	help := sMuted.Render("↑↓ navega · enter abre · n novo grupo · q sai")
	if a.disk != 0 && a.disk < sys.DiskLow && sys.HasMole() {
		help = sWarn.Render(a.cfg.Keys.CleanDisk+" limpa o disco com o Mole") + sMuted.Render(" · ") + help
	}
	b.WriteString("\n" + sHelp.Render(help))
	return b.String()
}

// groupSummary mostra objetivo, pasta, líder e membros, quebrando
// as linhas longas na largura da tela.
func groupSummary(g store.Group, list []engines.Engine, width int) string {
	wrap := lipgloss.NewStyle().Width(max(20, width))
	wrapRole := lipgloss.NewStyle().Width(max(18, width-2))

	var b strings.Builder
	b.WriteString(sLabel.Render("Objetivo") + "\n" + sText.Render(wrap.Render(g.Goal)) + "\n\n")
	b.WriteString(sLabel.Render("Pasta  ") + sText.Render(tildify(g.Folder)) + "\n\n")
	b.WriteString(sLabel.Render("Membros") + "\n")
	for _, m := range g.Members {
		name := m.Engine
		if e, ok := engines.Find(list, m.Engine); ok {
			name = e.Name
		}
		model := m.Model
		if model == "" {
			model = engines.Default
		}
		line := at(m.Handle, m.Color) + sMuted.Render(fmt.Sprintf("  %s · %s · %s", name, model, accessText(m.FullAccess)))
		if m.Handle == g.Lead {
			line += sActive.Render("  líder")
		}
		b.WriteString(line + "\n")
		b.WriteString(indent(sText.Render(wrapRole.Render(m.Role)), "  ") + "\n")
	}
	return b.String()
}

func handles(g store.Group) string {
	parts := make([]string, len(g.Members))
	for i, m := range g.Members {
		parts[i] = at(m.Handle, m.Color)
	}
	return strings.Join(parts, " ")
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

// groupOf diz de qual grupo é uma mensagem de voz.
func groupOf(msg tea.Msg) string {
	switch m := msg.(type) {
	case voiceDoneMsg:
		return m.groupID
	case voiceToolsDoneMsg:
		return m.groupID
	case modelProgressMsg:
		return m.groupID
	case modelDoneMsg:
		return m.groupID
	}
	return ""
}
