package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

// Set at build time with -ldflags "-X main.version=1.2.3 -X main.updateRepo=owner/repo".
var (
	version    = "dev"
	updateRepo = "jevido/dbird"
)

const (
	updateEvent         = "dbird:update"
	firstCheckDelay     = 5 * time.Second
	updateCheckInterval = 6 * time.Hour
)

// UpdateStatus is what the frontend shows about self-updates.
type UpdateStatus struct {
	CurrentVersion string `json:"currentVersion"`
	// State is one of: disabled, idle, checking, up-to-date, downloading,
	// ready, manual, error.
	State string `json:"state"`
	// LatestVersion is set when a newer release was found.
	LatestVersion string `json:"latestVersion"`
	Notes         string `json:"notes"`
	ReleaseURL    string `json:"releaseUrl"`
	Error         string `json:"error"`
	CheckedAt     string `json:"checkedAt"`
}

// UpdateService checks GitHub Releases in the background, downloads and
// verifies new versions automatically and lets the user restart into them.
type UpdateService struct {
	mu      sync.Mutex
	status  UpdateStatus
	enabled bool
	running bool
}

func NewUpdateService() *UpdateService {
	return &UpdateService{status: UpdateStatus{CurrentVersion: version, State: "disabled"}}
}

func (s *UpdateService) ServiceStartup(ctx context.Context, _ application.ServiceOptions) error {
	if version == "dev" || os.Getenv("DBIRD_NO_UPDATE") != "" {
		return nil // development builds never update themselves
	}
	provider, err := github.New(github.Config{
		Repository:    updateRepo,
		ChecksumAsset: "checksums.txt",
		AssetMatcher:  matchAsset,
		// For testing the update flow against a local fake of the GitHub API.
		BaseURL: os.Getenv("DBIRD_UPDATE_API"),
	})
	if err != nil {
		return err
	}
	app := application.Get()
	if err := app.Updater.Init(updater.Config{
		CurrentVersion: strings.TrimPrefix(version, "v"),
		Providers:      []updater.Provider{provider},
		Window:         updater.WindowNone, // dbird renders its own prompt
	}); err != nil {
		return err
	}
	s.mu.Lock()
	s.enabled = true
	s.status.State = "idle"
	s.mu.Unlock()

	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(firstCheckDelay):
		}
		t := time.NewTicker(updateCheckInterval)
		defer t.Stop()
		for {
			s.run(ctx)
			select {
			case <-ctx.Done():
				return
			case <-t.C:
			}
		}
	}()
	return nil
}

// matchAsset picks the portable archive built by the release workflow:
// dbird-linux-amd64.tar.gz, dbird-darwin-universal.zip, dbird-windows-amd64.zip.
func matchAsset(req updater.CheckRequest, assets []github.ReleaseAsset) int {
	archs := []string{req.Arch}
	if req.Platform == "darwin" {
		archs = append(archs, "universal")
	}
	for _, arch := range archs {
		prefix := "dbird-" + req.Platform + "-" + arch + "."
		for i, a := range assets {
			name := strings.ToLower(a.Name)
			if strings.HasPrefix(name, prefix) && (strings.HasSuffix(name, ".zip") || strings.HasSuffix(name, ".tar.gz")) {
				return i
			}
		}
	}
	return -1
}

// canSelfUpdate reports whether the running binary (or .app bundle) sits in
// a directory we can write to. AppImages and system package installs can't
// be swapped in place; those users get a link to the release instead.
func canSelfUpdate() bool {
	if os.Getenv("APPIMAGE") != "" {
		return false
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	exe, _ = filepath.EvalSymlinks(exe)
	target := exe
	if runtime.GOOS == "darwin" {
		if i := strings.Index(exe, ".app/"); i >= 0 {
			target = exe[:i+4]
		}
	}
	f, err := os.CreateTemp(filepath.Dir(target), ".dbird-update-check-*")
	if err != nil {
		return false
	}
	name := f.Name()
	f.Close()
	os.Remove(name)
	return true
}

func (s *UpdateService) set(fn func(st *UpdateStatus)) {
	s.mu.Lock()
	fn(&s.status)
	st := s.status
	s.mu.Unlock()
	if app := application.Get(); app != nil {
		app.Event.Emit(updateEvent, st)
	}
}

// run performs one check and, when a newer release exists, downloads it.
func (s *UpdateService) run(ctx context.Context) {
	s.mu.Lock()
	if !s.enabled || s.running || s.status.State == "ready" {
		s.mu.Unlock()
		return
	}
	s.running = true
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		s.running = false
		s.mu.Unlock()
	}()

	u := application.Get().Updater
	s.set(func(st *UpdateStatus) { st.State = "checking"; st.Error = "" })
	rel, err := u.Check(ctx)
	now := time.Now().Format(time.RFC3339)
	if err != nil {
		s.set(func(st *UpdateStatus) { st.State = "error"; st.Error = err.Error(); st.CheckedAt = now })
		return
	}
	if rel == nil {
		s.set(func(st *UpdateStatus) { st.State = "up-to-date"; st.CheckedAt = now })
		return
	}
	releaseURL, _ := rel.Metadata["github.release.htmlURL"].(string)
	s.set(func(st *UpdateStatus) {
		st.LatestVersion = rel.Version
		st.Notes = rel.Notes
		st.ReleaseURL = releaseURL
		st.CheckedAt = now
		st.State = "downloading"
	})
	if !canSelfUpdate() {
		s.set(func(st *UpdateStatus) { st.State = "manual" })
		return
	}
	if err := u.DownloadAndInstall(ctx); err != nil {
		s.set(func(st *UpdateStatus) { st.State = "error"; st.Error = err.Error() })
		return
	}
	s.set(func(st *UpdateStatus) { st.State = "ready" })
}

// Status returns the current update state.
func (s *UpdateService) Status() UpdateStatus {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.status
}

// CheckNow runs a check immediately and returns the resulting state.
func (s *UpdateService) CheckNow(ctx context.Context) UpdateStatus {
	s.run(ctx)
	return s.Status()
}

// Restart quits and relaunches into the downloaded version.
func (s *UpdateService) Restart(ctx context.Context) error {
	if s.Status().State != "ready" {
		return errors.New("no update has been downloaded yet")
	}
	return application.Get().Updater.Restart(ctx)
}

// OpenReleasePage opens the release notes of the available version in the browser.
func (s *UpdateService) OpenReleasePage() error {
	url := s.Status().ReleaseURL
	if url == "" {
		url = "https://github.com/" + updateRepo + "/releases/latest"
	}
	return application.Get().Browser.OpenURL(url)
}
