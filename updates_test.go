package main

import (
	"os"
	"testing"

	"github.com/wailsapp/wails/v3/pkg/updater"
	"github.com/wailsapp/wails/v3/pkg/updater/providers/github"
)

func TestMatchAsset(t *testing.T) {
	assets := []github.ReleaseAsset{
		{Name: "checksums.txt"},
		{Name: "dbird-0.2.0-x86_64.AppImage"},
		{Name: "dbird_0.2.0_amd64.deb"},
		{Name: "dbird-linux-amd64.tar.gz"},
		{Name: "dbird-linux-arm64.tar.gz"},
		{Name: "dbird-darwin-universal.zip"},
		{Name: "dbird-windows-amd64.zip"},
	}
	cases := []struct {
		platform, arch, want string
	}{
		{"linux", "amd64", "dbird-linux-amd64.tar.gz"},
		{"linux", "arm64", "dbird-linux-arm64.tar.gz"},
		{"darwin", "arm64", "dbird-darwin-universal.zip"},
		{"darwin", "amd64", "dbird-darwin-universal.zip"},
		{"windows", "amd64", "dbird-windows-amd64.zip"},
		{"windows", "arm64", ""},
	}
	for _, c := range cases {
		i := matchAsset(updater.CheckRequest{Platform: c.platform, Arch: c.arch}, assets)
		got := ""
		if i >= 0 {
			got = assets[i].Name
		}
		if got != c.want {
			t.Errorf("%s/%s: got %q, want %q", c.platform, c.arch, got, c.want)
		}
	}
}

func TestGuardUpdaterHelper(t *testing.T) {
	t.Setenv("TMPDIR", "/staging")
	t.Setenv(origTmpDirEnv, "/orig")

	// The first helper keeps its environment and gets the marker.
	t.Setenv(helperEnv, "1")
	t.Setenv(helperEnv+"_TARGET", "/app/dbird")
	t.Setenv(helperRanEnv, "")
	os.Unsetenv(helperRanEnv)
	guardUpdaterHelper()
	if os.Getenv(helperEnv) != "1" || os.Getenv(helperRanEnv) != "1" {
		t.Fatalf("first helper: %s=%q %s=%q", helperEnv, os.Getenv(helperEnv), helperRanEnv, os.Getenv(helperRanEnv))
	}

	// A binary relaunched by that helper (same environment) starts as the app.
	guardUpdaterHelper()
	for _, k := range []string{helperEnv, helperEnv + "_TARGET", helperRanEnv, origTmpDirEnv} {
		if v, set := os.LookupEnv(k); set {
			t.Errorf("relaunch kept %s=%q", k, v)
		}
	}
	if got := os.Getenv("TMPDIR"); got != "/orig" {
		t.Errorf("TMPDIR = %q, want the original /orig", got)
	}
}
