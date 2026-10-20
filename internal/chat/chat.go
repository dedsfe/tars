// Package chat decide quem fala e monta o que cada IA lê antes de responder.
package chat

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"strings"
	"sync"

	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/store"
)

// Everyone é o @ que chama o grupo inteiro; EveryoneEN faz o mesmo em inglês.
const (
	Everyone   = "todos"
	EveryoneEN = "all"
)

// MaxAgentTurns limita quantas vezes as IAs falam entre si depois de
// uma mensagem sua, pra conversa não virar loop infinito.
const MaxAgentTurns = 12

const (
	historyMessages = 40
	historyChars    = 24000
	messageChars    = 4000
)

var mentionRe = regexp.MustCompile(`@([A-Za-z0-9][A-Za-z0-9_-]*)`)

var me = sync.OnceValue(func() string {
	if h := strings.ToLower(strings.TrimPrefix(os.Getenv("TARS_ME"), "@")); h != "" {
		return h
	}
	if u, err := user.Current(); err == nil {
		if f := strings.Fields(u.Name); len(f) > 0 {
			if h := asciiHandle(f[0]); h != "" {
				return h
			}
		}
	}
	return "voce"
})

// Me é o seu @ no grupo, tirado do seu primeiro nome no Mac (TARS_ME troca).
func Me() string { return me() }

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
	"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c",
)

func asciiHandle(s string) string {
	var b strings.Builder
	for _, r := range accents.Replace(strings.ToLower(s)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MentionsMe diz se o texto marca você.
func MentionsMe(text string) bool {
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		if h := strings.ToLower(m[1]); h == Me() || h == Everyone || h == EveryoneEN {
			return true
		}
	}
	return false
}

// Notify mostra um aviso do macOS (título, subtítulo e texto).
func Notify(title, subtitle, text string) {
	text = oneLine(text)
	if r := []rune(text); len(r) > 180 {
		text = string(r[:179]) + "…"
	}
	script := `on run argv
display notification (item 1 of argv) with title (item 2 of argv) subtitle (item 3 of argv) sound name "Glass"
end run`
	_ = exec.Command("osascript", "-e", script, text, title, subtitle).Start()
}

// Mentions devolve os membros marcados no texto, na ordem, sem repetir.
// @todos vira todo mundo. self é ignorado (ninguém se chama).
func Mentions(text string, g store.Group, self string) []string {
	seen := map[string]bool{self: true}
	var out []string
	add := func(h string) {
		if !seen[h] {
			seen[h] = true
			out = append(out, h)
		}
	}
	for _, m := range mentionRe.FindAllStringSubmatch(text, -1) {
		h := strings.ToLower(m[1])
		if h == Everyone || h == EveryoneEN {
			for _, mem := range g.Members {
				add(mem.Handle)
			}
			continue
		}
		if _, ok := Member(g, h); ok {
			add(h)
		}
	}
	return out
}

// EveryoneHandle é o @ de todo mundo que a tela sugere no idioma atual
// (os dois sempre funcionam).
func EveryoneHandle() string {
	switch i18n.Lang() {
	case "pt", "es":
		return Everyone
	}
	return EveryoneEN
}

// Targets diz quem responde a uma mensagem sua: os marcados ou, sem @,
// o líder (ou todo mundo, se all).
func Targets(text string, g store.Group, all bool) []string {
	if t := Mentions(text, g, ""); len(t) > 0 {
		return t
	}
	if all {
		return Mentions("@"+Everyone, g, "")
	}
	return []string{g.Lead}
}

func Member(g store.Group, handle string) (store.Member, bool) {
	for _, m := range g.Members {
		if m.Handle == handle {
			return m, true
		}
	}
	return store.Member{}, false
}

// Prompt é tudo que a IA lê na vez dela: quem ela é, o grupo e a conversa.
func Prompt(g store.Group, me store.Member, calledBy string, history []store.Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "You are @%s, a member of a terminal chat group called %q.\n\n", me.Handle, g.Name)
	fmt.Fprintf(&b, "GROUP GOAL\n%s\n\n", g.Goal)
	fmt.Fprintf(&b, "YOUR ROLE\n%s\n\n", me.Role)

	b.WriteString("MEMBERS\n")
	fmt.Fprintf(&b, "- @%s (the human who owns the group)\n", Me())
	for _, m := range g.Members {
		lead := ""
		if m.Handle == g.Lead {
			lead = ", leader"
		}
		fmt.Fprintf(&b, "- @%s (%s%s): %s\n", m.Handle, m.Engine, lead, oneLine(m.Role))
	}
	fmt.Fprintf(&b, "\nWORKING FOLDER\n%s\n\n", g.Folder)

	b.WriteString("HOW THE GROUP WORKS\n")
	fmt.Fprintf(&b, "- Write your messages in %s.\n", i18n.LanguageName())
	b.WriteString("- Talk like you are in a group chat: keep it short and direct.\n")
	b.WriteString("- Do the actual work with your tools before saying it is done. Say what you did and where to find it.\n")
	b.WriteString("- To ask another member for something, write @handle and your request. They will wake up and read the conversation.\n")
	b.WriteString("- Only mention someone when you need them to do something. Never mention them just to say thanks, agree, or say goodbye.\n")
	fmt.Fprintf(&b, "- To speak to the human, mention @%s: they get a notification on their Mac. Mention them when you need a decision or deliver something they requested.\n", Me())
	b.WriteString("- When your part is done and you do not need anyone, do not mention anyone.\n")
	if me.Handle == g.Lead {
		b.WriteString("- You are the leader: split up the work by mentioning the right person for each part, then bring the results together for the human.\n")
	}

	b.WriteString("\nCONVERSATION SO FAR\n")
	b.WriteString(transcript(history))

	if imgs := Images(history); len(imgs) > 0 {
		b.WriteString("\nCONVERSATION IMAGES (open with your file-reading tool when useful)\n")
		for _, img := range imgs {
			fmt.Fprintf(&b, "- %s\n", img)
		}
	}

	fmt.Fprintf(&b, "\nIt is your turn now, @%s.", me.Handle)
	if calledBy != "" {
		fmt.Fprintf(&b, " Called by: %s.", calledBy)
	}
	b.WriteString(" Reply only with your message to the group.\n")
	return b.String()
}

