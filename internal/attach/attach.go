// Package attach cuida das imagens que você manda no chat.
package attach

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const exts = `png|jpe?g|gif|webp|heic|heif`

// Caminhos de imagem como o Terminal cola quando você arrasta um arquivo:
// entre aspas simples, ou com os espaços escapados por barra.
var pathRe = regexp.MustCompile(`(?i)'((?:/|~/)[^']+\.(?:` + exts + `))'|((?:/|~/)(?:\\ |[^\s'])+?\.(?:` + exts + `))(?:\s|$)`)

// Extract tira do texto os caminhos de imagem que existem no disco.
// Devolve o texto sem eles e a lista de caminhos.
func Extract(text string) (string, []string) {
	var paths []string
	clean := pathRe.ReplaceAllStringFunc(text, func(match string) string {
		sub := pathRe.FindStringSubmatch(match)
		raw := sub[1]
		if raw == "" {
			raw = sub[2]
		}
		p := expand(strings.ReplaceAll(raw, `\ `, " "))
		if info, err := os.Stat(p); err != nil || info.IsDir() {
			return match // não é arquivo de verdade, deixa o texto como está
		}
		paths = append(paths, p)
		if strings.HasSuffix(match, " ") || strings.HasSuffix(match, "\n") {
			return match[len(match)-1:]
		}
		return ""
	})
	return strings.TrimSpace(clean), paths
}

// Save copia a imagem pra pasta de anexos do grupo (os prints do macOS
// ficam numa pasta temporária que some). HEIC vira PNG, porque nem toda
// IA lê HEIC.
func Save(src, dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	base := strings.TrimSuffix(filepath.Base(src), filepath.Ext(src))
	ext := strings.ToLower(filepath.Ext(src))
	stamp := time.Now().Format("20060102-150405")
	if ext == ".heic" || ext == ".heif" {
		dst := filepath.Join(dir, fmt.Sprintf("%s-%s.png", stamp, safe(base)))
		if out, err := exec.Command("sips", "-s", "format", "png", src, "--out", dst).CombinedOutput(); err != nil {
			return "", fmt.Errorf("não consegui converter a imagem: %s", strings.TrimSpace(string(out)))
		}
		return dst, nil
	}
	dst := filepath.Join(dir, fmt.Sprintf("%s-%s%s", stamp, safe(base), ext))
	return dst, copyFile(src, dst)
}

// Clipboard salva a imagem copiada (ex.: print com cmd+ctrl+shift+4)
// na pasta de anexos e devolve o caminho.
func Clipboard(dir string) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(dir, "print-"+time.Now().Format("20060102-150405")+".png")
	script := `on run argv
set f to open for access (POSIX file (item 1 of argv)) with write permission
try
	set eof f to 0
	write (the clipboard as «class PNGf») to f
	close access f
on error e
	close access f
	error e
end try
end run`
	if err := exec.Command("osascript", "-e", script, dst).Run(); err != nil {
		os.Remove(dst)
		return "", fmt.Errorf("não tem imagem copiada")
	}
	if info, err := os.Stat(dst); err != nil || info.Size() == 0 {
		os.Remove(dst)
		return "", fmt.Errorf("não tem imagem copiada")
	}
	return dst, nil
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

var unsafeRe = regexp.MustCompile(`[^A-Za-z0-9._-]+`)

func safe(name string) string {
	s := strings.Trim(unsafeRe.ReplaceAllString(name, "-"), "-")
	if len(s) > 40 {
		s = s[:40]
	}
	if s == "" {
		return "imagem"
	}
	return s
}

func expand(p string) string {
	if strings.HasPrefix(p, "~/") {
		home, _ := os.UserHomeDir()
		return filepath.Join(home, p[2:])
	}
	return p
}
