// Package sys lê coisas do Mac que afetam as IAs, como o espaço em disco.
package sys

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"

	"github.com/dedsfe/tars/internal/i18n"
)

const (
	GB = 1 << 30
	MB = 1 << 20
)

// Limites de disco; SetLimits troca pelos do config.toml.
var (
	DiskLow   uint64 = 5 * GB   // amarelo no topo
	DiskPause uint64 = 2 * GB   // ninguém começa a trabalhar abaixo disso
	DiskKill  uint64 = 500 * MB // quem está trabalhando é parado
)

func SetLimits(warnGB, pauseGB, stopMB float64) {
	DiskLow = uint64(warnGB * GB)
	DiskPause = uint64(pauseGB * GB)
	DiskKill = uint64(stopMB * MB)
}

// FreeDisk devolve quanto sobra no disco da sua pasta pessoal.
// 0 quando não deu pra ler.
func FreeDisk() uint64 {
	// TARS_DISK_FILE aponta pra um arquivo com MB livres, pra testar sem encher o disco
	if f := os.Getenv("TARS_DISK_FILE"); f != "" {
		if data, err := os.ReadFile(f); err == nil {
			if mb, err := strconv.ParseUint(strings.TrimSpace(string(data)), 10, 64); err == nil {
				return mb * MB
			}
		}
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return 0
	}
	var st syscall.Statfs_t
	if syscall.Statfs(home, &st) != nil {
		return 0
	}
	return st.Bavail * uint64(st.Bsize)
}

// Size escreve bytes do jeito brasileiro: "1,3 GB", "450 MB".
func Size(b uint64) string {
	if b >= GB {
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(b)/GB), ".", i18n.Decimal(), 1)
	}
	return fmt.Sprintf("%d MB", b/MB)
}

// HasMole diz se o Mole (limpador do Mac) está instalado.
func HasMole() bool {
	_, err := exec.LookPath("mo")
	return err == nil
}

// MoleClean é o comando da limpeza do Mole, pra rodar no lugar do tars.
func MoleClean() *exec.Cmd { return exec.Command("mo", "clean") }

// OpenMole abre a limpeza do Mole numa janela nova do mesmo app de
// terminal que você está usando, pro tars continuar aberto.
func OpenMole() error {
	var script string
	switch os.Getenv("TERM_PROGRAM") {
	case "iTerm.app":
		script = `tell application "iTerm"
	activate
	create window with default profile command "mo clean"
end tell`
	default:
		script = `tell application "Terminal"
	activate
	do script "mo clean"
end tell`
	}
	return exec.Command("osascript", "-e", script).Run()
}
