package engines

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// TurnTimeout é o máximo que uma IA pode ficar trabalhando numa vez.
const TurnTimeout = 15 * time.Minute

// Turn é uma vez de uma IA falar.
type Turn struct {
	Engine string
	Model  string // vazio = padrão da IA
	Folder string
	Prompt string
}

// Run roda a IA sem interface e devolve a resposta final dela.
// Todas rodam no modo seguro de cada CLI: editam arquivos da pasta do
// grupo, mas nada de liberar permissões sem você aprovar.
func Run(ctx context.Context, t Turn) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, TurnTimeout)
	defer cancel()

	args, outFile, err := command(t)
	if err != nil {
		return "", err
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
		return "", fmt.Errorf("passou de %d minutos sem terminar", int(TurnTimeout.Minutes()))
	}
	if ctx.Err() == context.Canceled {
		return "", fmt.Errorf("parado")
	}

	reply := stdout.String()
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
		return "", fmt.Errorf("%v: %s", runErr, tail(detail, 400))
	}
	if reply == "" {
		return "", fmt.Errorf("terminou sem responder nada")
	}
	return reply, nil
}

// command monta os argumentos de cada IA. O Codex escreve a resposta
// final num arquivo (outFile) em vez da saída padrão.
func command(t Turn) (args []string, outFile string, err error) {
	model := t.Model
	switch t.Engine {
	case "claude":
		args = []string{"-p", t.Prompt, "--output-format", "text", "--permission-mode", "acceptEdits"}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "codex":
		f, err := os.CreateTemp("", "bot-codex-*.txt")
		if err != nil {
			return nil, "", err
		}
		f.Close()
		outFile = f.Name()
		args = []string{"exec", "--skip-git-repo-check", "-C", t.Folder, "-s", "workspace-write", "-o", outFile}
		if model != "" {
			args = append(args, "-m", model)
		}
		args = append(args, t.Prompt)
	case "gemini":
		args = []string{"-p", t.Prompt, "--approval-mode", "auto_edit"}
		if model != "" {
			args = append(args, "-m", model)
		}
	case "agy":
		args = []string{"-p", t.Prompt, "--output-format", "text", "--add-dir", t.Folder, "--mode", "accept-edits"}
		if model != "" {
			args = append(args, "--model", model)
		}
	case "cursor-agent":
		args = []string{"-p", "--output-format", "text", "--workspace", t.Folder}
		if model != "" {
			args = append(args, "--model", model)
		}
		args = append(args, t.Prompt)
	case "opencode":
		args = []string{"run", "--dir", t.Folder}
		if model != "" {
			args = append(args, "-m", model)
		}
		args = append(args, t.Prompt)
	case "ollama":
		if model == "" {
			return nil, "", fmt.Errorf("escolhe um modelo do Ollama")
		}
		args = []string{"run", model, t.Prompt}
	default:
		return nil, "", fmt.Errorf("não sei rodar %q", t.Engine)
	}
	return args, outFile, nil
}

var ansi = regexp.MustCompile(`\x1b\[[0-9;?]*[ -/]*[@-~]`)

func tail(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return "…" + string(r[len(r)-n:])
}
