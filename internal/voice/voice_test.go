package voice

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// Gera uma fala com o say do macOS e confere se o Whisper transcreve.
// Só roda quando o modelo já está baixado em ~/.bot.
func TestTranscribe(t *testing.T) {
	home, _ := os.UserHomeDir()
	root := filepath.Join(home, ".bot")
	if !Ready(root) {
		t.Skip("Whisper ou modelo não instalado")
	}
	dir := t.TempDir()
	aiff, wav := filepath.Join(dir, "fala.aiff"), filepath.Join(dir, "fala.wav")
	if err := exec.Command("say", "-v", "Eddy (Portuguese (Brazil))", "-o", aiff,
		"Oi pessoal, o claudinho monta a paleta e o codexinho faz a página inicial.").Run(); err != nil {
		t.Skip("say sem voz em português:", err)
	}
	if out, err := exec.Command("ffmpeg", "-loglevel", "error", "-y", "-i", aiff, "-ar", "16000", "-ac", "1", wav).CombinedOutput(); err != nil {
		t.Fatal(string(out))
	}
	text, err := Transcribe(context.Background(), root, wav, []string{"claudinho", "codexinho", "agy"})
	if err != nil {
		t.Fatal(err)
	}
	t.Log(text)
	for _, want := range []string{"paleta", "página inicial", "codexinho"} {
		if !strings.Contains(strings.ToLower(text), want) {
			t.Errorf("faltou %q em %q", want, text)
		}
	}
}
