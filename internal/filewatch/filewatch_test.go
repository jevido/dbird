package filewatch

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func watch(t *testing.T, paths ...string) (*Watcher, chan Change) {
	t.Helper()
	ch := make(chan Change, 10)
	w, err := New(func(c Change) { ch <- c })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { w.Close() })
	w.Set(paths)
	return w, ch
}

func expect(t *testing.T, ch chan Change, want string) {
	t.Helper()
	select {
	case c := <-ch:
		if c.Content != want {
			t.Fatalf("content = %q, want %q", c.Content, want)
		}
	case <-time.After(2 * time.Second):
		t.Fatalf("no change reported (want %q)", want)
	}
}

func expectNone(t *testing.T, ch chan Change) {
	t.Helper()
	select {
	case c := <-ch:
		t.Fatalf("unexpected change %q", c.Content)
	case <-time.After(400 * time.Millisecond):
	}
}

func TestInPlaceWrite(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.sql")
	os.WriteFile(p, []byte("select 1"), 0o644)
	_, ch := watch(t, p)
	os.WriteFile(p, []byte("select 2"), 0o644)
	expect(t, ch, "select 2")
}

// Editors like VS Code (with some settings) and vim save via rename.
func TestAtomicRenameSave(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.sql")
	os.WriteFile(p, []byte("select 1"), 0o644)
	_, ch := watch(t, p)
	tmp := filepath.Join(dir, ".a.sql.tmp")
	os.WriteFile(tmp, []byte("select 3"), 0o644)
	if err := os.Rename(tmp, p); err != nil {
		t.Fatal(err)
	}
	expect(t, ch, "select 3")
	// The watch survives the replaced inode.
	os.WriteFile(p, []byte("select 4"), 0o644)
	expect(t, ch, "select 4")
}

func TestIgnoresOwnSavesAndOtherFiles(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "a.sql")
	os.WriteFile(p, []byte("select 1"), 0o644)
	w, ch := watch(t, p)
	w.Saved(p, "select 9")
	os.WriteFile(p, []byte("select 9"), 0o644)
	os.WriteFile(filepath.Join(dir, "other.sql"), []byte("x"), 0o644)
	os.WriteFile(p, []byte("select 9"), 0o644) // touched but unchanged
	expectNone(t, ch)
}

func TestSetStopsWatching(t *testing.T) {
	p := filepath.Join(t.TempDir(), "a.sql")
	os.WriteFile(p, []byte("select 1"), 0o644)
	w, ch := watch(t, p)
	w.Set(nil)
	os.WriteFile(p, []byte("select 2"), 0o644)
	expectNone(t, ch)
}
