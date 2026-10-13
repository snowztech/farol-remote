#!/bin/sh
# Installs farol, the command for a machine that stays on: curl -fsSL <this file> | sh
# It downloads the latest release for this machine into ~/.local/bin, or FAROL_BIN when set.
set -eu

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "farol: no build for $(uname -m)" >&2; exit 1 ;;
esac
case $os in
    linux | darwin) ;;
    *) echo "farol: no build for $os" >&2; exit 1 ;;
esac

bin=${FAROL_BIN:-$HOME/.local/bin}
mkdir -p "$bin"
curl -fsSL "https://github.com/snowztech/farol-remote/releases/latest/download/farol-$os-$arch" -o "$bin/farol.new"
chmod +x "$bin/farol.new"
mv "$bin/farol.new" "$bin/farol"

echo "Installed $bin/farol"
command -v tmux >/dev/null || echo "tmux is not installed, and farol host needs it to run the sessions."
case ":$PATH:" in
    *":$bin:"*) ;;
    *) echo "Add $bin to your PATH, then run: farol host" ;;
esac
