// Package changelog holds the release notes embedded in the binary, so the
// "what's new" dialog works offline and for package-manager installs.
//
// changelog.json is generated at release time by scripts/changelog from the
// git history between version tags; the committed copy is an empty list.
package changelog

import (
	_ "embed"
	"encoding/json"
	"strconv"
	"strings"
)

//go:embed changelog.json
var data []byte

// Entry is one release.
type Entry struct {
	Version string   `json:"version"` // without the leading "v"
	Date    string   `json:"date"`    // YYYY-MM-DD
	Items   []string `json:"items"`
}

// Parse decodes a changelog, newest release first.
func Parse(b []byte) ([]Entry, error) {
	var out []Entry
	err := json.Unmarshal(b, &out)
	return out, err
}

// Embedded returns the changelog compiled into this binary.
func Embedded() []Entry {
	e, _ := Parse(data)
	return e
}

// Between returns the releases newer than last and up to current, newest
// first, i.e. what someone upgrading from last to current hasn't seen.
func Between(entries []Entry, last, current string) []Entry {
	var out []Entry
	for _, e := range entries {
		if Compare(e.Version, last) > 0 && Compare(e.Version, current) <= 0 {
			out = append(out, e)
		}
	}
	return out
}

// Compare compares dotted versions ("1.2.3", optional "v" prefix and
// "-suffix"); a pre-release sorts before its release.
func Compare(a, b string) int {
	pa, sa := split(a)
	pb, sb := split(b)
	for i := 0; i < 3; i++ {
		if pa[i] != pb[i] {
			if pa[i] < pb[i] {
				return -1
			}
			return 1
		}
	}
	switch {
	case sa == sb:
		return 0
	case sa == "":
		return 1
	case sb == "":
		return -1
	case sa < sb:
		return -1
	default:
		return 1
	}
}

func split(v string) ([3]int, string) {
	v = strings.TrimPrefix(v, "v")
	v, suffix, _ := strings.Cut(v, "-")
	var n [3]int
	for i, p := range strings.SplitN(v, ".", 3) {
		n[i], _ = strconv.Atoi(p)
	}
	return n, suffix
}
