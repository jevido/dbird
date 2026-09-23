package main

import (
	"context"
	"os"

	"dbird/internal/filewatch"
	"dbird/internal/omarchy"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// omarchyThemeEvent carries an OmarchyTheme whenever the desktop theme changes.
const omarchyThemeEvent = "theme:omarchy"

// OmarchyTheme is the desktop theme dbird follows on Omarchy systems.
type OmarchyTheme struct {
	Available bool          `json:"available"`
	Theme     omarchy.Theme `json:"theme"`
}

// ThemeService lets dbird follow the Omarchy desktop theme, live.
type ThemeService struct {
	dir     string
	watcher *filewatch.Watcher
}

func (s *ThemeService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if os.Getenv("DBIRD_NO_OMARCHY_THEME") != "" {
		return nil
	}
	s.dir = omarchy.Dir()
	if _, err := os.Stat(s.dir); err != nil {
		return nil // not an Omarchy system
	}
	w, err := filewatch.New(func(filewatch.Change) {
		application.Get().Event.Emit(omarchyThemeEvent, s.Omarchy())
	})
	if err != nil {
		return nil // theming is a nicety; never block startup
	}
	s.watcher = w
	w.Set(omarchy.Files(s.dir))
	return nil
}

func (s *ThemeService) ServiceShutdown() error {
	if s.watcher != nil {
		return s.watcher.Close()
	}
	return nil
}

// Omarchy returns the current Omarchy theme, if this is an Omarchy system.
func (s *ThemeService) Omarchy() OmarchyTheme {
	if s.dir == "" {
		return OmarchyTheme{}
	}
	t, ok := omarchy.Read(s.dir)
	return OmarchyTheme{Available: ok, Theme: t}
}
