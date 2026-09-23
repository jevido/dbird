// Package omarchy reads the current Omarchy desktop theme
// (https://omarchy.org), so dbird can match it.
//
// Omarchy keeps the active theme in ~/.local/state/omarchy/current: the name
// in theme.name and the palette in theme/colors.toml (flat `key = "value"`
// lines such as background, foreground, accent, selection and the 16
// terminal colors). `omarchy theme set` rewrites both.
package omarchy

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

// Theme is an Omarchy palette.
type Theme struct {
	Name   string            `json:"name"`
	Mode   string            `json:"mode"` // "dark" or "light"
	Colors map[string]string `json:"colors"`
}

// Dir returns Omarchy's current-theme directory, whether or not it exists.
func Dir() string {
	state := os.Getenv("XDG_STATE_HOME")
	if state == "" {
		home, _ := os.UserHomeDir()
		state = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(state, "omarchy", "current")
}

// Files returns the files that change when the theme changes.
func Files(dir string) []string {
	return []string{filepath.Join(dir, "theme.name"), filepath.Join(dir, "theme", "colors.toml")}
}

// Read loads the current theme from dir. ok is false when Omarchy isn't
// installed or the theme has no usable palette.
func Read(dir string) (t Theme, ok bool) {
	f, err := os.Open(filepath.Join(dir, "theme", "colors.toml"))
	if err != nil {
		return Theme{}, false
	}
	defer f.Close()
	t.Colors = map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		key, val, found := strings.Cut(sc.Text(), "=")
		if !found {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		if q := val[:min(1, len(val))]; q == `"` || q == "'" {
			// Quoted: take what's inside the quotes, ignore trailing comments.
			if end := strings.Index(val[1:], q); end >= 0 {
				val = val[1 : end+1]
			}
		} else if i := strings.Index(val, "#"); i > 0 {
			val = strings.TrimSpace(val[:i])
		}
		if key == "mode" {
			t.Mode = val
		} else if isHexColor(val) {
			t.Colors[key] = strings.ToLower(val)
		}
	}
	if t.Colors["background"] == "" || t.Colors["foreground"] == "" {
		return Theme{}, false
	}
	if t.Mode != "light" {
		t.Mode = "dark"
	}
	if b, err := os.ReadFile(filepath.Join(dir, "theme.name")); err == nil {
		t.Name = strings.TrimSpace(string(b))
	}
	return t, true
}

func isHexColor(s string) bool {
	if len(s) != 7 || s[0] != '#' {
		return false
	}
	for _, c := range s[1:] {
		if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
			return false
		}
	}
	return true
}
