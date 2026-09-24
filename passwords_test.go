package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"dbird/internal/dbx"
	"dbird/internal/secret"
	"dbird/internal/store"
)

type fixture struct {
	t    *testing.T
	path string
	k    *secret.Memory
	s    *ConnectionService
}

func newFixture(t *testing.T) *fixture {
	f := &fixture{t: t, path: filepath.Join(t.TempDir(), "dbird.json"), k: &secret.Memory{}}
	f.restart()
	return f
}

// restart reopens the settings file with a fresh service, like a new run of
// dbird: passwords typed in the previous session are gone.
func (f *fixture) restart() {
	st, err := store.Open(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	f.s = &ConnectionService{store: st, dbm: dbx.NewManager(), pw: newPasswords(f.k)}
	f.s.migratePasswords()
}

// onDisk fails the test if the settings file contains secret.
func (f *fixture) notOnDisk(secret string) {
	f.t.Helper()
	b, err := os.ReadFile(f.path)
	if err != nil {
		f.t.Fatal(err)
	}
	if strings.Contains(string(b), secret) {
		f.t.Errorf("settings file contains %q:\n%s", secret, b)
	}
}

func (f *fixture) save(c store.Connection) store.Connection {
	f.t.Helper()
	saved, err := f.s.Save(c)
	if err != nil {
		f.t.Fatal(err)
	}
	return saved
}

func (f *fixture) password(id string) (string, error) {
	c, _ := f.s.store.Connection(id)
	c, err := f.s.pw.resolve(c)
	return c.Password, err
}

func pg(name string) store.Connection {
	return store.Connection{Name: name, Driver: "postgres", Host: "db", Port: 5432, User: "me"}
}

func TestSavePasswordGoesToPasswordStore(t *testing.T) {
	f := newFixture(t)
	c := pg("app")
	c.Password = "hunter2"
	saved := f.save(c)
	if !saved.HasPassword || saved.AskPassword || saved.Password != "" {
		t.Errorf("saved = %+v", saved)
	}
	if v, _ := f.k.Get(saved.ID); v != "hunter2" {
		t.Errorf("password store has %q", v)
	}
	f.notOnDisk("hunter2")
	for _, c := range f.s.List() {
		if c.Password != "" {
			t.Errorf("List exposes the password of %s", c.Name)
		}
	}

	f.restart()
	if pw, err := f.password(saved.ID); pw != "hunter2" || err != nil {
		t.Errorf("after restart: %q, %v", pw, err)
	}
}

func TestSaveKeepsClearsAndCopies(t *testing.T) {
	f := newFixture(t)
	c := pg("app")
	c.Password = "one"
	saved := f.save(c)

	saved.Port = 5433 // edit without retyping the password
	saved = f.save(saved)
	if pw, _ := f.password(saved.ID); pw != "one" || !saved.HasPassword {
		t.Errorf("edit lost the password: %q %+v", pw, saved)
	}

	dup := saved
	dup.ID, dup.Name, dup.CopyFrom = "", "app copy", saved.ID
	dup = f.save(dup)
	if pw, _ := f.password(dup.ID); pw != "one" || dup.ID == saved.ID {
		t.Errorf("duplicate got %q", pw)
	}

	saved.ClearPassword = true
	saved = f.save(saved)
	if saved.HasPassword {
		t.Error("HasPassword still set after clearing")
	}
	if _, err := f.k.Get(saved.ID); !errors.Is(err, secret.ErrNotFound) {
		t.Errorf("password store still has the cleared password: %v", err)
	}
	f.restart()
	if pw, err := f.password(saved.ID); pw != "" || err != nil {
		t.Errorf("cleared connection resolves to %q, %v", pw, err)
	}

	if err := f.s.Delete(dup.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := f.k.Get(dup.ID); !errors.Is(err, secret.ErrNotFound) {
		t.Error("deleting a connection kept its password")
	}
}

func TestPasswordInURLIsSplitOff(t *testing.T) {
	f := newFixture(t)
	c := pg("url")
	c.URL = "postgres://me:s3cret@db:5432/app"
	saved := f.save(c)
	if saved.URL != "postgres://me@db:5432/app" || !saved.HasPassword {
		t.Errorf("saved = %+v", saved)
	}
	f.notOnDisk("s3cret")
	if pw, _ := f.password(saved.ID); pw != "s3cret" {
		t.Errorf("password = %q", pw)
	}
}

func TestWithoutPasswordStoreAsksOnConnect(t *testing.T) {
	f := newFixture(t)
	f.k.Unavailable = true
	if f.s.PasswordStore().Available {
		t.Error("PasswordStore reports an unavailable store as available")
	}
	c := pg("app")
	c.Password = "hunter2"
	saved := f.save(c)
	if saved.HasPassword || !saved.AskPassword {
		t.Errorf("saved = %+v", saved)
	}
	f.notOnDisk("hunter2")
	// Still usable this session.
	if pw, err := f.password(saved.ID); pw != "hunter2" || err != nil {
		t.Errorf("same session: %q, %v", pw, err)
	}

	f.restart()
	if _, err := f.password(saved.ID); !errors.Is(err, errPasswordRequired) {
		t.Errorf("after restart: err = %v, want errPasswordRequired", err)
	}
	if err := f.s.ProvidePassword(saved.ID, "typed", false); err != nil {
		t.Fatal(err)
	}
	if pw, _ := f.password(saved.ID); pw != "typed" {
		t.Errorf("provided password = %q", pw)
	}
	f.notOnDisk("typed")

	// Once a password store shows up, "remember" saves it there.
	f.k.Unavailable = false
	if err := f.s.ProvidePassword(saved.ID, "typed", true); err != nil {
		t.Fatal(err)
	}
	f.restart()
	if pw, err := f.password(saved.ID); pw != "typed" || err != nil {
		t.Errorf("remembered password = %q, %v", pw, err)
	}
}

func TestMigratePlaintextPasswords(t *testing.T) {
	f := newFixture(t)
	legacy := `{"connections":[
	  {"id":"a","name":"a","driver":"postgres","host":"db","user":"me","password":"old-pw"},
	  {"id":"b","name":"b","driver":"mysql","url":"me:url-pw@tcp(db:3306)/app"}
	]}`

	// No password store: passwords stay put rather than being lost.
	if err := os.WriteFile(f.path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	f.k.Unavailable = true
	f.restart()
	if left := f.s.PasswordStore().PlaintextLeft; left != 2 {
		t.Errorf("PlaintextLeft = %d, want 2", left)
	}
	if pw, _ := f.password("a"); pw != "old-pw" {
		t.Errorf("unmigrated password = %q", pw)
	}
	for _, c := range f.s.List() {
		if c.Password != "" || strings.Contains(c.URL, "url-pw") {
			t.Errorf("List exposes a legacy password: %+v", c)
		}
	}

	f.k.Unavailable = false
	f.restart()
	f.notOnDisk("old-pw")
	f.notOnDisk("url-pw")
	if left := f.s.PasswordStore().PlaintextLeft; left != 0 {
		t.Errorf("PlaintextLeft = %d after migrating", left)
	}
	for id, want := range map[string]string{"a": "old-pw", "b": "url-pw"} {
		if pw, err := f.password(id); pw != want || err != nil {
			t.Errorf("%s: %q, %v", id, pw, err)
		}
	}
	if b, _ := f.s.store.Connection("b"); b.URL != "me@tcp(db:3306)/app" {
		t.Errorf("migrated URL = %q", b.URL)
	}
}

func TestEditingUnmigratedConnectionMovesPassword(t *testing.T) {
	f := newFixture(t)
	legacy := `{"connections":[{"id":"a","name":"a","driver":"postgres","host":"db","password":"old-pw"}]}`
	if err := os.WriteFile(f.path, []byte(legacy), 0o600); err != nil {
		t.Fatal(err)
	}
	f.k.Unavailable = true
	f.restart()
	c := f.s.List()[0]
	c.Port = 5433
	c = f.save(c)
	f.notOnDisk("old-pw")
	if !c.AskPassword {
		t.Errorf("saved = %+v", c)
	}
	if pw, _ := f.password("a"); pw != "old-pw" {
		t.Errorf("password = %q", pw)
	}
}
