#!/bin/sh
# Install multiversectl (with the mvctl alias and the web view, multiversectl-server).
#   curl -fsSL https://multiversectl.dev/install.sh | sh
# Env: MULTIVERSECTL_VERSION (e.g. v0.0.1), MULTIVERSECTL_BIN_DIR, MULTIVERSECTL_SKIP_CHECKSUM=1
set -eu

REPO="cristiansteib/multiversectl"
BIN="multiversectl"

say() { printf '%s\n' "$*"; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

case "$(uname -s)" in
  Linux) os=linux ;;
  Darwin) os=darwin ;;
  *) die "unsupported OS '$(uname -s)' (supported: linux, darwin)" ;;
esac
case "$(uname -m)" in
  x86_64|amd64) arch=amd64 ;;
  aarch64|arm64) arch=arm64 ;;
  *) die "unsupported architecture '$(uname -m)' (supported: amd64, arm64)" ;;
esac

if have curl; then
  fetch() { curl -fsSL "$1" -o "$2"; }
  fetch_stdout() { curl -fsSL "$1"; }
elif have wget; then
  fetch() { wget -qO "$2" "$1"; }
  fetch_stdout() { wget -qO- "$1"; }
else
  die "curl or wget is required"
fi

asset="${BIN}_${os}_${arch}"

# The newest release is created before its binaries are attached, so for a
# minute or so "latest" names a tag with nothing to download. Take the newest
# release that already carries this platform's binary and the checksums.
latest_complete() {
  urls=$(fetch_stdout "https://api.github.com/repos/$REPO/releases?per_page=10" |
    sed -n 's/.*"browser_download_url"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p') || return 0
  for tag in $(printf '%s\n' "$urls" | sed -n "s|.*/download/\([^/]*\)/$asset\$|\1|p"); do
    if printf '%s\n' "$urls" | grep -q "/download/$tag/checksums.txt\$"; then
      printf '%s\n' "$tag"
      return 0
    fi
  done
}

version="${MULTIVERSECTL_VERSION:-}"
if [ -z "$version" ]; then
  version=$(latest_complete)
  [ -n "$version" ] || die "could not find a release with $asset (GitHub API rate limit, or none published yet); set MULTIVERSECTL_VERSION"
fi

tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT INT TERM

SERVER="multiversectl-server"
base="https://github.com/$REPO/releases/download/$version"

if have sha256sum; then
  sum() { sha256sum "$1" | cut -d ' ' -f 1; }
elif have shasum; then
  sum() { shasum -a 256 "$1" | cut -d ' ' -f 1; }
else
  sum=""
fi
verify=1
if ! have sha256sum && ! have shasum; then
  if [ "${MULTIVERSECTL_SKIP_CHECKSUM:-}" = 1 ]; then
    say "warning: no sha256sum or shasum found; skipping checksum verification"
    verify=0
  else
    die "no sha256sum or shasum found; install one, or set MULTIVERSECTL_SKIP_CHECKSUM=1 to continue unverified"
  fi
fi
if [ "$verify" = 1 ]; then
  fetch "$base/checksums.txt" "$tmp/checksums.txt" || die "could not download checksums.txt"
fi

# get downloads one release asset into $tmp, checks it, and makes it executable.
get() {
  fetch "$base/$1" "$tmp/$1" || return 1
  if [ "$verify" = 1 ]; then
    want=$(awk -v f="$1" '$2 == f || $2 == "*" f { print $1 }' "$tmp/checksums.txt" | head -n 1)
    [ -n "$want" ] || die "no checksum listed for $1"
    [ "$want" = "$(sum "$tmp/$1")" ] || die "checksum mismatch for $1"
  fi
  chmod +x "$tmp/$1"
}

say "Downloading $BIN $version ($os/$arch)"
get "$asset" || die "download failed: $base/$asset"
# The web view (multiversectl server) is a separate program that has to sit
# beside the command line. A release that predates it is not a failure.
server_asset="${SERVER}_${os}_${arch}"
have_server=1
get "$server_asset" || { have_server=0; say "warning: $version has no $server_asset; 'multiversectl server' will not work"; }

dir="${MULTIVERSECTL_BIN_DIR:-}"
sudo_cmd=""
if [ -z "$dir" ]; then
  if [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    dir="$HOME/.local/bin"
  else
    dir=/usr/local/bin
  fi
fi
if ! { mkdir -p "$dir" 2>/dev/null && [ -w "$dir" ]; }; then
  if [ "$(id -u)" -ne 0 ] && have sudo; then
    sudo_cmd=sudo
  else
    die "cannot write to $dir; set MULTIVERSECTL_BIN_DIR to a writable directory"
  fi
fi

$sudo_cmd mkdir -p "$dir"
$sudo_cmd mv "$tmp/$asset" "$dir/$BIN"
[ "$have_server" = 1 ] && $sudo_cmd mv "$tmp/$server_asset" "$dir/$SERVER"
$sudo_cmd ln -sfn "$BIN" "$dir/mvctl"

say "Installed $dir/$BIN (alias: mvctl)"
[ "$have_server" = 1 ] && say "Installed $dir/$SERVER (the web view: multiversectl server start)"
"$dir/$BIN" version 2>/dev/null || "$dir/$BIN" --version 2>/dev/null || true

# A web view that was already running still holds the old build; this makes it
# take the new one, and says nothing when there is none.
"$dir/$BIN" server restart --if-running 2>/dev/null || true

case ":$PATH:" in
  *":$dir:"*) ;;
  *) say "warning: $dir is not on your PATH; add it to your shell profile." ;;
esac
