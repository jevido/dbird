package main

import (
	"os"
	"strings"

	"dbird/internal/changelog"
	"dbird/internal/store"
)

// ChangelogService tells the frontend what changed since the version the
// user ran last, for the "what's new" dialog after an update.
type ChangelogService struct {
	store *store.Store
}

// WhatsNew returns the releases since the version that last ran (newest
// first), or nothing on a fresh install, a dev build, or no version change.
func (s *ChangelogService) WhatsNew() []changelog.Entry {
	current := strings.TrimPrefix(version, "v")
	if version == "dev" || os.Getenv("DBIRD_NO_CHANGELOG") != "" {
		return nil
	}
	last := s.store.LastVersion()
	if last == "" {
		// First run of dbird at all: nothing to compare with.
		s.store.SetLastVersion(current)
		return nil
	}
	if last == current {
		return nil
	}
	entries := changelog.Between(changelog.Embedded(), last, current)
	if len(entries) == 0 {
		s.store.SetLastVersion(current)
	}
	return entries
}

// Seen records that the user has seen the notes for the running version.
func (s *ChangelogService) Seen() error {
	if version == "dev" {
		return nil
	}
	return s.store.SetLastVersion(strings.TrimPrefix(version, "v"))
}

// ReleaseURL is the GitHub page of the running version's release.
func (s *ChangelogService) ReleaseURL() string {
	return "https://github.com/" + updateRepo + "/releases/tag/v" + strings.TrimPrefix(version, "v")
}
