package attach

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func TestExtract(t *testing.T) {
	dir := t.TempDir()
	spaced := filepath.Join(dir, "Screenshot 2026 at 11.50.png")
	plain := filepath.Join(dir, "foto.JPG")
	for _, p := range []string{spaced, plain} {
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	escaped := filepath.Join(dir, `Screenshot\ 2026\ at\ 11.50.png`)

	cases := []struct {
		in, text string
		paths    []string
	}{
		{"olha isso " + escaped + " @dev", "olha isso  @dev", []string{spaced}},
		{"'" + spaced + "' o que acha?", "o que acha?", []string{spaced}},
		{plain, "", []string{plain}},
		{"duas " + plain + " " + escaped, "duas", []string{plain, spaced}},
		{"não existe /tmp/nada-aqui.png ok", "não existe /tmp/nada-aqui.png ok", nil},
		{"sem imagem nenhuma", "sem imagem nenhuma", nil},
	}
	for _, c := range cases {
		text, paths := Extract(c.in)
		if text != c.text || !reflect.DeepEqual(paths, c.paths) {
			t.Errorf("Extract(%q)\n  = %q %v\n  quer %q %v", c.in, text, paths, c.text, c.paths)
		}
	}
}
