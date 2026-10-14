# Farol relay

Joins a Mac running Farol with the phones it paired, and passes their messages. Everything it passes is encrypted between the Mac and the phone, so the relay cannot read it. It stores nothing.

Farol uses `relay.farol.sh` unless you point it at your own in Settings → Remote.

## Run your own

```sh
docker build -t farol-relay .
docker run -p 8080:8080 farol-relay
```

Or without Docker: `go run .`. It listens on `PORT`, 8080 by default.

Put it behind something that speaks TLS and sets `X-Real-IP` or adds to `X-Forwarded-For`, which is how the relay counts connections from one address. Then enter `wss://your.host` in Settings → Remote and turn Remote off and on.

## Deploy relay.farol.sh

For whoever runs Farol's own relay, from this folder:

```sh
make deploy   # tests, then ships main to Railway
make logs     # wait for: farol relay on :8080, notifications on
```

It refuses unless you are on `main`, with nothing uncommitted, and the same as `origin/main`, so what runs is always what is on GitHub. It needs the Railway CLI, logged in and linked to the farol-relay project.

## Notifications

The relay asks Apple to notify a phone when the Mac says an agent is waiting or has finished. That needs a key from the developer of the iOS app, so only the relay that developer runs can do it. Without the key the relay works as before and sends none.

| Variable | What it is |
| --- | --- |
| `APNS_KEY` | The text of the `.p8` key from Apple's developer site |
| `APNS_KEY_ID` | That key's id |
| `APNS_TEAM_ID` | The developer team's id |
| `APNS_TOPIC` | The app's bundle id, `sh.farol.ios` unless set |

A notification says "An agent is waiting for you." or "An agent finished." and nothing else. The relay never learns which session, and a Mac cannot make it send other words.

## What it logs, and what it limits

The log says how busy the relay is and nothing about who uses it: a line when it starts, a line of counts when they change, and a line when Apple refuses a notification, with Apple's reason. A room's name, an address and a phone's token are never written.

To stay useful under abuse it turns away what a real Mac or phone never does:

| Limit | Value |
| --- | --- |
| Connections in total | 5000 |
| Connections from one address | 40 |
| Size of one message | 256 KB |
| What one connection may send | 512 KB a second, after a burst of 2 MB |
| Notifications one Mac may ask for | 20 a minute |
| Phones in one room | 8 |

## How it is laid out

| File | What it holds |
| --- | --- |
| `main.go` | Starts the server |
| `relay.go` | Rooms, and who a message goes to |
| `peer.go` | One connection: reading, writing and its limits |
| `push.go` | Asking Apple to notify a phone |
| `limits.go` | Counting who is connected, and refusing past the limits |
| `frames.go` | The one byte that says what a message is |

`GET /healthz` answers `ok` for health checks.
