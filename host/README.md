# The farol-host CLI

> **Experimental.** Farol Remote currently focuses on continuing sessions from Farol on the Mac. This CLI previews an always-on host for people who deliberately run their agents on a server.

Runs Farol Remote on a server. `farol-host` lets a paired phone see and type into the tmux sessions of a server, the way Farol on a Mac does for its own sessions. Your agents keep working, and you keep answering them, with the Mac closed or off.

The phone reaches the server through the relay, as it reaches a Mac. Everything is encrypted between the two, so the relay cannot read it.

## Set it up

You need a machine that stays on, with [tmux](https://github.com/tmux/tmux) installed, and Farol on your iPhone.

**1. Install.** This puts `farol-host` in `~/.local/bin`, with `fh` as a shorter name for it.

```sh
curl -fsSL https://farol.sh/host.sh | sh
```

**2. Set it up and pair your phone.**

```sh
farol-host setup
```

It connects Claude Code and Codex, so the phone shows when one works, waits or is done, and opens a session as the agent's conversation. It keeps `farol-host` running, from boot, with systemd. Then it shows a code: scan it from Farol on your phone.

**3. Start an agent.** Every tmux session on the machine is one the phone can open.

```sh
tmux new -s api -c ~/code/api
claude
```

On the phone, the session opens as the agent's conversation, and the terminal is one tap away. An approval or a question shows with its answers, to pick with a tap. Typing a slash lists the agent's commands and skills, and a photo or a screenshot can go with a reply. The phone is notified when an agent waits for you or finishes, even with the app closed, its Live Activity keeps count of the agents that work or wait, can open a new session in a project already open here, with Claude Code, Codex or a plain shell, and shows what changed in a session's repository, file by file. A session with no agent opens as its terminal.

A phone knows several Macs and servers at once. Scanning a server's code adds it next to your Mac, and its sessions show in the same list, with the server's name.

## Commands

| Command | What it does |
| --- | --- |
| `farol-host setup` | Makes the machine ready: connects your agents, keeps `farol-host` running, pairs a phone. Safe to run again. |
| `farol-host` | Lists the commands, and says whether it is running and which phones are paired. |
| `farol-host run` | Lets paired phones in, for as long as it runs. What the service runs for you. |
| `farol-host pair` | Shows a code to pair another phone. `farol-host` has to be running. |
| `farol-host devices` | Lists the phones this machine lets in. |
| `farol-host forget <name>` | Stops letting a phone in. One that is connected is cut off at once. |
| `farol-host status <word>` | Says what the agent in this session is doing. The hooks `setup` adds call it. |
| `farol-host version` | Says which release this is. So do `--version` and `-v`. |

A code works once and lasts two minutes. Whoever scans it can type into your sessions, so show it to nobody else.

## Without systemd

On a machine that has none, `farol-host setup` still connects your agents. Keep it running yourself, in a tmux session of its own:

```sh
tmux new -s farol-host
farol-host run
```

Scan the code it shows, then leave with `Ctrl-b d`.

## Good to know

- **A relay of your own.** Run `farol-host -relay wss://your.host setup` or set `FAROL_RELAY`. A phone stays with the relay it was paired through. See [relay/README.md](../relay/README.md).
- **What it keeps.** This machine's key and the phones it paired, in `farol-host` under your config folder, readable by you alone. `FAROL_HOST_DIR` puts them elsewhere. Where to notify each phone is kept beside them. Pictures from the phone go in `Farol/Attachments` under your cache folder, and are removed after a week.
- **Updating.** Run the install line again, then `farol-host setup` to restart it on the new version and connect the hooks it now needs. Restart your agents so they read them.
- **What setup changes.** It adds hooks to `~/.claude/settings.json` and `~/.codex/hooks.json`, keeping a copy of each as `.farol-backup`, and adds a systemd service named `farol-host`.
- **Removing it.** `systemctl --user disable --now farol-host`, then delete `~/.local/bin/farol-host`, `~/.local/bin/fh` and that folder. As root, leave out `--user`.

## Not done yet

- Starting a task in a new worktree from the phone.

## Work on it

| File | What it holds |
| --- | --- |
| `main.go` | The commands, and the connection to the relay |
| `setup.go` | Connecting the agents' hooks, and the systemd service |
| `host.go` | What a phone may ask, and what it is told |
| `agents.go` | The conversation of the agent a phone watches, a page at a time, answering what it asks, its commands, and pictures |
| `attachments.go` | Putting a picture together from its parts, and checking it is one |
| `notices.go` | Notifying phones when a session waits or finishes |
| `activity.go` | Keeping each phone's Live Activity current |
| `sessions.go` | Opening and closing sessions from the phone |
| `changes.go` | What changed in a session's repository, as the Mac's review panel compares it |
| `agent/` | Reading Claude Code's and Codex's logs, their hooks' input and their commands, as Farol on a Mac does |
| `tmux.go` | Listing, reading and typing into sessions, and what their agent's hooks said |
| `secure.go` | Pairing and the encryption, the same as the Swift package |
| `wire.go` | The messages, written as the Swift package writes them |
| `store.go` | The key, the paired phones and the code on screen |
| `install.sh` | What `farol.sh/host.sh` runs: it downloads a release and checks it |

`testdata/swift.json` was written by the Swift package, from two keys made for the tests. The tests open it to prove a real phone and this CLI understand each other.

```sh
go test -race ./...
go build -o farol-host .   # build it yourself
```
