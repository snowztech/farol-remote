# Farol relay

Joins a Mac running Farol with the phones it paired, and passes their messages. Everything it passes is encrypted between the Mac and the phone, so the relay cannot read it. It stores nothing.

Farol uses `relay.farol.sh` unless you point it at your own in Settings → Remote.

## Run your own

```sh
docker build -t farol-relay .
docker run -p 8080:8080 farol-relay
```

Or without Docker: `go run .`. It listens on `PORT`, 8080 by default.

Put it behind something that speaks TLS, then enter `wss://your.host` in Settings → Remote and turn Remote off and on.

## Notifications

The relay asks Apple to notify a phone when the Mac says an agent is waiting or has finished. That needs a key from the developer of the iOS app, so only the relay that developer runs can do it. Without the key the relay works as before and sends none.

| Variable | What it is |
| --- | --- |
| `APNS_KEY` | The text of the `.p8` key from Apple's developer site |
| `APNS_KEY_ID` | That key's id |
| `APNS_TEAM_ID` | The developer team's id |
| `APNS_TOPIC` | The app's bundle id, `sh.farol.ios` unless set |

A notification says "An agent is waiting for you." or "An agent finished." and nothing else. The relay never learns which session, and a Mac cannot make it send other words.

## How it is laid out

| File | What it holds |
| --- | --- |
| `main.go` | Starts the server |
| `relay.go` | Rooms, and who a message goes to |
| `peer.go` | One connection: reading, writing and its limits |
| `push.go` | Asking Apple to notify a phone |
| `frames.go` | The one byte that says what a message is |

`GET /healthz` answers `ok` for health checks.
