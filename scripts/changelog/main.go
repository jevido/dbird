// Command changelog builds dbird's release notes from git history: for each
// version tag, the subjects of the commits since the previous tag. Commits
// that only touch things users don't see (CI, docs, dev tooling, tests) are
// left out.
//
//	go run ./scripts/changelog > internal/changelog/changelog.json
//	go run ./scripts/changelog -notes v0.2.0          # markdown for one release
//	go run ./scripts/changelog -next v0.2.0           # include unreleased commits as v0.2.0
package main

import (
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"

	"dbird/internal/changelog"
)

// Paths that never matter to users. A commit touching only these is skipped.
var internalPaths = []string{".github/", "dev/", "scripts/", "docs/", "build/"}
var internalFiles = []string{"Taskfile.yml", "LICENSE", ".gitignore", "install.sh", "install.ps1"}

func internal(file string) bool {
	for _, p := range internalPaths {
		if strings.HasPrefix(file, p) {
			return true
		}
	}
	base := path.Base(file)
	for _, f := range internalFiles {
		if file == f {
			return true
		}
	}
	return strings.HasSuffix(base, ".md") || strings.HasSuffix(base, "_test.go") || strings.HasSuffix(base, ".test.ts")
}

//go:embed ignore.txt
var ignoreList string

func ignored(subject string) bool {
	for _, l := range lines(ignoreList) {
		if l = strings.TrimSpace(l); l != "" && !strings.HasPrefix(l, "#") && l == subject {
			return true
		}
	}
	return false
}

func git(args ...string) string {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		fmt.Fprintf(os.Stderr, "git %s: %v\n", strings.Join(args, " "), err)
		os.Exit(1)
	}
	return strings.TrimSpace(string(out))
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}

// items lists the user-facing commit subjects in rng (e.g. "v0.1.0..v0.2.0").
func items(rng string) []string {
	var out []string
	for _, c := range lines(git("log", "--no-merges", "--reverse", "--format=%H", rng)) {
		files := lines(git("diff-tree", "--no-commit-id", "--name-only", "-r", "--root", c))
		visible := false
		for _, f := range files {
			if !internal(f) {
				visible = true
				break
			}
		}
		if subject := git("log", "-1", "--format=%s", c); visible && !ignored(subject) {
			out = append(out, subject)
		}
	}
	return out
}

func main() {
	notes := flag.String("notes", "", "print markdown release notes for this tag instead of JSON")
	next := flag.String("next", "", "treat commits after the last tag as this (unreleased) version")
	flag.Parse()

	tags := lines(git("tag", "--list", "v*", "--sort=version:refname"))
	type rel struct{ tag, rng, date string }
	var rels []rel
	prev := ""
	for _, t := range tags {
		rng := t
		if prev != "" {
			rng = prev + ".." + t
		}
		rels = append(rels, rel{t, rng, git("log", "-1", "--format=%cs", t)})
		prev = t
	}
	if *next != "" {
		rng := "HEAD"
		if prev != "" {
			rng = prev + "..HEAD"
		}
		rels = append(rels, rel{*next, rng, git("log", "-1", "--format=%cs", "HEAD")})
	}

	var entries []changelog.Entry
	for i := len(rels) - 1; i >= 0; i-- {
		r := rels[i]
		entries = append(entries, changelog.Entry{Version: strings.TrimPrefix(r.tag, "v"), Date: r.date, Items: items(r.rng)})
	}

	if *notes != "" {
		for _, e := range entries {
			if e.Version == strings.TrimPrefix(*notes, "v") {
				for _, it := range e.Items {
					fmt.Printf("- %s\n", it)
				}
				if len(e.Items) == 0 {
					fmt.Println("- Maintenance release")
				}
				return
			}
		}
		fmt.Fprintf(os.Stderr, "no release %s\n", *notes)
		os.Exit(1)
	}
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if entries == nil {
		entries = []changelog.Entry{}
	}
	enc.Encode(entries)
}
