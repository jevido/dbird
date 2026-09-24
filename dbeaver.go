package main

import (
	"errors"
	"strings"

	"dbird/internal/dbeaver"
	"dbird/internal/dbx"
	"dbird/internal/store"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// DBeaverScan is what ScanDBeaver found.
type DBeaverScan struct {
	// Dir is the DBeaver workspace that was read ("" if none was found).
	Dir        string              `json:"dir"`
	Candidates []dbeaver.Candidate `json:"candidates"`
}

// ScanDBeaver reads the connections of the DBeaver workspace in dir, or of
// the first one found on this machine when dir is empty. Finding none is not
// an error: Dir is then empty.
func (s *ConnectionService) ScanDBeaver(dir string) (DBeaverScan, error) {
	if dir == "" {
		found := dbeaver.DefaultWorkspaces()
		if len(found) == 0 {
			return DBeaverScan{}, nil
		}
		dir = found[0]
	}
	cands, err := dbeaver.Scan(dir)
	if err != nil {
		return DBeaverScan{}, err
	}
	saved := s.store.Connections()
	for i := range cands {
		for _, c := range saved {
			if sameDatabase(cands[i].Connection, c) {
				cands[i].Existing = c.Name
				break
			}
		}
	}
	return DBeaverScan{Dir: dir, Candidates: cands}, nil
}

// sameDatabase reports whether a and b connect to the same database as the
// same user.
func sameDatabase(a, b store.Connection) bool {
	if a.Driver != b.Driver || a.Database != b.Database {
		return false
	}
	if a.Driver == dbx.SQLite {
		return true
	}
	return strings.EqualFold(a.Host, b.Host) && a.Port == b.Port && a.User == b.User
}

// PickDBeaverFolder shows a folder dialog and returns the chosen directory
// ("" if cancelled).
func (s *ConnectionService) PickDBeaverFolder() (string, error) {
	return application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "Choose DBeaver workspace",
		CanChooseDirectories: true,
		CanChooseFiles:       false,
		ShowHiddenFiles:      true,
	}).PromptForSingleSelection()
}

// Import saves conns as new connections. Nothing is saved unless all of them
// are valid.
func (s *ConnectionService) Import(conns []store.Connection) (int, error) {
	if len(conns) == 0 {
		return 0, errors.New("nothing selected to import")
	}
	for i := range conns {
		conns[i].ID = ""
		conns[i].Name = strings.TrimSpace(conns[i].Name)
		if err := validate(conns[i]); err != nil {
			return 0, errors.New(conns[i].Name + ": " + err.Error())
		}
	}
	for i, c := range conns {
		if _, err := s.save(c); err != nil {
			return i, err
		}
	}
	return len(conns), nil
}
