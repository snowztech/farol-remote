#!/bin/sh
# Installs the farol-host CLI, which lets a paired phone reach the agents on a machine that stays on.
#   curl -fsSL https://farol.sh/host.sh | sh
# It downloads the latest release for this machine into ~/.local/bin, or FAROL_BIN when set.
set -eu

os=$(uname -s | tr '[:upper:]' '[:lower:]')
case $(uname -m) in
    x86_64 | amd64) arch=amd64 ;;
    aarch64 | arm64) arch=arm64 ;;
    *) echo "farol-host: no build for $(uname -m)" >&2; exit 1 ;;
esac
case $os in
    linux | darwin) ;;
    *) echo "farol-host: no build for $os" >&2; exit 1 ;;
esac

bin=${FAROL_BIN:-$HOME/.local/bin}
mkdir -p "$bin"
release=https://github.com/snowztech/farol-remote/releases/latest/download
curl -fsSL "$release/farol-host-$os-$arch" -o "$bin/farol-host.new"

# A download that was cut short or changed on the way is not installed.
want=$(curl -fsSL "$release/checksums.txt" | awk -v file="farol-host-$os-$arch" '$2 == file { print $1 }')
if command -v sha256sum >/dev/null; then
    got=$(sha256sum "$bin/farol-host.new" | cut -d' ' -f1)
else
    got=$(shasum -a 256 "$bin/farol-host.new" | cut -d' ' -f1)
fi
if [ -z "$want" ] || [ "$want" != "$got" ]; then
    rm -f "$bin/farol-host.new"
    echo "farol-host: the download does not match its checksum, so nothing was installed" >&2
    exit 1
fi
chmod +x "$bin/farol-host.new"
mv "$bin/farol-host.new" "$bin/farol-host"
# The program was called farol-remote before. That copy goes, so only one name is left on the PATH.
rm -f "$bin/farol-remote"

echo "Installed $bin/farol-host"
command -v tmux >/dev/null || echo "tmux is not installed, and farol-host needs it to run the sessions."
case ":$PATH:" in
    *":$bin:"*) echo "Next: farol-host setup" ;;
    *) echo "Add $bin to your PATH, then run: farol-host setup" ;;
esac
