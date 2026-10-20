package engines

import (
	"bytes"
	"encoding/json"
	"regexp"
	"strings"
	"sync"
)

// Activity é uma coisa que a IA fez no meio da fala: editar um arquivo, rodar um comando...
type Activity struct {
	Kind   string `json:"kind"`   // edit, write, delete, read, run, search, web, tool
	Detail string `json:"detail"` // caminho, comando, busca ou nome da ferramenta
}

const (
	ActEdit   = "edit"
	ActWrite  = "write"
	ActDelete = "delete"
	ActRead   = "read"
	ActRun    = "run"
	ActSearch = "search"
	ActWeb    = "web"
	ActTool   = "tool"
)

// lines junta a saída em linhas inteiras e manda cada uma pro parser da IA.
type lines struct {
	mu   sync.Mutex
	buf  []byte
	line func([]byte)
}

func (l *lines) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf = append(l.buf, p...)
	for {
		i := bytes.IndexByte(l.buf, '\n')
		if i < 0 {
			break
		}
		l.line(l.buf[:i])
		l.buf = l.buf[i+1:]
	}
	return len(p), nil
}

// activityParser devolve quem entende os eventos ao vivo de cada IA (nil = ela não conta).
func activityParser(engine string) func([]byte) []Activity {
	switch engine {
	case "claude":
		return claudeActivity
	case "codex":
		return codexActivity
	case "agy":
		return agyActivity
	}
	return nil
}

func claudeActivity(line []byte) []Activity {
	var ev struct {
		Type    string `json:"type"`
		Message struct {
			Content []struct {
				Type  string         `json:"type"`
				Name  string         `json:"name"`
				Input map[string]any `json:"input"`
			} `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Type != "assistant" {
		return nil
	}
	var out []Activity
	for _, c := range ev.Message.Content {
		if c.Type != "tool_use" {
			continue
		}
		in := func(keys ...string) string { return firstString(c.Input, keys...) }
		switch c.Name {
		case "Edit", "MultiEdit", "NotebookEdit":
			out = append(out, Activity{ActEdit, in("file_path", "notebook_path")})
		case "Write":
			out = append(out, Activity{ActWrite, in("file_path")})
		case "Read":
			out = append(out, Activity{ActRead, in("file_path")})
		case "Bash":
			out = append(out, Activity{ActRun, in("command")})
		case "Glob", "Grep":
			out = append(out, Activity{ActSearch, in("pattern")})
		case "WebFetch", "WebSearch":
			out = append(out, Activity{ActWeb, in("url", "query")})
		case "TodoWrite":
		default:
			out = append(out, Activity{ActTool, c.Name})
		}
	}
	return out
}

// shellWrap tira o "/bin/zsh -lc '...'" que o Codex põe em volta dos comandos.
var shellWrap = regexp.MustCompile(`^/bin/(?:ba|z)?sh -l?c '?(.*?)'?$`)

func codexActivity(line []byte) []Activity {
	var ev struct {
		Type string `json:"type"`
		Item struct {
			Type    string `json:"type"`
			Command string `json:"command"`
			Query   string `json:"query"`
			Tool    string `json:"tool"`
			Changes []struct {
				Path string `json:"path"`
				Kind string `json:"kind"`
			} `json:"changes"`
		} `json:"item"`
	}
	if json.Unmarshal(line, &ev) != nil {
		return nil
	}
	it := ev.Item
	switch {
	case ev.Type == "item.started" && it.Type == "command_execution":
		cmd := it.Command
		if m := shellWrap.FindStringSubmatch(cmd); m != nil {
			cmd = m[1]
		}
		return []Activity{{ActRun, cmd}}
	case ev.Type == "item.completed" && it.Type == "file_change":
		var out []Activity
		for _, ch := range it.Changes {
			kind := ActEdit
			switch ch.Kind {
			case "add":
				kind = ActWrite
			case "delete":
				kind = ActDelete
			}
			out = append(out, Activity{kind, ch.Path})
		}
		return out
	case ev.Type == "item.started" && it.Type == "web_search":
		return []Activity{{ActWeb, it.Query}}
	case ev.Type == "item.started" && it.Type == "mcp_tool_call":
		return []Activity{{ActTool, it.Tool}}
	}
	return nil
}

func agyActivity(line []byte) []Activity {
	var ev struct {
		Event string `json:"event"`
		Step  struct {
			State    string `json:"state"`
			StepType string `json:"step_type"`
			Tool     string `json:"tool_name"`
			Info     struct {
				Parameters map[string]any `json:"parameters"`
			} `json:"tool_info"`
		} `json:"step_update"`
	}
	if json.Unmarshal(line, &ev) != nil || ev.Event != "step_update" || ev.Step.StepType != "tool" || ev.Step.State != "ACTIVE" {
		return nil
	}
	p := ev.Step.Info.Parameters
	path := firstString(p, "Path", "AbsolutePath", "TargetFile", "FilePath", "File", "DirectoryPath", "SearchDirectory")
	name := strings.ToLower(ev.Step.Tool)
	switch {
	case name == "run_command" || strings.Contains(name, "command"):
		return []Activity{{ActRun, firstString(p, "CommandLine", "Command")}}
	case strings.Contains(name, "write") || strings.Contains(name, "create"):
		return []Activity{{ActWrite, path}}
	case strings.Contains(name, "edit") || strings.Contains(name, "replace"):
		return []Activity{{ActEdit, path}}
	case strings.Contains(name, "delete"):
		return []Activity{{ActDelete, path}}
	case strings.Contains(name, "view") || strings.Contains(name, "read"):
		return []Activity{{ActRead, path}}
	case strings.Contains(name, "grep") || strings.Contains(name, "search") || strings.Contains(name, "find") || strings.Contains(name, "list"):
		if q := firstString(p, "Query", "Pattern"); q != "" {
			return []Activity{{ActSearch, q}}
		}
		return []Activity{{ActSearch, path}}
	case strings.HasPrefix(name, "browser") || strings.Contains(name, "url") || strings.Contains(name, "web"):
		return []Activity{{ActWeb, firstString(p, "Url", "URL", "Query")}}
	case strings.HasPrefix(name, "ask_"):
		return nil
	}
	return []Activity{{ActTool, ev.Step.Tool}}
}

func firstString(m map[string]any, keys ...string) string {
	for _, k := range keys {
		if s, ok := m[k].(string); ok && s != "" {
			return s
		}
	}
	return ""
}
