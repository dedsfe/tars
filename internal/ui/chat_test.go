package ui

import (
	"testing"

	"github.com/dedsfe/tars/internal/config"
	"github.com/dedsfe/tars/internal/store"
)

// O aviso de "falaram demais" aparece uma vez só por rodada, mesmo que as
// IAs continuem se marcando depois do limite.
func TestLoopNoticeOncePerRound(t *testing.T) {
	t.Setenv("TARS_DIR", t.TempDir())
	st, err := store.Open()
	if err != nil {
		t.Fatal(err)
	}
	g := store.Group{ID: "g", Name: "G", Folder: t.TempDir(), Lead: "a",
		Members: []store.Member{{Handle: "a", Engine: "claude"}, {Handle: "b", Engine: "codex"}}}
	c := newChat(st, g, nil, 100, 30)
	c.cfg = config.Default()
	c.cfg.MaxAgentTurns = 1
	c.root = 1
	c.budget[1] = 1

	for i := 0; i < 3; i++ { // três ondas de marcações depois do limite
		c.enqueue(job{handle: "a", root: 1})
		c.enqueue(job{handle: "b", root: 1})
		c.pump()
	}
	notices := 0
	for _, m := range c.msgs {
		if m.From == store.System {
			notices++
		}
	}
	if notices != 1 {
		t.Fatalf("aviso apareceu %d vezes, queria 1", notices)
	}
	if len(c.running) != 0 {
		t.Fatalf("ninguém deveria ter começado a trabalhar: %v", c.running)
	}
}
