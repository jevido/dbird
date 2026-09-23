package omarchy

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRead(t *testing.T) {
	dir := t.TempDir()
	if _, ok := Read(dir); ok {
		t.Fatal("empty dir should not be a theme")
	}
	os.MkdirAll(filepath.Join(dir, "theme"), 0o755)
	os.WriteFile(filepath.Join(dir, "theme.name"), []byte("tokyo-night\n"), 0o644)
	os.WriteFile(filepath.Join(dir, "theme", "colors.toml"), []byte(`mode = "light"
accent = "#7AA2F7"
background = "#1a1b26"   # comment
foreground = "#c0caf5"
hyprland_active_border = "rgba(798186ee) rgba(caccccee)"
`), 0o644)
	th, ok := Read(dir)
	if !ok || th.Name != "tokyo-night" || th.Mode != "light" || th.Colors["accent"] != "#7aa2f7" || th.Colors["background"] != "#1a1b26" {
		t.Fatalf("got %+v %v", th, ok)
	}
	if _, has := th.Colors["hyprland_active_border"]; has {
		t.Fatal("non-hex values must be skipped")
	}
}

// Every stock theme on this machine parses (skipped where Omarchy isn't installed).
func TestStockThemes(t *testing.T) {
	home, _ := os.UserHomeDir()
	themes, _ := filepath.Glob(filepath.Join(home, ".local/share/omarchy/themes/*"))
	if len(themes) == 0 {
		t.Skip("no Omarchy themes installed")
	}
	for _, dir := range themes {
		tmp := t.TempDir()
		os.Symlink(dir, filepath.Join(tmp, "theme"))
		th, ok := Read(tmp)
		if !ok || len(th.Colors) < 16 {
			t.Errorf("%s: ok=%v colors=%d", filepath.Base(dir), ok, len(th.Colors))
		}
	}
}
