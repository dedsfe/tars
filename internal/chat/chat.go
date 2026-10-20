// Package chat decide quem fala e monta o que cada IA lê antes de responder.
package chat

import (
	"fmt"
	"regexp"
	"strings"

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

// Targets diz quem responde a uma mensagem sua: os marcados ou, sem @, o líder.
func Targets(text string, g store.Group) []string {
	if t := Mentions(text, g, ""); len(t) > 0 {
		return t
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
	b.WriteString("- você (o humano dono do grupo; chame de \"você\", ele não tem @)\n")
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
	b.WriteString("- Quando terminar sua parte e não precisar de ninguém, não marque ninguém.\n")
	if me.Handle == g.Lead {
		b.WriteString("- Você é o líder: divida o trabalho marcando quem combina com cada parte, e no fim junte o resultado pra você (o humano).\n")
	}

	b.WriteString("\nCONVERSA ATÉ AGORA\n")
	b.WriteString(transcript(history))

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
		line := "[" + Speaker(history[i].From) + "] " + clip(history[i].Text, messageChars)
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

// Speaker é como cada remetente aparece na conversa.
func Speaker(from string) string {
	switch from {
	case "":
		return "você"
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
