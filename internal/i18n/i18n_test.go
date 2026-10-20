package i18n

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
)

var formatVerb = regexp.MustCompile(`%[-+# 0]*(?:\d+|\*)?(?:\.(?:\d+|\*))?[a-zA-Z%]`)

func verbs(text string) []string {
	var out []string
	for _, verb := range formatVerb.FindAllString(text, -1) {
		if verb != "%%" {
			out = append(out, verb[len(verb)-1:])
		}
	}
	return out
}

func TestCatalog(t *testing.T) {
	for src, translations := range catalog {
		if len(translations) != 7 {
			t.Errorf("%q: got %d languages, want 7", src, len(translations))
		}
		want := verbs(src)
		for _, lang := range []string{"pt", "es", "fr", "de", "it", "ja", "zh"} {
			text := translations[lang]
			if strings.TrimSpace(text) == "" {
				t.Errorf("%q: empty %s translation", src, lang)
			}
			got := verbs(text)
			if len(got) != len(want) || strings.Join(got, "") != strings.Join(want, "") {
				t.Errorf("%q (%s): verbs %v, want %v", src, lang, got, want)
			}
			if strings.Contains(text, "—") {
				t.Errorf("%q (%s): contains an em dash", src, lang)
			}
		}
		if strings.Contains(src, "—") {
			t.Errorf("%q: contains an em dash", src)
		}
		if _, ok := translations["en"]; ok {
			t.Errorf("%q: English must be the key only", src)
		}
	}
}

func TestDetectTARSLang(t *testing.T) {
	for _, tc := range []struct {
		env, config, want string
	}{
		{"pt_BR.UTF-8", "de", "pt"},
		{"es-ES", "pt", "es"},
		{"fr_CA", "auto", "fr"},
		{"de_DE", "", "de"},
		{"it-IT", "", "it"},
		{"ja_JP", "", "ja"},
		{"zh-Hans", "", "zh"},
		{"EN_us", "pt", "en"},
		{"xx_XX", "pt", "en"},
		{"", "it_IT", "it"},
		{"", "unsupported", "en"},
	} {
		t.Run(fmt.Sprintf("%s/%s", tc.env, tc.config), func(t *testing.T) {
			t.Setenv("TARS_LANG", tc.env)
			if got := Detect(tc.config); got != tc.want {
				t.Errorf("Detect(%q) = %q, want %q", tc.config, got, tc.want)
			}
		})
	}
}

func TestTranslationAndLanguage(t *testing.T) {
	old := Lang()
	t.Cleanup(func() { Set(old) })
	for _, tc := range []struct {
		lang, name, decimal string
	}{
		{"en", "English", "."},
		{"pt", "Brazilian Portuguese", ","},
		{"es", "Spanish", ","},
		{"fr", "French", ","},
		{"de", "German", ","},
		{"it", "Italian", ","},
		{"ja", "Japanese", "."},
		{"zh", "Simplified Chinese", "."},
	} {
		Set(tc.lang)
		if Lang() != tc.lang || LanguageName() != tc.name || Decimal() != tc.decimal {
			t.Errorf("%s: got %s, %s, %s", tc.lang, Lang(), LanguageName(), Decimal())
		}
		if got := T("missing %s", "translation"); got != "missing translation" {
			t.Errorf("fallback = %q", got)
		}
	}
	Set("pt")
	if got := T("disk %s free", "1,2 GB"); got != "disco 1,2 GB livre" {
		t.Errorf("translation = %q", got)
	}
	Set("en")
	if got := T("disk %s free", "1.2 GB"); got != "disk 1.2 GB free" {
		t.Errorf("English = %q", got)
	}
	Set("unsupported")
	if Lang() != "en" {
		t.Errorf("unsupported language = %q", Lang())
	}
}
