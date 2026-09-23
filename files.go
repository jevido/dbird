package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"dbird/internal/filewatch"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const maxScriptSize = 20 << 20

// fileChangedEvent carries a filewatch.Change when a watched script is
// modified outside dbird.
const fileChangedEvent = "file:changed"

// FileService reads and writes SQL scripts and exports, and watches open
// scripts so edits made in another editor show up in dbird.
type FileService struct {
	watcher *filewatch.Watcher
}

func (s *FileService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	w, err := filewatch.New(func(c filewatch.Change) {
		application.Get().Event.Emit(fileChangedEvent, c)
	})
	if err != nil {
		return err
	}
	s.watcher = w
	return nil
}

func (s *FileService) ServiceShutdown() error {
	if s.watcher != nil {
		return s.watcher.Close()
	}
	return nil
}

// Watch sets the script files to watch for outside changes (the files open
// in tabs). Changes arrive as "file:changed" events.
func (s *FileService) Watch(paths []string) {
	if s.watcher != nil {
		s.watcher.Set(paths)
	}
}

// ReadScript returns the current content of an open script, e.g. to pick up
// changes made while dbird was closed.
func (s *FileService) ReadScript(path string) (string, error) {
	if !filepath.IsAbs(path) {
		return "", errors.New("path must be absolute")
	}
	info, err := os.Stat(path)
	if err != nil {
		return "", err
	}
	if info.Size() > maxScriptSize {
		return "", fmt.Errorf("%s is too large (%d MB)", filepath.Base(path), info.Size()>>20)
	}
	b, err := os.ReadFile(path)
	return string(b), err
}

// OpenExternally opens a script in the system's default application for it.
func (s *FileService) OpenExternally(path string) error {
	if !filepath.IsAbs(path) {
		return errors.New("path must be absolute")
	}
	if _, err := os.Stat(path); err != nil {
		return err
	}
	return application.Get().Browser.OpenFile(path)
}

func (s *FileService) saved(path, content string) {
	if s.watcher != nil {
		s.watcher.Saved(path, content)
	}
}

// OpenedFile is a file loaded through the open dialog.
type OpenedFile struct {
	Path    string `json:"path"`
	Name    string `json:"name"`
	Content string `json:"content"`
}

var fileFilters = map[string][]application.FileFilter{
	"sql": {{DisplayName: "SQL scripts", Pattern: "*.sql"}, {DisplayName: "All files", Pattern: "*"}},
	"csv": {{DisplayName: "CSV files", Pattern: "*.csv"}, {DisplayName: "All files", Pattern: "*"}},
	"tsv": {{DisplayName: "TSV files", Pattern: "*.tsv"}, {DisplayName: "All files", Pattern: "*"}},
}

// OpenScript asks for a .sql file and returns its contents. A zero value
// means the dialog was cancelled.
func (s *FileService) OpenScript() (OpenedFile, error) {
	path, err := application.Get().Dialog.OpenFileWithOptions(&application.OpenFileDialogOptions{
		Title:                "Open SQL script",
		CanChooseFiles:       true,
		AllowsOtherFileTypes: true,
		Filters:              fileFilters["sql"],
	}).PromptForSingleSelection()
	if err != nil || path == "" {
		return OpenedFile{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		return OpenedFile{}, err
	}
	if info.Size() > maxScriptSize {
		return OpenedFile{}, fmt.Errorf("%s is too large (%d MB)", filepath.Base(path), info.Size()>>20)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return OpenedFile{}, err
	}
	return OpenedFile{Path: path, Name: filepath.Base(path), Content: string(b)}, nil
}

// SaveAs asks where to save content and writes it. kind selects the file
// filter ("sql", "csv" or "tsv"). Returns "" if the dialog was cancelled.
func (s *FileService) SaveAs(suggestedName, content, kind string) (string, error) {
	filters, ok := fileFilters[kind]
	if !ok {
		return "", fmt.Errorf("unknown file kind %q", kind)
	}
	path, err := application.Get().Dialog.SaveFileWithOptions(&application.SaveFileDialogOptions{
		Title:                "Save " + strings.ToUpper(kind),
		Filename:             filepath.Base(suggestedName),
		CanCreateDirectories: true,
		AllowOtherFileTypes:  true,
		Filters:              filters,
	}).PromptForSingleSelection()
	if err != nil || path == "" {
		return "", err
	}
	if filepath.Ext(path) == "" {
		path += "." + kind
	}
	s.saved(path, content)
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// SaveScript overwrites an existing SQL script previously opened or saved.
func (s *FileService) SaveScript(path, content string) error {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".sql") {
		return errors.New("can only save .sql files with an absolute path; use Save As")
	}
	s.saved(path, content)
	return os.WriteFile(path, []byte(content), 0o644)
}
