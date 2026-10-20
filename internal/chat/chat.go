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

	"bot/internal/store"
)

// Everyone é o @ que chama o grupo inteiro.
const Everyone = "todos"

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
	if h := strings.ToLower(strings.TrimPrefix(os.Getenv("BOT_ME"), "@")); h != "" {
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

// Me é o seu @ no grupo, tirado do seu primeiro nome no Mac (BOT_ME troca).
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
		if h := strings.ToLower(m[1]); h == Me() || h == Everyone {
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
		if h == Everyone {
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
	fmt.Fprintf(&b, "Você é @%s, membro de um grupo de chat chamado %q que roda no terminal.\n\n", me.Handle, g.Name)
	fmt.Fprintf(&b, "OBJETIVO DO GRUPO\n%s\n\n", g.Goal)
	fmt.Fprintf(&b, "SUA FUNÇÃO\n%s\n\n", me.Role)

	b.WriteString("MEMBROS\n")
	fmt.Fprintf(&b, "- @%s (o humano dono do grupo)\n", Me())
	for _, m := range g.Members {
		lead := ""
		if m.Handle == g.Lead {
			lead = ", líder"
		}
		fmt.Fprintf(&b, "- @%s (%s%s): %s\n", m.Handle, m.Engine, lead, oneLine(m.Role))
	}
	fmt.Fprintf(&b, "\nPASTA DE TRABALHO\n%s\n\n", g.Folder)

	b.WriteString("COMO O GRUPO FUNCIONA\n")
	b.WriteString("- Fale como numa conversa de grupo: curto, direto, em português do Brasil.\n")
	b.WriteString("- Faça o trabalho de verdade com suas ferramentas antes de dizer que fez. Diga o que fez e onde ficou.\n")
	b.WriteString("- Pra pedir algo a outro membro, escreva @apelido e o pedido. Ele é acordado e lê a conversa.\n")
	b.WriteString("- Só marque alguém quando precisar que ele faça algo. Nunca marque pra agradecer, concordar ou se despedir.\n")
	fmt.Fprintf(&b, "- Pra falar com o humano, marque @%s: ele recebe um aviso no Mac. Marque ele quando precisar de uma decisão dele ou quando entregar algo que ele pediu.\n", Me())
	b.WriteString("- Quando terminar sua parte e não precisar de ninguém, não marque ninguém.\n")
	if me.Handle == g.Lead {
		b.WriteString("- Você é o líder: divida o trabalho marcando quem combina com cada parte, e no fim junte o resultado pra você (o humano).\n")
	}

	b.WriteString("\nCONVERSA ATÉ AGORA\n")
	b.WriteString(transcript(history))

	if imgs := Images(history); len(imgs) > 0 {
		b.WriteString("\nIMAGENS DA CONVERSA (abra com sua ferramenta de ler arquivos quando forem úteis)\n")
		for _, img := range imgs {
			fmt.Fprintf(&b, "- %s\n", img)
		}
	}

	fmt.Fprintf(&b, "\nAgora é sua vez, @%s.", me.Handle)
	if calledBy != "" {
		fmt.Fprintf(&b, " Quem te chamou: %s.", calledBy)
	}
	b.WriteString(" Responda só com a sua mensagem pro grupo.\n")
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
		return "@" + Me() + " (humano)"
	case store.System:
		return "aviso do bot"
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
