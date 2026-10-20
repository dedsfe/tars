package usage

import (
	"testing"
	"time"
)

func TestClaudeEvent(t *testing.T) {
	line := []byte(`{"type":"rate_limit_event","rate_limit_info":{"unifiedWindows":{"five_hour":{"utilization":0.34,"resetsAt":4102444800},"seven_day":{"utilization":0.9,"resetsAt":4102444800}}}}`)
	l, ok := ClaudeEvent(line)
	if !ok || len(l.Windows) != 2 || l.Windows[0].Name != "5h" {
		t.Fatalf("evento lido errado: %+v", l)
	}
	w, _ := l.Worst(time.Now())
	if w.Name != "semana" || w.Used != 90 {
		t.Errorf("pior janela deveria ser a semana a 90%%, veio %+v", w)
	}
	if _, ok := ClaudeEvent([]byte(`{"type":"result"}`)); ok {
		t.Error("outro evento não deveria virar limite")
	}
}

func TestExhaustedIgnoresWindowsThatReset(t *testing.T) {
	now := time.Now()
	l := Limits{Windows: []Window{{Name: "semana", Used: 100, ResetsAt: now.Add(-time.Minute)}}}
	if _, over := l.Exhausted(now); over {
		t.Error("janela que já reiniciou não deveria contar como estourada")
	}
	l.Windows[0].ResetsAt = now.Add(time.Hour)
	if reset, over := l.Exhausted(now); !over || !reset.Equal(l.Windows[0].ResetsAt) {
		t.Error("janela a 100% deveria estar estourada até reiniciar")
	}
}

func TestTokensResetEachDay(t *testing.T) {
	c := OpenCache(t.TempDir())
	c.AddTokens("agy", 500)
	c.AddTokens("agy", 700)
	if got := c.Today("agy"); got != 1200 {
		t.Errorf("Today = %d, quer 1200", got)
	}
	c.Tokens["agy"] = DayTokens{Day: "2000-01-01", Total: 99}
	if got := c.Today("agy"); got != 0 {
		t.Errorf("tokens de outro dia não deveriam contar, veio %d", got)
	}
}
