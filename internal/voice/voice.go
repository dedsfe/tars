// Package voice grava sua voz pelo microfone e transcreve no próprio Mac
// com o Whisper (whisper.cpp), sem mandar áudio pra internet.
package voice

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Model é o modelo do Whisper: bom em português e rápido no Mac.
const (
	Model    = "ggml-large-v3-turbo-q5_0.bin"
	ModelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + Model
)

// ModelPath é onde o modelo fica guardado.
func ModelPath(root string) string { return filepath.Join(root, "modelos", Model) }

// Missing diz o que falta instalar pra ouvir funcionar, ou "" se está tudo certo.
func Missing(root string) string {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		return "Falta o ffmpeg pra gravar. Instala com: brew install ffmpeg"
	}
	if _, err := exec.LookPath("whisper-cli"); err != nil {
		return "Falta o Whisper pra transcrever. Instala com: brew install whisper-cpp"
	}
	if _, err := os.Stat(ModelPath(root)); err != nil {
		return "Falta o modelo do Whisper (uns 570 MB). Baixa com: curl -L --create-dirs -o " +
			ModelPath(root) + " " + ModelURL
	}
	return ""
}

// Recording é uma gravação em andamento.
type Recording struct {
	cmd   *exec.Cmd
	path  string
	Start time.Time
	errb  bytes.Buffer
}

// Record começa a gravar o microfone num WAV de 16 kHz, o formato do Whisper.
func Record(dir string) (*Recording, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, err
	}
	r := &Recording{path: filepath.Join(dir, "voz-"+time.Now().Format("20060102-150405")+".wav"), Start: time.Now()}
	r.cmd = exec.Command("ffmpeg", "-hide_banner", "-loglevel", "error", "-y",
		"-f", "avfoundation", "-i", ":"+microphone(),
		"-ar", "16000", "-ac", "1", r.path)
	r.cmd.Stderr = &r.errb
	if err := r.cmd.Start(); err != nil {
		return nil, err
	}
	return r, nil
}

// Stop termina a gravação e devolve o arquivo de áudio.
func (r *Recording) Stop() (string, error) {
	_ = r.cmd.Process.Signal(syscall.SIGINT) // o ffmpeg fecha o arquivo direitinho
	done := make(chan error, 1)
	go func() { done <- r.cmd.Wait() }()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		_ = r.cmd.Process.Kill()
	}
	info, err := os.Stat(r.path)
	if err != nil || info.Size() < 4096 {
		msg := strings.TrimSpace(r.errb.String())
		if msg == "" {
			msg = "nenhum áudio gravado. Confere se o Terminal tem permissão de microfone em Ajustes > Privacidade"
		}
		return "", fmt.Errorf("%s", msg)
	}
	return r.path, nil
}

// Cancel joga a gravação fora.
func (r *Recording) Cancel() {
	_ = r.cmd.Process.Kill()
	_ = r.cmd.Wait()
	os.Remove(r.path)
}

var bracketRe = regexp.MustCompile(`\[[^\]]*\]|\([^)]*\)`)

// Transcribe passa o áudio pelo Whisper em português e apaga o arquivo.
// hints são palavras que o Whisper deve reconhecer, como os apelidos do grupo.
func Transcribe(ctx context.Context, root, wav string, hints []string) (string, error) {
	defer os.Remove(wav)
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	args := []string{"-m", ModelPath(root), "-l", "pt", "-nt", "-np", "-f", wav}
	if len(hints) > 0 {
		args = append(args, "--prompt", strings.Join(hints, ", ")+".")
	}
	out, err := exec.CommandContext(ctx, "whisper-cli", args...).Output()
	if err != nil {
		return "", fmt.Errorf("o Whisper falhou: %v", err)
	}
	// tira marcações como [música] ou (silêncio) e junta as linhas
	text := strings.Join(strings.Fields(bracketRe.ReplaceAllString(string(out), "")), " ")
	if text == "" {
		return "", fmt.Errorf("não entendi nada no áudio")
	}
	return text, nil
}

// microphone escolhe o microfone do Mac, não o do iPhone (Continuidade).
// BOT_MIC troca pelo índice ou nome que você quiser.
func microphone() string {
	if m := os.Getenv("BOT_MIC"); m != "" {
		return m
	}
	out, _ := exec.Command("ffmpeg", "-hide_banner", "-f", "avfoundation", "-list_devices", "true", "-i", "").CombinedOutput()
	audio := false
	first := ""
	for _, line := range strings.Split(string(out), "\n") {
		if strings.Contains(line, "audio devices") {
			audio = true
			continue
		}
		if !audio {
			continue
		}
		m := deviceRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		if first == "" {
			first = m[1]
		}
		name := strings.ToLower(m[2])
		if strings.Contains(name, "macbook") || strings.Contains(name, "built-in") || strings.Contains(name, "imac") {
			return m[1]
		}
	}
	if first == "" {
		return "0"
	}
	return first
}

var deviceRe = regexp.MustCompile(`\[(\d+)\] (.+)$`)
