package main

import (
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
