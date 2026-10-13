#!/bin/sh
# Installs the farol-remote CLI, which lets a paired phone reach the agents on a machine that stays on.
#   curl -fsSL https://farol.sh/remote.sh | sh
# It downloads the latest release for this machine into ~/.local/bin, or FAROL_BIN when set.
set -eu

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "farol-remote: no build for $(uname -m)" >&2; exit 1 ;;
esac
case $os in
    linux | darwin) ;;
    *) echo "farol-remote: no build for $os" >&2; exit 1 ;;
esac

bin=${FAROL_BIN:-$HOME/.local/bin}
mkdir -p "$bin"
release=https://github.com/snowztech/farol-remote/releases/latest/download
curl -fsSL "$release/farol-remote-$os-$arch" -o "$bin/farol-remote.new"

# A download that was cut short or changed on the way is not installed.
want=$(curl -fsSL "$release/checksums.txt" | awk -v file="farol-remote-$os-$arch" '$2 == file { print $1 }')
if command -v sha256sum >/dev/null; then
    got=$(sha256sum "$bin/farol-remote.new" | cut -d' ' -f1)
else
    got=$(shasum -a 256 "$bin/farol-remote.new" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
    rm -f "$bin/farol-remote.new"
    echo "farol-remote: the download does not match its checksum, so nothing was installed" >&2
    exit 1
fi
chmod +x "$bin/farol-remote.new"
mv "$bin/farol-remote.new" "$bin/farol-remote"

echo "Installed $bin/farol-remote"
command -v tmux >/dev/null || echo "tmux is not installed, and farol-remote needs it to run the sessions."
case ":$PATH:" in
    *":$bin:"*) echo "Next: farol-remote setup" ;;
    *) echo "Add $bin to your PATH, then run: farol-remote setup" ;;
esac
