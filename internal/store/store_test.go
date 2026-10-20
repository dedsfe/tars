package store

import (
	"testing"
	"time"
)

func TestSaveListAndChat(t *testing.T) {
	t.Setenv("TARS_DIR", t.TempDir())
	st, err := Open()
	if err != nil {
		t.Fatal(err)
	}
	old := Group{Name: "Antigo", CreatedAt: time.Now().Add(-time.Hour)}
	novo := Group{Name: "Criação de Sites!"}
	for _, g := range []*Group{&old, &novo} {
		if err := st.Save(g); err != nil {
			t.Fatal(err)
		}
	}
	if novo.ID == "" || novo.ID[:16] != "criacao-de-sites" {
		t.Errorf("ID com acento/pontuação ficou %q", novo.ID)
	}
	groups, _ := st.List()
	if len(groups) != 2 || groups[0].Name != novo.Name {
		t.Errorf("List deveria vir do mais novo pro mais antigo: %+v", groups)
	}

	for _, m := range []Message{{Text: "oi"}, {From: "dev", Text: "olá", Images: []string{"/a.png"}}} {
		if err := st.Append(novo.ID, m); err != nil {
			t.Fatal(err)
		}
	}
	msgs, _ := st.Messages(novo.ID)
	if len(msgs) != 2 || msgs[1].From != "dev" || msgs[1].Images[0] != "/a.png" {
		t.Errorf("conversa lida errado: %+v", msgs)
	}
}
