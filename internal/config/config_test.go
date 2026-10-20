package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadCreatesCommentedDefault(t *testing.T) {
	dir := t.TempDir()
	cfg, err := Load(dir)
	if err != nil || cfg != Default() {
		t.Fatalf("primeira vez deveria ser o padrão: %+v %v", cfg, err)
	}
	again, err := Load(dir) // relê o arquivo que acabou de ser criado
	if err != nil || again != Default() {
		t.Fatalf("o arquivo criado deveria valer o padrão: %+v %v", again, err)
	}
}

func TestLoadFillsMissingAndInvalid(t *testing.T) {
	dir := t.TempDir()
	toml := "no_mention = \"qualquer\"\n[keys]\nvoice = \"alt+v\"\n[disk]\npause_gb = -1\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(toml), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := Load(dir)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Keys.Voice != "alt+v" || cfg.Keys.Paste != "ctrl+v" {
		t.Errorf("teclas: %+v", cfg.Keys)
	}
	if cfg.NoMention != NoMentionLead || cfg.Disk.PauseGB != 2 || cfg.Disk.Mole != MoleInside {
		t.Errorf("valor inválido deveria voltar ao padrão: %+v", cfg)
	}
}
