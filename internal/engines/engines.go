// Package engines descobre quais IAs de terminal estão instaladas no Mac.
package engines

import (
	"os/exec"
	"strings"
)

// Default é o modelo que deixa a própria IA escolher.
const Default = "padrão"

type Engine struct {
	ID        string // nome do binário
	Name      string
	Handle    string // apelido sugerido, sem @
	Models    []string
	Installed bool
	Note      string // por que não dá pra usar
}

var catalog = []Engine{
	{ID: "claude", Name: "Claude", Handle: "claude", Models: []string{Default, "opus", "sonnet", "haiku"}},
	{ID: "codex", Name: "Codex", Handle: "codex", Models: []string{Default}},
	{ID: "agy", Name: "Antigravity", Handle: "agy", Models: []string{Default}},
	{ID: "cursor-agent", Name: "Cursor", Handle: "cursor", Models: []string{Default}},
	{ID: "opencode", Name: "OpenCode", Handle: "opencode", Models: []string{Default}},
	{ID: "ollama", Name: "Ollama (local, grátis)", Handle: "ollama"},
}

// Detect devolve o catálogo inteiro, marcando o que está instalado.
func Detect() []Engine {
	out := make([]Engine, len(catalog))
	for i, e := range catalog {
		if _, err := exec.LookPath(e.ID); err != nil {
			e.Note = "não instalado"
		} else if e.ID == "ollama" {
			e.Models = ollamaModels()
			if len(e.Models) == 0 {
				e.Note = "nenhum modelo baixado"
			} else {
				e.Installed = true
			}
		} else {
			e.Installed = true
		}
		out[i] = e
	}
	return out
}

// Find acha uma IA pelo id.
func Find(list []Engine, id string) (Engine, bool) {
	for _, e := range list {
		if e.ID == id {
			return e, true
		}
	}
	return Engine{}, false
}

func ollamaModels() []string {
	out, err := exec.Command("ollama", "list").Output()
	if err != nil {
		return nil
	}
	var models []string
	for i, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		fields := strings.Fields(line)
		if i == 0 || len(fields) == 0 {
			continue // cabeçalho
		}
		models = append(models, fields[0])
	}
	return models
}
