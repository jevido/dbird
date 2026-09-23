#!/bin/sh
# dbird installer for Linux and macOS.
#
#   curl -fsSL https://raw.githubusercontent.com/jevido/dbird/main/install.sh | sh
#
# Installs the latest release into your home directory (no sudo), where dbird
# can update itself, and adds it to your app launcher.
#   Linux: ~/.local/share/dbird/dbird, linked as ~/.local/bin/dbird
#   macOS: ~/Applications/dbird.app
#
# Uninstall: curl -fsSL https://raw.githubusercontent.com/jevido/dbird/main/install.sh | sh -s -- --uninstall
set -eu

REPO=${DBIRD_REPO:-jevido/dbird}
BASE=${DBIRD_DOWNLOAD_BASE:-https://github.com/$REPO/releases/latest/download}
RAW=${DBIRD_RAW:-https://raw.githubusercontent.com/$REPO/main}

say() { printf '\033[1m%s\033[0m\n' "$*"; }
fail() { printf 'error: %s\n' "$*" >&2; exit 1; }
need() { command -v "$1" >/dev/null 2>&1 || fail "$1 is required"; }

fetch() { # url dest
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2"
  elif command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1"
  else fail "curl or wget is required"; fi
}

data=${XDG_DATA_HOME:-$HOME/.local/share}
bin=$HOME/.local/bin

uninstall() {
  case $(uname -s) in
    Darwin) rm -rf "$HOME/Applications/dbird.app" ;;
    *)
      rm -rf "$data/dbird" "$bin/dbird" "$data/applications/dbird.desktop" "$data/icons/hicolor/512x512/apps/dbird.png"
      command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$data/applications" >/dev/null 2>&1 || true
      ;;
  esac
  say "dbird removed. Your connections and tabs are kept in ~/.config/dbird (delete it to remove them too)."
}

[ "${1:-}" = "--uninstall" ] && { uninstall; exit 0; }

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT

case $(uname -s) in
  Linux)
    case $(uname -m) in
      x86_64 | amd64) arch=amd64 ;;
      aarch64 | arm64) arch=arm64 ;;
      *) fail "unsupported architecture $(uname -m)" ;;
    esac
    need tar
    say "Downloading dbird for Linux ($arch)…"
    fetch "$BASE/dbird-linux-$arch.tar.gz" "$tmp/dbird.tar.gz"
    tar -xzf "$tmp/dbird.tar.gz" -C "$tmp"
    mkdir -p "$data/dbird" "$bin" "$data/applications" "$data/icons/hicolor/512x512/apps"
    install -m 755 "$tmp/dbird" "$data/dbird/dbird"
    ln -sf "$data/dbird/dbird" "$bin/dbird"
    fetch "$RAW/build/appicon.png" "$data/icons/hicolor/512x512/apps/dbird.png" || true
    fetch "$RAW/packaging/linux/dbird.desktop" "$tmp/dbird.desktop"
    sed "s|^Exec=.*|Exec=$data/dbird/dbird|" "$tmp/dbird.desktop" > "$data/applications/dbird.desktop"
    command -v update-desktop-database >/dev/null 2>&1 && update-desktop-database "$data/applications" >/dev/null 2>&1 || true

    # dbird needs GTK 4 and WebKitGTK 6.0.
    if ! (ldconfig -p 2>/dev/null | grep -q 'libwebkitgtk-6.0.so'); then
      say "dbird needs WebKitGTK 6.0, which doesn't seem to be installed:"
      if command -v pacman >/dev/null 2>&1; then echo "  sudo pacman -S --needed gtk4 webkitgtk-6.0"
      elif command -v apt-get >/dev/null 2>&1; then echo "  sudo apt install libgtk-4-1 libwebkitgtk-6.0-4"
      elif command -v dnf >/dev/null 2>&1; then echo "  sudo dnf install gtk4 webkitgtk6.0"
      else echo "  install gtk4 and webkitgtk-6.0 with your package manager"; fi
    fi

    say "dbird is installed. Start it from your app launcher, or run: dbird"
    case ":$PATH:" in *":$bin:"*) ;; *) echo "(add $bin to your PATH to run it from a terminal)" ;; esac
    ;;
  Darwin)
    need unzip
    say "Downloading dbird for macOS…"
    fetch "$BASE/dbird-darwin-universal.zip" "$tmp/dbird.zip"
    unzip -q "$tmp/dbird.zip" -d "$tmp"
    mkdir -p "$HOME/Applications"
    rm -rf "$HOME/Applications/dbird.app"
    mv "$tmp/dbird.app" "$HOME/Applications/dbird.app"
    xattr -dr com.apple.quarantine "$HOME/Applications/dbird.app" 2>/dev/null || true
    say "dbird is installed in ~/Applications. Open it from Launchpad or Spotlight."
    ;;
  *)
    fail "this installer supports Linux and macOS; on Windows use install.ps1 (see the README)"
    ;;
esac
