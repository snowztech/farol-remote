# Farol Remote

Answer your coding agents from your phone. [Farol](https://github.com/snowztech/farol) shows its sessions to a phone you paired and types what you reply. Everything is encrypted between the two, so nothing in between can read it.

## Use it

In Farol on your Mac, open Settings → Remote, turn it on and show the code. Open Farol on your iPhone and scan it. The sessions already open on the Mac appear on the phone, where you can read and answer them while the Mac stays on.

## Experimental: an always-on host

The `farol-host` CLI is an early preview for running sessions on your own server. It exposes tmux sessions from that server instead of sessions from Farol on the Mac, and the iPhone app still describes the paired host as a Mac.

With tmux installed on the server:

```sh
curl -fsSL https://farol.sh/host.sh | sh
farol-host setup
```

Every tmux session on that machine is then one the phone can open. The Mac and server cannot be paired at the same time yet. The full steps are in [host/README.md](host/README.md).

## What is in this repo

Everything that touches security, in one small place to read.

| Folder | What it is | More |
| --- | --- | --- |
| `Sources/FarolRemote` | The Swift package the Mac and iPhone apps share: the messages, pairing and the encryption | |
| `relay/` | The server in between, in Go. It passes bytes it cannot read and stores nothing | [relay/README.md](relay/README.md) |
| `host/` | The experimental `farol-host` CLI and its installer. It lets a server play the Mac's part with tmux sessions | [host/README.md](host/README.md) |
| `Sources/RemoteProbe` | A phone in a terminal, for testing without the iPhone app | |

## How it works

![The Mac, the relay and the phone](docs/architecture.png)

A server running the `farol-host` CLI connects the same way a Mac does. The relay and the phone do not tell them apart.

## How it is kept private

- A phone is paired by scanning a code on the Mac. The code works once and lasts two minutes, and what makes it valid never reaches the relay.
- After that, each side knows the other's public key. Every connection is encrypted in both directions with HPKE, from Apple's CryptoKit, and tied to numbers made for that connection, so a recording of an older one does not open.
- The relay only ever sees a public key, a room name that is a hash of the Mac's key, and encrypted bytes.

One limit to know: if a Mac's own key were stolen later, traffic recorded from a phone to that Mac could be read. The design has not had an outside review yet.

## Roadmap

Remote first, with the Mac as the host. See [ROADMAP.md](ROADMAP.md).

## Work on it

```sh
swift test                      # the package
cd relay && go test -race ./... # the relay
cd host && go test -race ./... # the farol-host CLI
```

## License

[MIT](LICENSE).
