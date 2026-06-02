#!/bin/sh
# trawl installer — downloads the latest release binary for your platform.
#
#   curl -fsSL https://raw.githubusercontent.com/liam-od/trawl/main/install.sh | sh
#
# Honours these environment variables:
#   BIN_DIR   install directory (default: /usr/local/bin, falls back to sudo)
set -eu

REPO="liam-od/trawl"
BINARY="trawl"

os=$(uname -s)
arch=$(uname -m)

if [ "$os" != "Linux" ]; then
	echo "trawl: this installer only supports Linux." >&2
	echo "Windows users: download trawl-windows-amd64.exe from" >&2
	echo "  https://github.com/$REPO/releases/latest" >&2
	exit 1
fi

case "$arch" in
	x86_64 | amd64) asset="$BINARY-linux-amd64" ;;
	*)
		echo "trawl: no prebuilt binary for architecture '$arch' (linux amd64 only)." >&2
		echo "Build from source instead: https://github.com/$REPO#from-source" >&2
		exit 1
		;;
esac

url="https://github.com/$REPO/releases/latest/download/$asset"

dir="${BIN_DIR:-/usr/local/bin}"
sudo=""
if [ ! -d "$dir" ] || [ ! -w "$dir" ]; then
	if [ -z "${BIN_DIR:-}" ] && command -v sudo >/dev/null 2>&1; then
		sudo="sudo"
	else
		echo "trawl: $dir is not writable. Re-run with BIN_DIR set to a writable directory," >&2
		echo "e.g.  curl -fsSL .../install.sh | BIN_DIR=\"\$HOME/.local/bin\" sh" >&2
		exit 1
	fi
fi

tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT

echo "Downloading $asset…"
curl -fsSL "$url" -o "$tmp"
chmod +x "$tmp"

echo "Installing to $dir/$BINARY"
$sudo mv "$tmp" "$dir/$BINARY"
trap - EXIT

echo "Done. Run: $BINARY --help"
