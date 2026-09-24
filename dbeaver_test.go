package main

import (
	"path/filepath"
	"testing"

	"dbird/internal/secret"
	"dbird/internal/store"
)

func TestSameDatabase(t *testing.T) {
	pg := store.Connection{Driver: "postgres", Host: "DB.example.com", Port: 5432, Database: "app", User: "me"}
	cases := []struct {
		name string
		b    store.Connection
		want bool
	}{
		{"host case ignored", store.Connection{Driver: "postgres", Host: "db.example.com", Port: 5432, Database: "app", User: "me"}, true},
		{"other user", store.Connection{Driver: "postgres", Host: "db.example.com", Port: 5432, Database: "app", User: "you"}, false},
		{"other port", store.Connection{Driver: "postgres", Host: "db.example.com", Port: 5433, Database: "app", User: "me"}, false},
		{"other driver", store.Connection{Driver: "mysql", Host: "db.example.com", Port: 5432, Database: "app", User: "me"}, false},
	}
	for _, c := range cases {
		if got := sameDatabase(pg, c.b); got != c.want {
			t.Errorf("%s: got %v, want %v", c.name, got, c.want)
		}
	}
	a := store.Connection{Driver: "sqlite", Database: "/tmp/x.db"}
	if !sameDatabase(a, store.Connection{Driver: "sqlite", Database: "/tmp/x.db", Name: "other"}) {
		t.Error("sqlite connections to the same file should match")
	}
}

func TestImport(t *testing.T) {
	st, err := store.Open(filepath.Join(t.TempDir(), "dbird.json"))
	if err != nil {
		t.Fatal(err)
	}
	s := &ConnectionService{store: st, pw: newPasswords(&secret.Memory{})}

	bad := []store.Connection{
		{Name: "ok", Driver: "postgres"},
		{Name: "broken", Driver: "oracle"},
	}
	if _, err := s.Import(bad); err == nil {
		t.Fatal("expected an error for an unsupported driver")
	}
	if n := len(st.Connections()); n != 0 {
		t.Fatalf("a failed import saved %d connections", n)
	}

	n, err := s.Import([]store.Connection{
		{ID: "from-dbeaver", Name: " shop ", Driver: "mysql", Host: "h", Port: 3306},
		{Name: "local", Driver: "sqlite", Database: "/tmp/a.db"},
	})
	if err != nil || n != 2 {
		t.Fatalf("Import = %d, %v", n, err)
	}
	saved := st.Connections()
	if len(saved) != 2 || saved[0].ID == "from-dbeaver" || saved[0].ID == "" || saved[0].Name != "shop" {
		t.Errorf("saved = %+v", saved)
	}
}
