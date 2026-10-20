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
	// FullAccess é o "Mac inteiro" que você escolheu no perfil da IA:
	// ela roda sem pedir permissão pra nada.
	FullAccess bool
}

// Run roda a IA sem interface e devolve a resposta final dela.
// Sem FullAccess roda no modo seguro de cada CLI (edita a pasta do grupo).
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
		args = []string{"-p", t.Prompt, "--output-format", "text"}
		if t.FullAccess {
			args = append(args, "--dangerously-skip-permissions")
		} else {
			args = append(args, "--permission-mode", "acceptEdits")
		}
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
		args = []string{"exec", "--skip-git-repo-check", "-C", t.Folder, "-o", outFile}
		if t.FullAccess {
			args = append(args, "--dangerously-bypass-approvals-and-sandbox")
		} else {
			args = append(args, "-s", "workspace-write")
		}
		if model != "" {
			args = append(args, "-m", model)
		}
		args = append(args, t.Prompt)
	case "gemini":
		args = []string{"-p", t.Prompt}
		if t.FullAccess {
			args = append(args, "--yolo")
		} else {
			args = append(args, "--approval-mode", "auto_edit")
		}
		if model != "" {
			args = append(args, "-m", model)
		}
	case "agy":
		args = []string{"-p", t.Prompt, "--output-format", "text", "--add-dir", t.Folder}
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
