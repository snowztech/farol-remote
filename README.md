# Farol Remote

Answer your coding agents from your phone. [Farol](https://github.com/snowztech/farol) shows its sessions to a phone you paired and types what you reply. Everything is encrypted between the two, so nothing in between can read it.

## Use it

In Farol on your Mac, open Settings → Remote, turn it on and show the code. Open Farol on your iPhone and scan it. The sessions already open on the Mac appear on the phone, where you can read and answer them while the Mac stays on.

## Experimental: an always-on host

The `farol-host` CLI is an early preview for running sessions on your own server. It exposes the tmux sessions on that server, and the phone lists them by the server's name, next to your Mac's.

With tmux installed on the server:

```sh
curl -fsSL https://farol.sh/host.sh | sh
farol-host setup
```

Every tmux session on that machine is then one the phone can open. The full steps are in [host/README.md](host/README.md), and the [Remote docs](https://farol.sh/docs#remote) cover the phone side.

## Docs

- [Architecture](docs/architecture.md): the parts, where they live, and the messages between them
- [Security](docs/security.md): pairing, the encrypted channel, what the relay sees, and what is not covered yet
- [Testing](docs/testing.md): what to try with a real Mac, iPhone and agent before Remote goes to testers
- [Roadmap](ROADMAP.md)
- [Terms](CONTEXT.md): host, device, session, relay

![The Mac, the relay, the phone and the experimental server](docs/diagrams/architecture.png)

## What is in this repo

Everything that touches security, in one small place to read.

| Folder | What it is |
| --- | --- |
| `Sources/FarolRemote` | The Swift package the Mac and iPhone apps share: the messages, pairing and the encryption |
| `relay/` | The relay, in Go. See [relay/README.md](relay/README.md) |
| `host/` | The experimental `farol-host` and its installer. See [host/README.md](host/README.md) |
| `Sources/RemoteProbe` | A phone in a terminal, for testing without the iPhone app |
| `docs/` | The pages above, and the diagrams' sources in `docs/diagrams` |

## Work on it

```sh
swift test                      # the package
cd relay && go test -race ./... # the relay
cd host && go test -race ./... # the farol-host CLI
```

## License

[MIT](LICENSE).
