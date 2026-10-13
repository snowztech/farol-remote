# farol, for a machine that stays on

`farol host` lets a paired phone see and type into the tmux sessions of a server, the way Farol on a Mac does for its own sessions. Your agents keep working, and you keep answering them, with the Mac closed or off.

The phone reaches the host through the relay, as it reaches a Mac. Everything is encrypted between the phone and the host, so the relay cannot read it.

## Set it up

On the machine that stays on, with [tmux](https://github.com/tmux/tmux) installed:

```sh
curl -fsSL https://raw.githubusercontent.com/snowztech/farol-remote/main/install.sh | sh
farol host
```

The first run shows a code. Scan it from Farol on your phone. From then on, every tmux session on that machine is one the phone can open:

```sh
tmux new -s api -c ~/code/api
claude
```

Leave `farol host` running, in a tmux session of its own or as a service.

## Commands

| Command | What it does |
| --- | --- |
| `farol host` | Lets paired phones in. Shows a code when no phone is paired yet. |
| `farol host pair` | Shows a code to pair another phone. The host has to be running. |
| `farol host devices` | Lists the phones this host lets in. |
| `farol host forget <name>` | Stops letting a phone in. One that is connected is cut off at once. |
| `farol status <word>` | Says what the agent in this session is doing. An agent's hooks call it. |
| `farol version` | Says which release this is. |

A code works once and lasts two minutes. Whoever scans it can type into your sessions, so show it to nobody else.

To use a relay of your own, run `farol host -relay wss://your.host` or set `FAROL_RELAY`. A phone stays with the relay it was paired through.

## Showing what an agent is doing

`farol status working`, `waiting`, `done` and `clear` set what the phone shows for the session they are run in. They are the same words Farol on a Mac takes, so the hooks you use there work here unchanged. Outside tmux the command does nothing.

## What it keeps

The host's key and the phones it paired, in `farol-host` under your config folder, readable by you alone. `FAROL_HOST_DIR` puts them elsewhere.

## Not done yet

- Notifications when an agent waits or finishes.
- Starting a task from the phone.
- A phone paired with a Mac and a host at the same time. Today a phone knows one of them.

## How it is laid out

| File | What it holds |
| --- | --- |
| `main.go` | The commands, and the connection to the relay |
| `host.go` | What a phone may ask, and what it is told |
| `tmux.go` | Listing, reading and typing into sessions, and their status |
| `secure.go` | Pairing and the encryption, the same as the Swift package |
| `wire.go` | The messages, written as the Swift package writes them |
| `store.go` | The key, the paired phones and the code on screen |

`testdata/swift.json` was written by the Swift package, from two keys made for the tests. The tests open it to prove a real phone and this host understand each other.

To build it yourself: `go install github.com/snowztech/farol-remote/farol@latest`.