// transcript pega o fim da conversa, cabendo no limite de caracteres.
func transcript(history []store.Message) string {
	if len(history) > historyMessages {
		history = history[len(history)-historyMessages:]
	}
	var lines []string
	total := 0
	for i := len(history) - 1; i >= 0; i-- {
		text := history[i].Text
		for _, img := range history[i].Images {
			text += " [anexou a imagem " + filepath.Base(img) + "]"
		}
		line := "[" + Speaker(history[i].From) + "] " + clip(text, messageChars)
		if total+len(line) > historyChars {
			break
		}
		total += len(line)
		lines = append([]string{line}, lines...)
	}
	if len(lines) == 0 {
		return "(vazia)\n"
	}
	return strings.Join(lines, "\n") + "\n"
}

// MaxImages é quantas imagens recentes cada IA recebe junto da conversa.
const MaxImages = 4

// Images são as imagens mais recentes da conversa que ainda existem no disco.
func Images(history []store.Message) []string {
	var out []string
	for i := len(history) - 1; i >= 0 && len(out) < MaxImages; i-- {
		imgs := history[i].Images
		for j := len(imgs) - 1; j >= 0 && len(out) < MaxImages; j-- {
			if _, err := os.Stat(imgs[j]); err == nil {
				out = append([]string{imgs[j]}, out...)
			}
		}
	}
	return out
}

// Speaker é como cada remetente aparece na conversa.
func Speaker(from string) string {
	switch from {
	case "":
		return i18n.T("@%s (human)", Me())
	case store.System:
		return i18n.T("tars notice")
	}
	return "@" + from
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + " [cortado]"
}

func oneLine(s string) string { return strings.Join(strings.Fields(s), " ") }
