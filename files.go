package main

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const maxScriptSize = 20 << 20

// FileService reads and writes SQL scripts and exports.
type FileService struct{}

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
	return path, os.WriteFile(path, []byte(content), 0o644)
}

// SaveScript overwrites an existing SQL script previously opened or saved.
func (s *FileService) SaveScript(path, content string) error {
	if !filepath.IsAbs(path) || !strings.EqualFold(filepath.Ext(path), ".sql") {
		return errors.New("can only save .sql files with an absolute path; use Save As")
	}
	return os.WriteFile(path, []byte(content), 0o644)
}
