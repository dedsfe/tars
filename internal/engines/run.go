package engines

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/dedsfe/tars/internal/i18n"
	"github.com/dedsfe/tars/internal/usage"
)

// Result é o que a IA respondeu e, quando ela informa, o limite do plano.
type Result struct {
	Text   string
	Limits *usage.Limits
	Tokens int64 // tokens gastos, quando a IA informa
}

// TurnTimeout é o máximo que uma IA pode ficar trabalhando numa vez.
const TurnTimeout = 15 * time.Minute

// Turn é uma vez de uma IA falar.
type Turn struct {
	Engine string
	Model  string // vazio = padrão da IA
	Folder string
	Prompt string
	// FullAccess é o "Mac inteiro" que você escolheu no perfil da IA:
	// ela roda sem pedir permissão pra nada.
	FullAccess bool
	// Images são imagens da conversa que a IA deve conseguir abrir.
	Images []string
}

var ErrStopped = errors.New("stopped")

// Run roda a IA sem interface e devolve a resposta final dela.
// Sem FullAccess roda no modo seguro de cada CLI (edita a pasta do grupo).
func Run(ctx context.Context, t Turn) (Result, error) {
	ctx, cancel := context.WithTimeout(ctx, TurnTimeout)
	defer cancel()

	args, outFile, err := command(t)
	if err != nil {
		return Result{}, err
	}
	if outFile != "" {
		defer os.Remove(outFile)
	}

	cmd := exec.CommandContext(ctx, t.Engine, args...)
	cmd.Dir = t.Folder
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr

	runErr := cmd.Run()
	if ctx.Err() == context.DeadlineExceeded {
		return Result{}, errors.New(i18n.T("took more than %d minutes without finishing", int(TurnTimeout.Minutes())))
	}
	if ctx.Err() == context.Canceled {
		return Result{}, ErrStopped
	}

	var res Result
	reply := stdout.String()
	if t.Engine == "claude" {
		text, limits, claudeErr := claudeStream(stdout.Bytes())
		res.Limits = limits
		if claudeErr != nil {
			return res, claudeErr
		}
		reply = text
	}
	if t.Engine == "agy" {
		text, tokens, agyErr := agyStream(stdout.Bytes())
		res.Tokens = tokens
		if agyErr != nil {
			return res, agyErr
		}
		if text != "" {
			reply = text
		}
	}
	if outFile != "" {
		if data, err := os.ReadFile(outFile); err == nil {
			reply = string(data)
		}
	}
	reply = strings.TrimSpace(ansi.ReplaceAllString(reply, ""))

	if runErr != nil {
		detail := strings.TrimSpace(ansi.ReplaceAllString(stderr.String(), ""))
		if detail == "" {
			detail = reply
		}
		return res, fmt.Errorf("%v: %s", runErr, tail(detail, 400))
	}
	if reply == "" {
		return res, errors.New(i18n.T("finished without a reply"))
	}
	res.Text = reply
	return res, nil
}

// claudeStream tira da saída stream-json do Claude a resposta final e o limite.
func claudeStream(out []byte) (string, *usage.Limits, error) {
	var text string
	var limits *usage.Limits
	for _, line := range bytes.Split(out, []byte("\n")) {
		if l, ok := usage.ClaudeEvent(line); ok {
			limits = &l
			continue
		}
		var ev struct {
			Type    string `json:"type"`
			Result  string `json:"result"`
			IsError bool   `json:"is_error"`
		}
		if json.Unmarshal(line, &ev) == nil && ev.Type == "result" {
			if ev.IsError {
				return "", limits, fmt.Errorf("%s", tail(ev.Result, 400))
			}
			text = ev.Result
		}
	}
	return text, limits, nil
}

// command monta os argumentos de cada IA. O Codex escreve a resposta
// final num arquivo (outFile) em vez da saída padrão.
func command(t Turn) (args []string, outFile string, err error) {
	model := t.Model
	switch t.Engine {
	case "claude":
		args = []string{"-p", t.Prompt, "--output-format", "stream-json", "--verbose"}
		for _, d := range dirs(t.Images) {
			args = append(args, "--add-dir="+d)
		}
		if t.FullAccess {
			args = append(args, "--dangerously-skip-permissions")
		} else {
			args = append(args, "--permission-mode", "acceptEdits")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "codex":
		f, err := os.CreateTemp("", "tars-codex-*.txt")
		if err != nil {
			return nil, "", err
		}
		f.Close()
		outFile = f.Name()
		args = []string{"exec", "--skip-git-repo-check", "-C", t.Folder, "-o", outFile}
		for _, img := range t.Images {
			args = append(args, "--image="+img)
		}
		if t.FullAccess {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		} else {
			args = append(args, "-s", "workspace-write")
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		args = append(args, t.Prompt)
	case "agy":
		args = []string{"-p", t.Prompt, "--output-format", "stream-json", "--add-dir", t.Folder}
		for _, d := range dirs(t.Images) {
			args = append(args, "--add-dir", d)
		}
		if t.FullAccess {
			args = append(args, "--dangerously-skip-permissions")
		} else {
			args = append(args, "--mode", "accept-edits")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "cursor-agent":
		args = []string{"-p", "--output-format", "text", "--workspace", t.Folder}
		if t.FullAccess {
			args = append(args, "--force", "--approve-mcps")
		}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, t.Prompt)
	case "opencode":
		args = []string{"run", "--dir", t.Folder}
		for _, img := range t.Images {
			args = append(args, "--file="+img)
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		args = append(args, t.Prompt)
	case "ollama":
		if model == "" {
			return nil, "", errors.New(i18n.T("pick an Ollama model"))
		}
		args = []string{"run", model, t.Prompt}
	default:
		return nil, "", errors.New(i18n.T("do not know how to run %q", t.Engine))
	}
	return args, outFile, nil
}

// agyStream tira da saída stream-json do Antigravity a resposta e os tokens.
func agyStream(out []byte) (string, int64, error) {
	for _, line := range bytes.Split(out, []byte("\n")) {
		var ev struct {
			Event  string `json:"event"`
			Result struct {
				Status   string `json:"status"`
				Response string `json:"response"`
				Usage    struct {
					Total int64 `json:"total_tokens"`
				} `json:"usage"`
			} `json:"result"`
		}
		if json.Unmarshal(line, &ev) != nil || ev.Event != "result" {
			continue
		}
		if ev.Result.Status != "SUCCESS" {
			return "", ev.Result.Usage.Total, errors.New(i18n.T("finished with status %s: %s", ev.Result.Status, tail(ev.Result.Response, 300)))
		}
		return strings.TrimSpace(ev.Result.Response), ev.Result.Usage.Total, nil
	}
	return "", 0, nil
}

// dirs são as pastas das imagens, sem repetir.
func dirs(images []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, img := range images {
		if d := filepath.Dir(img); !seen[d] {
			seen[d] = true
			out = append(out, d)
		}
	}
	return out
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}
