// Package store persists dbird's saved connections and open editor tabs as
// JSON files in the user's config directory.
package store

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
)

// Connection is a saved database connection.
type Connection struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Driver   string `json:"driver"` // postgres, mysql, sqlite
	Host     string `json:"host"`
	Port     int    `json:"port"`
	User     string `json:"user"`
	Password string `json:"password"`
	Database string `json:"database"` // database name, or file path for sqlite
	SSLMode  string `json:"sslMode"`  // postgres only
	// URL, when set, is used verbatim as the DSN and overrides the fields above.
	URL   string `json:"url"`
	Color string `json:"color"`
	// Completion selects how editor autocompletion gets table and column
	// names: "" (automatic), "preload", "lookup" or "off".
	Completion string `json:"completion"`
}

// Tab is an open SQL editor tab.
type Tab struct {
	ID           string `json:"id"`
	Title        string `json:"title"`
	ConnectionID string `json:"connectionId"`
	SQL          string `json:"sql"`
	// FilePath is set when the tab is backed by a .sql file on disk.
	FilePath string `json:"filePath"`
	// Dirty is set when a file-backed tab has edits not saved to FilePath.
	Dirty bool `json:"dirty"`
}

// Workspace is the persisted UI state.
type Workspace struct {
	Tabs        []Tab  `json:"tabs"`
	ActiveTabID string `json:"activeTabId"`
}

type data struct {
	Connections []Connection `json:"connections"`
	Workspace   Workspace    `json:"workspace"`
}

// Store is a JSON-file-backed store. It is safe for concurrent use.
type Store struct {
	path string
	mu   sync.Mutex
	d    data
}

// DefaultPath returns the config file location, e.g. ~/.config/dbird/dbird.json.
func DefaultPath() (string, error) {
	dir, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "dbird", "dbird.json"), nil
}

// Open loads the store at path, creating an empty one if the file is missing.
func Open(path string) (*Store, error) {
	s := &Store{path: path}
	b, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(b, &s.d); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return s, nil
}

// NewID returns a random 16-character hex identifier.
func NewID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (s *Store) save() error {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.d, "", "  ")
	if err != nil {
		return err
	}
	// Passwords live in this file, so keep it private to the user.
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		os.Remove(tmp)
		return err
	}
	if err := os.Rename(tmp, s.path); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Connections returns a copy of all saved connections.
func (s *Store) Connections() []Connection {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Connection, len(s.d.Connections))
	copy(out, s.d.Connections)
	return out
}

// Connection returns the saved connection with the given id.
func (s *Store) Connection(id string) (Connection, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, c := range s.d.Connections {
		if c.ID == id {
			return c, true
		}
	}
	return Connection{}, false
}

// SaveConnection inserts or updates c. A connection without an ID gets a new one.
func (s *Store) SaveConnection(c Connection) (Connection, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if c.ID == "" {
		c.ID = NewID()
		s.d.Connections = append(s.d.Connections, c)
	} else {
		found := false
		for i := range s.d.Connections {
			if s.d.Connections[i].ID == c.ID {
				s.d.Connections[i] = c
				found = true
				break
			}
		}
		if !found {
			s.d.Connections = append(s.d.Connections, c)
		}
	}
	return c, s.save()
}

// DeleteConnection removes the connection with the given id.
func (s *Store) DeleteConnection(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.d.Connections[:0]
	for _, c := range s.d.Connections {
		if c.ID != id {
			out = append(out, c)
		}
	}
	s.d.Connections = out
	return s.save()
}

// Workspace returns the saved workspace.
func (s *Store) Workspace() Workspace {
	s.mu.Lock()
	defer s.mu.Unlock()
	w := s.d.Workspace
	w.Tabs = append([]Tab(nil), w.Tabs...)
	return w
}

// SaveWorkspace replaces the saved workspace.
func (s *Store) SaveWorkspace(w Workspace) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.d.Workspace = w
	return s.save()
}
