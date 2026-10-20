package engines

import (
	"reflect"
	"testing"
)

// Linhas reais tiradas da saída de cada CLI.
func TestActivityParsers(t *testing.T) {
	cases := []struct {
		engine, line string
		want         []Activity
	}{
		{"claude", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Bash","input":{"command":"echo hi > a.txt && ls","description":"x"}}]}}`,
			[]Activity{{ActRun, "echo hi > a.txt && ls"}}},
		{"claude", `{"type":"assistant","message":{"content":[{"type":"tool_use","name":"Edit","input":{"file_path":"/p/index.html"}},{"type":"text","text":"ok"}]}}`,
			[]Activity{{ActEdit, "/p/index.html"}}},
		{"claude", `{"type":"user","message":{"content":[{"type":"tool_result","content":"a.txt"}]}}`, nil},
		{"codex", `{"type":"item.started","item":{"id":"item_2","type":"command_execution","command":"/bin/zsh -lc 'printf hi > a.txt'","status":"in_progress"}}`,
			[]Activity{{ActRun, "printf hi > a.txt"}}},
		{"codex", `{"type":"item.started","item":{"id":"item_3","type":"command_execution","command":"/bin/zsh -lc ls"}}`,
			[]Activity{{ActRun, "ls"}}},
		{"codex", `{"type":"item.completed","item":{"type":"file_change","changes":[{"path":"/p/a.css","kind":"update"},{"path":"/p/b.js","kind":"add"}]}}`,
			[]Activity{{ActEdit, "/p/a.css"}, {ActWrite, "/p/b.js"}}},
		{"agy", `{"event":"step_update","step_update":{"step_index":2,"state":"ACTIVE","step_type":"tool","tool_name":"write_file","tool_info":{"name":"write_file","parameters":{"Path":"/p/a.txt"}}}}`,
			[]Activity{{ActWrite, "/p/a.txt"}}},
		{"agy", `{"event":"step_update","step_update":{"step_index":3,"state":"ACTIVE","step_type":"tool","tool_name":"run_command","tool_info":{"name":"run_command","parameters":{"CommandLine":"ls"}}}}`,
			[]Activity{{ActRun, "ls"}}},
		{"agy", `{"event":"step_update","step_update":{"step_index":3,"state":"DONE","step_type":"tool","tool_name":"run_command","tool_info":{"parameters":{"CommandLine":"ls"}}}}`, nil},
	}
	for _, c := range cases {
		got := activityParser(c.engine)([]byte(c.line))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s %s:\n got %v\nwant %v", c.engine, c.line, got, c.want)
		}
	}
}

func TestLinesSplitsChunks(t *testing.T) {
	var got []string
	l := &lines{line: func(b []byte) { got = append(got, string(b)) }}
	l.Write([]byte("ab"))
	l.Write([]byte("c\nde\nf"))
	l.Write([]byte("\n"))
	if !reflect.DeepEqual(got, []string{"abc", "de", "f"}) {
		t.Errorf("got %q", got)
	}
}
