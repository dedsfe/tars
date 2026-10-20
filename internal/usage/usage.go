// Package usage acompanha quanto cada IA já gastou do limite do seu plano.
//
// Claude: vem no evento rate_limit_event de cada resposta (guardamos a
// última em usage.json). Codex: vem nos registros de sessão que ele mesmo
// grava em ~/.codex/sessions. Antigravity não informa limite.
package usage

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// Window é uma janela de limite: a de 5 horas ou a da semana.
type Window struct {
	Name     string    `json:"name"` // "5h" ou "semana"
	Used     float64   `json:"used"` // 0 a 100
	ResetsAt time.Time `json:"resets_at"`
}

// Limits é o retrato do limite de uma IA num momento.
type Limits struct {
	Windows []Window  `json:"windows"`
	At      time.Time `json:"at"`
}

// Worst é a janela mais perto de acabar que ainda não reiniciou.
func (l Limits) Worst(now time.Time) (Window, bool) {
	var best Window
	found := false
	for _, w := range l.Windows {
		if !w.ResetsAt.IsZero() && now.After(w.ResetsAt) {
			continue // já reiniciou, o número é velho
		}
		if !found || w.Used > best.Used {
			best, found = w, true
		}
	}
	return best, found
}

// Exhausted diz se a IA bateu o limite e até quando.
func (l Limits) Exhausted(now time.Time) (time.Time, bool) {
	if w, ok := l.Worst(now); ok && w.Used >= 100 {
		return w.ResetsAt, true
	}
	return time.Time{}, false
}

func windowName(minutes int) string {
	if minutes >= 24*60 {
		return "semana"
	}
	return "5h"
}

// --- Claude ---

// ClaudeEvent lê um evento rate_limit_event da saída stream-json do Claude.
func ClaudeEvent(line []byte) (Limits, bool) {
	var ev struct {
		Type string `json:"type"`
		Info struct {
			Windows map[string]struct {
				Utilization float64 `json:"utilization"`
				ResetsAt    int64   `json:"resetsAt"`
			} `json:"unifiedWindows"`
		} `json:"rate_limit_info"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "rate_limit_event" || len(ev.Info.Windows) == 0 {
		return Limits{}, false
	}
	l := Limits{At: time.Now()}
	for key, w := range ev.Info.Windows {
		name := "5h"
		if strings.Contains(key, "day") || strings.Contains(key, "week") {
			name = "semana"
		}
		l.Windows = append(l.Windows, Window{Name: name, Used: w.Utilization * 100, ResetsAt: time.Unix(w.ResetsAt, 0)})
	}
	sortWindows(l.Windows)
	return l, true
}

// --- Codex ---

// Codex lê o último limite registrado nas sessões do Codex.
func Codex() (Limits, bool) {
	home, err := os.UserHomeDir()
	if err != nil {
		return Limits{}, false
	}
	// as sessões ficam em sessions/AAAA/MM/DD; olha hoje e os dias anteriores
	for d := 0; d < 7; d++ {
		day := time.Now().AddDate(0, 0, -d).Format("2006/01/02")
		files, _ := filepath.Glob(filepath.Join(home, ".codex", "sessions", day, "*.jsonl"))
		sort.Slice(files, func(i, j int) bool { return modTime(files[i]).After(modTime(files[j])) })
		for _, f := range files {
			if l, ok := codexFile(f); ok {
				return l, true
			}
		}
	}
	return Limits{}, false
}

func codexFile(path string) (Limits, bool) {
	f, err := os.Open(path)
	if err != nil {
		return Limits{}, false
	}
	defer f.Close()

	type win struct {
		UsedPercent   float64 `json:"used_percent"`
		WindowMinutes int     `json:"window_minutes"`
		ResetsAt      int64   `json:"resets_at"`
	}
	var last *struct {
		Primary   *win `json:"primary"`
		Secondary *win `json:"secondary"`
	}
	var at time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 16<<20)
	for sc.Scan() {
		line := sc.Bytes()
		if !strings.Contains(string(line), `"rate_limits"`) {
			continue
		}
		var ev struct {
			Timestamp time.Time `json:"timestamp"`
			Payload   struct {
				RateLimits *struct {
					Primary   *win `json:"primary"`
					Secondary *win `json:"secondary"`
				} `json:"rate_limits"`
			} `json:"payload"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Payload.RateLimits != nil {
			last, at = ev.Payload.RateLimits, ev.Timestamp
		}
	}
	if last == nil {
		return Limits{}, false
	}
	l := Limits{At: at}
	for _, w := range []*win{last.Primary, last.Secondary} {
		if w != nil {
			l.Windows = append(l.Windows, Window{Name: windowName(w.WindowMinutes), Used: w.UsedPercent, ResetsAt: time.Unix(w.ResetsAt, 0)})
		}
	}
	sortWindows(l.Windows)
	return l, len(l.Windows) > 0
}

func modTime(p string) time.Time {
	if info, err := os.Stat(p); err == nil {
		return info.ModTime()
	}
	return time.Time{}
}

func sortWindows(ws []Window) {
	sort.Slice(ws, func(i, j int) bool { return ws[i].Name == "5h" && ws[j].Name != "5h" })
}

// --- guardado entre execuções ---

// Cache guarda o último limite de cada IA em usage.json.
type Cache struct {
	path   string
	By     map[string]Limits    `json:"by"`
	Tokens map[string]DayTokens `json:"tokens,omitempty"`
}

// DayTokens soma os tokens que uma IA gastou pelo tars num dia.
// Serve pras IAs que não informam o limite do plano (Antigravity).
type DayTokens struct {
	Day   string `json:"day"` // AAAA-MM-DD
	Total int64  `json:"total"`
}

// Today devolve os tokens gastos hoje por essa IA.
func (c *Cache) Today(engine string) int64 {
	if t, ok := c.Tokens[engine]; ok && t.Day == time.Now().Format("2006-01-02") {
		return t.Total
	}
	return 0
}

// AddTokens soma tokens ao dia de hoje.
func (c *Cache) AddTokens(engine string, n int64) {
	if n <= 0 {
		return
	}
	if c.Tokens == nil {
		c.Tokens = map[string]DayTokens{}
	}
	day := time.Now().Format("2006-01-02")
	t := c.Tokens[engine]
	if t.Day != day {
		t = DayTokens{Day: day}
	}
	t.Total += n
	c.Tokens[engine] = t
	c.save()
}

func (c *Cache) save() {
	if data, err := json.MarshalIndent(c, "", "  "); err == nil {
		_ = os.WriteFile(c.path, data, 0o644)
	}
}

func OpenCache(dir string) *Cache {
	c := &Cache{path: filepath.Join(dir, "usage.json"), By: map[string]Limits{}}
	if data, err := os.ReadFile(c.path); err == nil {
		_ = json.Unmarshal(data, c)
		if c.By == nil {
			c.By = map[string]Limits{}
		}
	}
	return c
}

func (c *Cache) Set(engine string, l Limits) {
	if old, ok := c.By[engine]; ok && old.At.After(l.At) {
		return // já tem um retrato mais novo
	}
	c.By[engine] = l
	c.save()
}
