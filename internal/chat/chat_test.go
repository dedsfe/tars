package chat

import (
	"reflect"
	"strings"
	"testing"

	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/store"
)

var group = store.Group{
	Name: "Sites", Goal: "Fazer sites.", Lead: "chefe",
	Members: []store.Member{
		{Handle: "chefe", Engine: "claude", Role: "Organiza"},
		{Handle: "dev", Engine: "codex", Role: "Código"},
		{Handle: "agy", Engine: "agy", Role: "Design"},
	},
}

func TestMentions(t *testing.T) {
	cases := []struct {
		text, self string
		want       []string
	}{
		{"@dev faz isso e @agy aquilo", "", []string{"dev", "agy"}},
		{"@DEV maiúsculo", "", []string{"dev"}},
		{"@dev @dev repetido", "", []string{"dev"}},
		{"@ninguem não existe", "", nil},
		{"@todos bora", "", []string{"chefe", "dev", "agy"}},
		{"@todos bora", "dev", []string{"chefe", "agy"}},
		{"@all go", "", []string{"chefe", "dev", "agy"}},
		{"me chamo @chefe", "chefe", nil},
		{"email a@dev.com não é marcação de verdade? é sim", "", []string{"dev"}},
	}
	for _, c := range cases {
		if got := Mentions(c.text, group, c.self); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Mentions(%q, self=%q) = %v, quer %v", c.text, c.self, got, c.want)
		}
	}
}

func TestTargets(t *testing.T) {
	if got := Targets("sem marcação", group, false); !reflect.DeepEqual(got, []string{"chefe"}) {
		t.Errorf("sem @ deveria ir pro líder, foi pra %v", got)
	}
	if got := Targets("sem marcação", group, true); len(got) != 3 {
		t.Errorf("sem @ com all deveria ir pra todos, foi pra %v", got)
	}
	if got := Targets("@agy só você", group, true); !reflect.DeepEqual(got, []string{"agy"}) {
		t.Errorf("com @ vai só pro marcado, foi pra %v", got)
	}
}

func TestPrompt(t *testing.T) {
	history := []store.Message{{Text: "oi"}, {From: "dev", Text: "fiz", Images: []string{"/x/print.png"}}}
	p := Prompt(group, group.Members[0], "você", history)
	for _, want := range []string{"You are @chefe", "Fazer sites.", "Write your messages in English.", "You are the leader", "@" + Me(), "[@dev] fiz [anexou a imagem print.png]"} {
		if !strings.Contains(p, want) {
			t.Errorf("prompt sem %q", want)
		}
	}
	if strings.Contains(Prompt(group, group.Members[1], "@chefe", nil), "You are the leader") {
		t.Error("quem não é líder não deveria receber instrução de líder")
	}
	old := i18n.Lang()
	t.Cleanup(func() { i18n.Set(old) })
	for _, lang := range []string{"pt", "es", "fr", "de", "it", "ja", "zh"} {
		i18n.Set(lang)
		p := Prompt(group, group.Members[0], "@"+Me(), history)
		if !strings.Contains(p, "Write your messages in "+i18n.LanguageName()+".") {
			t.Errorf("%s: missing response language", lang)
		}
		if !strings.Contains(p, "[@dev] fiz [anexou a imagem print.png]") {
			t.Errorf("%s: transcript changed", lang)
		}
	}

}

func TestTranscriptKeepsTheEnd(t *testing.T) {
	var history []store.Message
	for i := 0; i < 100; i++ {
		history = append(history, store.Message{Text: strings.Repeat("x", 1000)})
	}
	history = append(history, store.Message{Text: "última"})
	got := transcript(history)
	if !strings.HasSuffix(strings.TrimSpace(got), "última") || len(got) > historyChars+200 {
		t.Errorf("transcript deveria guardar o fim dentro do limite (tamanho %d)", len(got))
	}
}
