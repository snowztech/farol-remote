# farol host

Lets a paired phone see and type into the tmux sessions of a server, the way Farol on a Mac does for its own sessions. Your agents keep working, and you keep answering them, with the Mac closed or off.

The phone reaches the server through the relay, as it reaches a Mac. Everything is encrypted between the two, so the relay cannot read it.

## Set it up

You need a machine that stays on, with [tmux](https://github.com/tmux/tmux) installed, and Farol on your iPhone.

**1. Install.** This puts `farol` in `~/.local/bin`.

```sh
curl -fsSL https://raw.githubusercontent.com/snowztech/farol-remote/main/install.sh | sh
```

**2. Set it up and pair your phone.**

```sh
farol host setup
```

It connects Claude Code and Codex, so the phone shows when one works, waits or is done. It keeps the host running, from boot, with systemd. Then it shows a code: scan it from Farol on your phone.

**3. Start an agent.** Every tmux session on the machine is one the phone can open.

```sh
tmux new -s api -c ~/code/api
claude
```

A phone knows one Mac or one server at a time. Scanning a server's code takes it away from your Mac, and scanning the Mac's code brings it back.

## Commands

| Command | What it does |
| --- | --- |
| `farol host setup` | Makes the machine ready: connects your agents, keeps the host running, pairs a phone. Safe to run again. |
| `farol host` | Lets paired phones in, for as long as it runs. What the service runs for you. |
| `farol host pair` | Shows a code to pair another phone. The host has to be running. |
| `farol host devices` | Lists the phones this host lets in. |
| `farol host forget <name>` | Stops letting a phone in. One that is connected is cut off at once. |
| `farol status <word>` | Says what the agent in this session is doing. The hooks `setup` adds call it. |
| `farol version` | Says which release this is. |

A code works once and lasts two minutes. Whoever scans it can type into your sessions, so show it to nobody else.

## Without systemd

On a machine that has none, `farol host setup` still connects your agents. Keep the host running yourself, in a tmux session of its own:

```sh
tmux new -s farol
farol host
```

Scan the code it shows, then leave with `Ctrl-b d`.

## Good to know

- **A relay of your own.** Run `farol host -relay wss://your.host` or set `FAROL_RELAY`. A phone stays with the relay it was paired through. See [relay/README.md](../relay/README.md).
- **What it keeps.** The host's key and the phones it paired, in `farol-host` under your config folder, readable by you alone. `FAROL_HOST_DIR` puts them elsewhere.
- **Updating.** Run the install line again, then `farol host setup` to restart the host on the new version.
- **What setup changes.** It adds hooks to `~/.claude/settings.json` and `~/.codex/hooks.json`, keeping a copy of each as `.farol-backup`, and adds a systemd service named `farol-host`.
- **Removing it.** `systemctl --user disable --now farol-host`, then delete `~/.local/bin/farol` and that folder. As root, leave out `--user`.

## Not done yet

- Notifications when an agent waits or finishes.
- Starting a task from the phone.
- A phone paired with a Mac and a server at the same time.

## Work on it

| File | What it holds |
| --- | --- |
| `main.go` | The commands, and the connection to the relay |
| `setup.go` | Connecting the agents' hooks, and the systemd service |
| `host.go` | What a phone may ask, and what it is told |
| `tmux.go` | Listing, reading and typing into sessions, and their status |
| `secure.go` | Pairing and the encryption, the same as the Swift package |
| `wire.go` | The messages, written as the Swift package writes them |
| `store.go` | The key, the paired phones and the code on screen |

`testdata/swift.json` was written by the Swift package, from two keys made for the tests. The tests open it to prove a real phone and this host understand each other.

```sh
go test -race ./...
go install github.com/snowztech/farol-remote/farol@latest   # build it yourself
```
