// Package sys lê coisas do Mac que afetam as IAs, como o espaço em disco.
package sys

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"syscall"
)

const (
	GB = 1 << 30
	MB = 1 << 20

	DiskLow   = 5 * GB   // amarelo no topo
	DiskPause = 2 * GB   // ninguém começa a trabalhar abaixo disso
	DiskKill  = 500 * MB // quem está trabalhando é parado
)

// FreeDisk devolve quanto sobra no disco da sua pasta pessoal.
// 0 quando não deu pra ler.
func FreeDisk() uint64 {
	// BOT_DISK_FILE aponta pra um arquivo com MB livres, pra testar sem encher o disco
	if f := os.Getenv("BOT_DISK_FILE"); f != "" {
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
		return strings.Replace(fmt.Sprintf("%.1f GB", float64(b)/GB), ".", ",", 1)
	}
	return fmt.Sprintf("%d MB", b/MB)
}

// HasMole diz se o Mole (limpador do Mac) está instalado.
func HasMole() bool {
	_, err := exec.LookPath("mo")
	return err == nil
}

// MoleClean é o comando que abre a limpeza do Mole no terminal.
func MoleClean() *exec.Cmd { return exec.Command("mo", "clean") }
