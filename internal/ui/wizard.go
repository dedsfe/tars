package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/dedsfe/tars/internal/chat"
	"github.com/dedsfe/tars/internal/engines"
	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/store"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Passos da criação de grupo.
const (
	stepBasics = iota
	stepPick
	stepProfiles
	stepLead
	stepReview
)

var stepNames = []string{"Goal", "Who joins", "Profiles", "Leader", "Review"}

// Campos do passo Objetivo.
const (
	fName = iota
	fGoal
	fFolder
)

// Campos do perfil de cada IA.
const (
	pHandle = iota
	pRole
	pModel
	pColor
	pAccess
	pCount
)

var handleRe = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,19}$`)

// Apelidos que o chat vai usar pra outras coisas.
var reservedHandles = map[string]bool{"todos": true, "all": true, "eu": true}

type wizardDoneMsg struct{ group store.Group }
type wizardCancelMsg struct{}

type draft struct {
	engine engines.Engine
	nth    int // qual cópia dessa IA (0, 1, ...)
	handle textinput.Model
	role   textinput.Model
	model  int
	color  int
	full   bool
}

type wizard struct {
	step    int
	engines []engines.Engine
	err     string
	width   int

	name   textinput.Model
	goal   textarea.Model
	folder textinput.Model
	focus  int

	cursor int
	counts []int // quantas cópias de cada IA

	drafts []*draft
	cur    int // IA sendo editada
	field  int

	lead int
}

func newWizard(list []engines.Engine, width int) *wizard {
	w := &wizard{engines: list, counts: make([]int, len(list))}

	w.name = newInput(i18n.T("E.g. Bakery landing page"), 40)
	w.goal = textarea.New()
	w.goal.Placeholder = i18n.T("What will this group do? Every AI reads this before replying.")
	w.goal.ShowLineNumbers = false
	w.goal.Prompt = "  "
	w.goal.CharLimit = 1000
	w.goal.SetHeight(3)
	w.goal.FocusedStyle.CursorLine = lipgloss.NewStyle()

	cwd, _ := os.Getwd()
	w.folder = newInput(i18n.T("Folder where AIs work"), 300)
	w.folder.SetValue(tildify(cwd))

	w.cursor = firstInstalled(list)
	w.resize(width)
	w.name.Focus()
	return w
}

func newInput(placeholder string, limit int) textinput.Model {
	t := textinput.New()
	t.Placeholder = placeholder
	t.CharLimit = limit
	t.Prompt = "  "
	return t
}

func (w *wizard) resize(width int) {
	w.width = width
	fieldW := min(64, max(20, width-12))
	w.name.Width = fieldW
	w.folder.Width = fieldW
	w.goal.SetWidth(fieldW + 2)
	for _, d := range w.drafts {
		d.handle.Width = 20
		d.role.Width = fieldW
	}
}

func (w *wizard) Update(msg tea.Msg) (*wizard, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if isKey {
		w.err = ""
		if key.String() == "esc" {
			return w, w.back()
		}
	}
	switch w.step {
	case stepBasics:
		return w, w.updateBasics(msg, key, isKey)
	case stepPick:
		if isKey {
			return w, w.updatePick(key)
		}
	case stepProfiles:
		return w, w.updateProfiles(msg, key, isKey)
	case stepLead:
		if isKey {
			return w, w.updateLead(key)
		}
	case stepReview:
		if isKey && key.String() == "enter" {
			g := w.build()
			return w, func() tea.Msg { return wizardDoneMsg{g} }
		}
	}
	return w, nil
}

// back volta um campo, uma IA ou um passo.
func (w *wizard) back() tea.Cmd {
	switch w.step {
	case stepBasics:
		return func() tea.Msg { return wizardCancelMsg{} }
	case stepProfiles:
		if w.cur > 0 {
			return w.focusDraft(w.cur-1, pHandle)
		}
		w.step = stepPick
	case stepLead:
		return w.focusDraft(len(w.drafts)-1, pHandle)
	case stepReview:
		if len(w.drafts) > 1 {
			w.step = stepLead
		} else {
			return w.focusDraft(len(w.drafts)-1, pHandle)
		}
	default:
		w.step--
		if w.step == stepBasics {
			return w.focusBasics(fFolder)
		}
	}
	return nil
}

// --- Objetivo ---

func (w *wizard) updateBasics(msg tea.Msg, key tea.KeyMsg, isKey bool) tea.Cmd {
	if isKey {
		switch key.String() {
		case "tab", "down":
			if key.String() == "down" && w.focus == fGoal {
				break // seta dentro do texto anda entre linhas
			}
			return w.focusBasics(min(w.focus+1, fFolder))
		case "shift+tab", "up":
			if key.String() == "up" && w.focus == fGoal {
				break
			}
			return w.focusBasics(max(w.focus-1, fName))
		case "enter":
			if w.focus == fGoal {
				break // nova linha no objetivo
			}
			if w.focus == fName {
				if strings.TrimSpace(w.name.Value()) == "" {
					w.err = i18n.T("Give the group a name.")
					return nil
				}
				return w.focusBasics(fGoal)
			}
			if err := w.validateBasics(); err != "" {
				w.err = err
				return nil
			}
			w.step = stepPick
			return nil
		}
	}
	var cmd tea.Cmd
	switch w.focus {
	case fName:
		w.name, cmd = w.name.Update(msg)
	case fGoal:
		w.goal, cmd = w.goal.Update(msg)
	case fFolder:
		w.folder, cmd = w.folder.Update(msg)
	}
	return cmd
}

func (w *wizard) focusBasics(f int) tea.Cmd {
	w.step = stepBasics
	w.focus = f
	w.name.Blur()
	w.goal.Blur()
	w.folder.Blur()
	switch f {
	case fName:
		return w.name.Focus()
	case fGoal:
		return w.goal.Focus()
	default:
		return w.folder.Focus()
	}
}

func (w *wizard) validateBasics() string {
	if strings.TrimSpace(w.name.Value()) == "" {
		w.focusBasics(fName)
		return i18n.T("Give the group a name.")
	}
	if strings.TrimSpace(w.goal.Value()) == "" {
		w.focusBasics(fGoal)
		return i18n.T("Write a goal. It guides the AIs.")
	}
	info, err := os.Stat(expand(w.folder.Value()))
	if err != nil || !info.IsDir() {
		w.focusBasics(fFolder)
		return i18n.T("That folder does not exist.")
	}
	return ""
}

func (w *wizard) viewBasics() string {
	var b strings.Builder
	b.WriteString(w.label(fName, i18n.T("Group name")) + "\n" + w.name.View() + "\n\n")
	b.WriteString(w.label(fGoal, i18n.T("Goal")) + "\n" + w.goal.View() + "\n\n")
	b.WriteString(w.label(fFolder, i18n.T("Folder")) + "\n" + w.folder.View())
	return b.String()
}

func (w *wizard) label(f int, text string) string {
	if w.focus == f {
		return sActive.Render(text)
	}
	return sLabel.Render(text)
}

// --- Quem entra ---

func (w *wizard) updatePick(key tea.KeyMsg) tea.Cmd {
	e := w.engines[w.cursor]
	switch key.String() {
	case "up", "k", "shift+tab":
		w.cursor = w.moveInstalled(-1)
	case "down", "j", "tab":
		w.cursor = w.moveInstalled(1)
	case " ", "x":
		if e.Installed {
			w.counts[w.cursor] = 1 - min(w.counts[w.cursor], 1)
		}
	case "+", "=":
		if e.Installed && w.counts[w.cursor] < 5 {
			w.counts[w.cursor]++
		}
	case "-":
		if w.counts[w.cursor] > 0 {
			w.counts[w.cursor]--
		}
	case "enter":
		total := 0
		for _, c := range w.counts {
			total += c
		}
		if total == 0 {
			w.err = i18n.T("Pick at least one AI.")
			return nil
		}
		w.syncDrafts()
		return w.focusDraft(0, pHandle)
	}
	return nil
}

func (w *wizard) moveInstalled(dir int) int {
	for i := w.cursor + dir; i >= 0 && i < len(w.engines); i += dir {
		if w.engines[i].Installed {
			return i
		}
	}
	return w.cursor
}

func firstInstalled(list []engines.Engine) int {
	for i, e := range list {
		if e.Installed {
			return i
		}
	}
	return 0
}

func (w *wizard) viewPick() string {
	var b strings.Builder
	b.WriteString(sLabel.Render(i18n.T("Who joins the group?")) + "\n\n")
	for i, e := range w.engines {
		on := i == w.cursor
		if !e.Installed {
			b.WriteString("  " + sFaint.Render("[ ] "+engineName(e.Name)+"  "+engineNote(e.Note)) + "\n")
			continue
		}
		name := sText.Render(engineName(e.Name))
		if on {
			name = sActive.Render(engineName(e.Name))
		}
		line := pointer(on) + check(w.counts[i] > 0) + " " + name
		if w.counts[i] > 1 {
			line += sMuted.Render(fmt.Sprintf("  x%d", w.counts[i]))
		}
		b.WriteString(line + "\n")
	}
	return b.String()
}

// syncDrafts monta a lista de perfis a partir das IAs escolhidas,
// reaproveitando o que já foi preenchido.
func (w *wizard) syncDrafts() {
	old := map[string]*draft{}
	for _, d := range w.drafts {
		old[fmt.Sprintf("%s#%d", d.engine.ID, d.nth)] = d
	}
	w.drafts = nil
	for i, e := range w.engines {
		for n := 0; n < w.counts[i]; n++ {
			if d, ok := old[fmt.Sprintf("%s#%d", e.ID, n)]; ok {
				w.drafts = append(w.drafts, d)
				continue
			}
			w.drafts = append(w.drafts, w.newDraft(e, n))
		}
	}
	w.resize(w.width)
}

func (w *wizard) newDraft(e engines.Engine, n int) *draft {
	d := &draft{engine: e, nth: n}
	d.handle = newInput(i18n.T("handle"), 20)
	d.handle.Prompt = sMuted.Render("@")
	d.handle.SetValue(w.freeHandle(e.Handle, n))
	d.role = newInput(i18n.T("E.g. browse the web and bring back the numbers"), 200)
	d.role.Prompt = ""
	d.color = w.freeColor()
	return d
}

// freeHandle sugere um apelido que ninguém do grupo usa.
func (w *wizard) freeHandle(base string, n int) string {
	h := base
	if n > 0 {
		h = fmt.Sprintf("%s%d", base, n+1)
	}
	for i := 2; w.handleTaken(h, nil); i++ {
		h = fmt.Sprintf("%s%d", base, i)
	}
	return h
}

func (w *wizard) handleTaken(h string, except *draft) bool {
	for _, d := range w.drafts {
		if d != except && cleanHandle(d.handle.Value()) == h {
			return true
		}
	}
	return false
}

func (w *wizard) freeColor() int {
	used := map[int]bool{}
	for _, d := range w.drafts {
		used[d.color] = true
	}
	for i := range palette {
		if !used[i] {
			return i
		}
	}
	return len(w.drafts) % len(palette)
}

// --- Perfis ---

func (w *wizard) focusDraft(i, field int) tea.Cmd {
	w.step = stepProfiles
	w.cur = i
	w.field = field
	d := w.drafts[i]
	d.handle.Blur()
	d.role.Blur()
	switch field {
	case pHandle:
		return d.handle.Focus()
	case pRole:
		return d.role.Focus()
	}
	return nil
}

func (w *wizard) updateProfiles(msg tea.Msg, key tea.KeyMsg, isKey bool) tea.Cmd {
	d := w.drafts[w.cur]
	if isKey {
		k := key.String()
		switch {
		case k == "tab" || k == "down":
			return w.focusDraft(w.cur, (w.field+1)%pCount)
		case k == "shift+tab" || k == "up":
			return w.focusDraft(w.cur, (w.field+pCount-1)%pCount)
		case k == "enter":
			if err := w.validateDraft(d); err != "" {
				w.err = err
				return nil
			}
			if w.cur+1 < len(w.drafts) {
				return w.focusDraft(w.cur+1, pHandle)
			}
			w.clampLead()
			if len(w.drafts) == 1 {
				w.step = stepReview
			} else {
				w.step = stepLead
			}
			return nil
		case w.field >= pModel && (k == "left" || k == "right" || k == " "):
			dir := 1
			if k == "left" {
				dir = -1
			}
			w.cycle(d, dir)
			return nil
		}
	}
	var cmd tea.Cmd
	switch w.field {
	case pHandle:
		d.handle, cmd = d.handle.Update(msg)
		// o @ já aparece na frente; apelido é sempre minúsculo e sem espaço
		if v := d.handle.Value(); v != liveHandle(v) {
			d.handle.SetValue(liveHandle(v))
		}
	case pRole:
		d.role, cmd = d.role.Update(msg)
	}
	return cmd
}

func (w *wizard) cycle(d *draft, dir int) {
	switch w.field {
	case pModel:
		if n := len(d.engine.Models); n > 0 {
			d.model = (d.model + dir + n) % n
		}
	case pColor:
		d.color = (d.color + dir + len(palette)) % len(palette)
	case pAccess:
		d.full = !d.full
	}
}

func (w *wizard) validateDraft(d *draft) string {
	h := cleanHandle(d.handle.Value())
	switch {
	case h == "":
		w.focusDraft(w.cur, pHandle)
		return i18n.T("Give this AI a handle.")
	case !handleRe.MatchString(h):
		w.focusDraft(w.cur, pHandle)
		return i18n.T("Use lowercase letters, numbers, - or _ for handles (up to 20).")
	case reservedHandles[h] || h == chat.Me():
		w.focusDraft(w.cur, pHandle)
		return i18n.T("@%s is reserved. Pick another.", h)
	case w.handleTaken(h, d):
		w.focusDraft(w.cur, pHandle)
		return i18n.T("@%s is already in the group.", h)
	case strings.TrimSpace(d.role.Value()) == "":
		w.focusDraft(w.cur, pRole)
		return i18n.T("Say in one sentence how @%s will help.", h)
	}
	d.handle.SetValue(h)
	return ""
}

func (w *wizard) viewProfile() string {
	d := w.drafts[w.cur]
	hex := palette[d.color].Hex
	h := cleanHandle(d.handle.Value())
	if h == "" {
		h = "..."
	}

	var b strings.Builder
	b.WriteString(sMuted.Render(i18n.T("AI %d of %d  ·  ", w.cur+1, len(w.drafts))) +
		handleStyle(hex).Render(engineName(d.engine.Name)) + "\n\n")

	// coluna dos rótulos do tamanho do maior, medido em colunas da tela
	// (japonês e chinês ocupam duas colunas por letra)
	labels := []string{i18n.T("Handle"), i18n.T("Role"), i18n.T("Model"), i18n.T("Color"), i18n.T("Access")}
	col := 10
	for _, l := range labels {
		col = max(col, lipgloss.Width(l)+2)
	}
	row := func(f int, label, value string) {
		padded := label + strings.Repeat(" ", col-lipgloss.Width(label))
		lbl := sLabel.Render(padded)
		if w.field == f {
			lbl = sActive.Render(padded)
		}
		b.WriteString(pointer(w.field == f) + lbl + value + "\n")
	}
	choice := func(f int, value string) string {
		if w.field == f {
			return sMuted.Render("‹ ") + sText.Render(value) + sMuted.Render(" ›")
		}
		return sText.Render(value)
	}

	row(pHandle, i18n.T("Handle"), d.handle.View())
	row(pRole, i18n.T("Role"), d.role.View())
	b.WriteString(strings.Repeat(" ", 2+col) + sFaint.Render(i18n.T("What should @%s help with?", h)) + "\n")
	model := engines.Default
	if len(d.engine.Models) > 0 {
		model = d.engine.Models[d.model]
	}
	row(pModel, i18n.T("Model"), choice(pModel, modelText(model)))
	row(pColor, i18n.T("Color"), choice(pColor, handleStyle(hex).Render(i18n.T(palette[d.color].Name))))
	row(pAccess, i18n.T("Access"), choice(pAccess, accessText(d.full)))

	b.WriteString("\n" + sMuted.Render(i18n.T("In chat: ")) + at(h, hex))
	return b.String()
}

func accessText(full bool) string {
	if full {
		return i18n.T("Whole Mac (browser and apps)")
	}
	return i18n.T("Group folder only")
}

// --- Líder ---

func (w *wizard) clampLead() {
	if w.lead >= len(w.drafts) {
		w.lead = 0
	}
}

func (w *wizard) updateLead(key tea.KeyMsg) tea.Cmd {
	switch key.String() {
	case "up", "k", "shift+tab":
		w.lead = max(w.lead-1, 0)
	case "down", "j", "tab":
		w.lead = min(w.lead+1, len(w.drafts)-1)
	case "enter":
		w.step = stepReview
	}
	return nil
}

func (w *wizard) viewLead() string {
	var b strings.Builder
	b.WriteString(sLabel.Render(i18n.T("Who leads the group?")) + "\n")
	b.WriteString(sFaint.Render(i18n.T("Gets your messages without @ and splits the work among the others.")) + "\n\n")
	for i, d := range w.drafts {
		h := "@" + d.handle.Value()
		h += strings.Repeat(" ", max(1, 21-lipgloss.Width(h)))
		b.WriteString(pointer(i == w.lead) + handleStyle(palette[d.color].Hex).Render(h) +
			sMuted.Render(engineName(d.engine.Name)) + "\n")
	}
	return b.String()
}

// --- Revisão ---

func (w *wizard) build() store.Group {
	g := store.Group{
		Name:   strings.TrimSpace(w.name.Value()),
		Goal:   strings.TrimSpace(w.goal.Value()),
		Folder: expand(w.folder.Value()),
	}
	for i, d := range w.drafts {
		m := store.Member{
			Handle:     cleanHandle(d.handle.Value()),
			Engine:     d.engine.ID,
			Role:       strings.TrimSpace(d.role.Value()),
			Color:      palette[d.color].Hex,
			FullAccess: d.full,
		}
		if len(d.engine.Models) > 0 && d.engine.Models[d.model] != engines.Default {
			m.Model = d.engine.Models[d.model]
		}
		if i == w.lead {
			g.Lead = m.Handle
		}
		g.Members = append(g.Members, m)
	}
	return g
}

func (w *wizard) viewReview() string {
	g := w.build()
	return sLabel.Render(i18n.T("Name")) + "\n" + sText.Render(g.Name) + "\n\n" + groupSummary(g, w.engines, max(20, w.width-6))
}

// --- Tela ---

func (w *wizard) View() string {
	var b strings.Builder
	b.WriteString(sTitle.Render(i18n.T("New group")) + "   " + w.progress() + "\n\n")
	switch w.step {
	case stepBasics:
		b.WriteString(w.viewBasics())
	case stepPick:
		b.WriteString(w.viewPick())
	case stepProfiles:
		b.WriteString(w.viewProfile())
	case stepLead:
		b.WriteString(w.viewLead())
	case stepReview:
		b.WriteString(w.viewReview())
	}
	if w.err != "" {
		b.WriteString("\n\n" + sError.Render(w.err))
	}
	b.WriteString("\n" + sHelp.Render(w.help()))
	return b.String()
}

func (w *wizard) progress() string {
	parts := make([]string, len(stepNames))
	for i, n := range stepNames {
		n = i18n.T(n)
		switch {
		case i == w.step:
			parts[i] = sActive.Render(n)
		case i < w.step:
			parts[i] = sMuted.Render(n)
		default:
			parts[i] = sFaint.Render(n)
		}
	}
	return strings.Join(parts, sFaint.Render(" › "))
}

func (w *wizard) help() string {
	switch w.step {
	case stepBasics:
		return i18n.T("tab next field · enter continue · esc cancel")
	case stepPick:
		return i18n.T("↑↓ navigate · space select · + add a copy · enter continue · esc back")
	case stepProfiles:
		return i18n.T("↑↓ switch field · ←→ change option · enter next AI · esc back")
	case stepLead:
		return i18n.T("↑↓ choose · enter continue · esc back")
	default:
		return i18n.T("enter create group · esc back")
	}
}

// --- utilidades ---

func cleanHandle(s string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(s), "@"))
}

func liveHandle(s string) string {
	return strings.ToLower(strings.NewReplacer("@", "", " ", "").Replace(s))
}

func expand(p string) string {
	p = strings.TrimSpace(p)
	if p == "~" || strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		p = filepath.Join(home, strings.TrimPrefix(p, "~"))
	}
	return filepath.Clean(p)
}

func tildify(p string) string {
	home, _ := os.UserHomeDir()
	if home != "" && (p == home || strings.HasPrefix(p, home+"/")) {
		return "~" + strings.TrimPrefix(p, home)
	}
	return p
}
