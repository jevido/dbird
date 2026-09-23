// Package filewatch reports when watched files change on disk, e.g. a .sql
// script saved from another editor.
//
// It watches the files' directories rather than the files themselves: most
// editors save by writing a temporary file and renaming it over the original,
// which replaces the inode a file watch would be attached to.
package filewatch

import (
	"crypto/sha256"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/fsnotify/fsnotify"
)

// Change is a new version of a watched file.
type Change struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// MaxSize is the largest file reported; bigger files are ignored.
const MaxSize = 20 << 20

// Settle is how long a file must stay quiet before it is read, since editors
// often produce several events per save.
var Settle = 150 * time.Millisecond

// Watcher reports content changes of a set of files.
type Watcher struct {
	onChange func(Change)
	fs       *fsnotify.Watcher

	mu     sync.Mutex
	files  map[string][32]byte // path -> hash of the content last seen
	dirs   map[string]bool
	timers map[string]*time.Timer
	done   chan struct{}
}

// New starts a watcher that calls onChange (from its own goroutine) whenever
// a watched file's content differs from what it last saw.
func New(onChange func(Change)) (*Watcher, error) {
	fs, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}
	w := &Watcher{
		onChange: onChange,
		fs:       fs,
		files:    map[string][32]byte{},
		dirs:     map[string]bool{},
		timers:   map[string]*time.Timer{},
		done:     make(chan struct{}),
	}
	go w.loop()
	return w, nil
}

// Close stops the watcher.
func (w *Watcher) Close() error {
	w.mu.Lock()
	for _, t := range w.timers {
		t.Stop()
	}
	w.mu.Unlock()
	err := w.fs.Close()
	<-w.done
	return err
}

// Set replaces the watched files. Files already watched keep their state;
// new ones are read now so later changes are compared against their current
// content.
func (w *Watcher) Set(paths []string) {
	want := map[string]bool{}
	for _, p := range paths {
		if abs, err := filepath.Abs(p); err == nil {
			want[abs] = true
		}
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	for p := range w.files {
		if !want[p] {
			delete(w.files, p)
		}
	}
	for p := range want {
		if _, ok := w.files[p]; !ok {
			if b, err := readSmall(p); err == nil {
				w.files[p] = sha256.Sum256(b)
			} else {
				w.files[p] = [32]byte{}
			}
		}
	}
	dirs := map[string]bool{}
	for p := range w.files {
		dirs[filepath.Dir(p)] = true
	}
	for d := range w.dirs {
		if !dirs[d] {
			w.fs.Remove(d)
			delete(w.dirs, d)
		}
	}
	for d := range dirs {
		if !w.dirs[d] && w.fs.Add(d) == nil {
			w.dirs[d] = true
		}
	}
}

// Saved tells the watcher that content was just written to path by us, so
// the resulting file events aren't reported back as an outside change.
func (w *Watcher) Saved(path, content string) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return
	}
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.files[abs]; ok {
		w.files[abs] = sha256.Sum256([]byte(content))
	}
}

func (w *Watcher) loop() {
	defer close(w.done)
	for {
		select {
		case ev, ok := <-w.fs.Events:
			if !ok {
				return
			}
			if ev.Op&(fsnotify.Write|fsnotify.Create|fsnotify.Rename) != 0 {
				w.schedule(filepath.Clean(ev.Name))
			}
		case _, ok := <-w.fs.Errors:
			if !ok {
				return
			}
		}
	}
}

func (w *Watcher) schedule(path string) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if _, ok := w.files[path]; !ok {
		return
	}
	if t := w.timers[path]; t != nil {
		t.Stop()
	}
	w.timers[path] = time.AfterFunc(Settle, func() { w.check(path) })
}

func (w *Watcher) check(path string) {
	b, err := readSmall(path)
	if err != nil {
		return // deleted, or mid-rename; a later event will follow
	}
	sum := sha256.Sum256(b)
	w.mu.Lock()
	delete(w.timers, path)
	prev, ok := w.files[path]
	if !ok || prev == sum {
		w.mu.Unlock()
		return
	}
	w.files[path] = sum
	w.mu.Unlock()
	w.onChange(Change{Path: path, Content: string(b)})
}

func readSmall(path string) ([]byte, error) {
	info, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	if info.Size() > MaxSize {
		return nil, os.ErrInvalid
	}
	return os.ReadFile(path)
}
