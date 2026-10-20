// Package store guarda os grupos em ~/.bot/grupos, um JSON por grupo.
package store

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Member é uma IA dentro de um grupo.
type Member struct {
	Handle     string `json:"handle"` // sem o @
	Engine     string `json:"engine"` // id do engines.Engine
	Model      string `json:"model,omitempty"`
	Role       string `json:"role"` // em que ela ajuda
	Color      string `json:"color"`
	FullAccess bool   `json:"full_access"` // false = só a pasta do grupo
}

// Group é uma conversa com objetivo, pasta, membros e um líder.
type Group struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Goal      string    `json:"goal"`
	Folder    string    `json:"folder"`
	Lead      string    `json:"lead"` // handle do líder
	Members   []Member  `json:"members"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct{ dir string }

// Open usa ~/.bot/grupos, ou BOT_DIR quando definido (útil pra testes).
func Open() (*Store, error) {
	dir := os.Getenv("BOT_DIR")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, err
		}
		dir = filepath.Join(home, ".bot", "grupos")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	return &Store{dir: dir}, nil
}

// List devolve os grupos do mais novo pro mais antigo.
func (s *Store) List() ([]Group, error) {
	paths, err := filepath.Glob(filepath.Join(s.dir, "*.json"))
	if err != nil {
		return nil, err
	}
	groups := make([]Group, 0, len(paths))
	for _, p := range paths {
		data, err := os.ReadFile(p)
		if err != nil {
			return nil, err
		}
		var g Group
		if err := json.Unmarshal(data, &g); err != nil {
			return nil, fmt.Errorf("%s: %w", filepath.Base(p), err)
		}
		groups = append(groups, g)
	}
	sort.Slice(groups, func(i, j int) bool { return groups[i].CreatedAt.After(groups[j].CreatedAt) })
	return groups, nil
}

// Save grava o grupo de forma atômica, criando ID e data se faltarem.
func (s *Store) Save(g *Group) error {
	if g.CreatedAt.IsZero() {
		g.CreatedAt = time.Now()
	}
	if g.ID == "" {
		g.ID = slug(g.Name) + "-" + strconv.FormatInt(g.CreatedAt.Unix(), 36)
	}
	data, err := json.MarshalIndent(g, "", "  ")
	if err != nil {
		return err
	}
	final := filepath.Join(s.dir, g.ID+".json")
	tmp := final + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, final)
}

var accents = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i",
	"ó", "o", "ô", "o", "õ", "o", "ú", "u", "ü", "u", "ç", "c",
)

func slug(name string) string {
	var b strings.Builder
	dash := false
	for _, r := range accents.Replace(strings.ToLower(name)) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	out := strings.TrimSuffix(b.String(), "-")
	if out == "" {
		return "grupo"
	}
	return out
}

// Message é uma fala no chat do grupo.
type Message struct {
	From  string    `json:"from"` // handle da IA, "" = você, "sistema" = aviso do bot
	Text  string    `json:"text"`
	At    time.Time `json:"at"`
	Error bool      `json:"error,omitempty"`
}

// System é o remetente dos avisos do próprio bot.
const System = "sistema"

func (s *Store) chatPath(groupID string) string {
	return filepath.Join(s.dir, groupID+".chat.jsonl")
}

// Messages lê o histórico do chat do grupo.
func (s *Store) Messages(groupID string) ([]Message, error) {
	data, err := os.ReadFile(s.chatPath(groupID))
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var msgs []Message
	for _, line := range strings.Split(string(data), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		var m Message
		if json.Unmarshal([]byte(line), &m) == nil {
			msgs = append(msgs, m)
		}
	}
	return msgs, nil
}

// Append grava uma mensagem no fim do chat.
func (s *Store) Append(groupID string, m Message) error {
	line, err := json.Marshal(m)
	if err != nil {
		return err
	}
	f, err := os.OpenFile(s.chatPath(groupID), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.Write(append(line, '\n'))
	return err
}
