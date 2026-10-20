// Package ui é a tela do bot no terminal.
package ui

import (
	"fmt"
	"strings"

	"bot/internal/engines"
	"bot/internal/store"

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
}

func New(st *store.Store, list []engines.Engine) *App {
	a := &App{store: st, engines: list, chats: map[string]*chatScreen{}, width: 80, height: 24}
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

func (a *App) Init() tea.Cmd { return nil }

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
		if msg.String() == "ctrl+c" {
			a.stopAll()
			return a, tea.Quit
		}
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
	case turnDoneMsg:
		// a resposta vai pro chat certo, mesmo com você em outra tela
		if c, ok := a.chats[msg.groupID]; ok {
			_, cmd, _ := c.Update(msg)
			return a, cmd
		}
		return a, nil
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
	switch a.screen {
	case screenWizard:
		return sPage.Render(a.wiz.View())
	case screenChat:
		return sPage.Render(a.open.View())
	}
	return sPage.Render(a.viewHome())
}

func (a *App) viewHome() string {
	var b strings.Builder
	b.WriteString(sTitle.Render("bot") + sMuted.Render("  suas IAs trabalhando juntas") + "\n\n")

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
	b.WriteString("\n" + sHelp.Render("↑↓ navega · enter abre · n novo grupo · q sai"))
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
