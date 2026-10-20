// Package voice grava sua voz pelo microfone e transcreve no próprio Mac
// com o Whisper (whisper.cpp), sem mandar áudio pra internet.
package voice

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"

	"github.com/dedsfe/tars/internal/i18n"
)

// Model é o modelo do Whisper: bom em português e rápido no Mac.
const (
	Model    = "ggml-large-v3-turbo-q5_0.bin"
	ModelURL = "https://huggingface.co/ggerganov/whisper.cpp/resolve/main/" + Model
)

// ModelPath é onde o modelo fica guardado.
func ModelPath(root string) string { return filepath.Join(root, "modelos", Model) }

// MissingTools são os pacotes do Homebrew que faltam pra gravar e transcrever.
func MissingTools() []string {
	var out []string
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		out = append(out, "ffmpeg")
	}
	if _, err := exec.LookPath("whisper-cli"); err != nil {
		out = append(out, "whisper-cpp")
	}
	return out
}

// HasModel diz se o modelo do Whisper já foi baixado.
func HasModel(root string) bool {
	_, err := os.Stat(ModelPath(root))
	return err == nil
}

// Ready diz se dá pra gravar e transcrever agora.
func Ready(root string) bool { return len(MissingTools()) == 0 && HasModel(root) }

// Download baixa o modelo, avisando o progresso. Grava num .part e só
// renomeia no fim, pra nunca sobrar um modelo pela metade.
func Download(ctx context.Context, root string, progress func(done, total int64)) error {
	dst := ModelPath(root)
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ModelURL, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New(i18n.T("download returned %s", resp.Status))
	}
	part := dst + ".part"
	f, err := os.Create(part)
	if err != nil {
		return err
	}
	var done int64
	buf := make([]byte, 1<<20)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, err := f.Write(buf[:n]); err != nil {
				f.Close()
				os.Remove(part)
				return err
			}
			done += int64(n)
			progress(done, resp.ContentLength)
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			f.Close()
			os.Remove(part)
			return rerr
		}
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(part, dst)
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
			msg = i18n.T("no audio recorded. Check Terminal microphone access in Settings > Privacy")
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
		return "", errors.New(i18n.T("Whisper failed: %v", err))
	}
	// tira marcações como [música] ou (silêncio) e junta as linhas
	text := strings.Join(strings.Fields(bracketRe.ReplaceAllString(string(out), "")), " ")
	if text == "" {
		return "", errors.New(i18n.T("could not understand the audio"))
	}
	return text, nil
}

// microphone escolhe o microfone do Mac, não o do iPhone (Continuidade).
// Microphone (do config.toml) ou TARS_MIC trocam pelo índice ou nome que você quiser.
var Microphone string

func microphone() string {
	if m := os.Getenv("TARS_MIC"); m != "" {
		return m
	}
	if Microphone != "" {
		return Microphone
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
