package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

func TestMarkdownLooksClean(t *testing.T) {
	in := "### Pronto\nOlha o [index.html](file:///tmp/index.html), **ficou\nbom** e usa `#F4A261`."
	got := ansi.Strip(styleMarkdown(plainMarkdown(in)))
	want := "Pronto\nOlha o index.html, ficou\nbom e usa #F4A261."
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
	if !strings.Contains(styleMarkdown("**a**"), "a") {
		t.Fatal("negrito sumiu com o texto")
	}
}
