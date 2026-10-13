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

## How it is laid out

| File | What it holds |
| --- | --- |
| `main.go` | Starts the server |
| `relay.go` | Rooms, and who a message goes to |
| `peer.go` | One connection: reading, writing and its limits |
| `frames.go` | The one byte that says what a message is |

`GET /healthz` answers `ok` for health checks.
