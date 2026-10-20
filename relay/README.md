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

Railway deploys relay.farol.sh from GitHub: every push to `main` ships it, with `relay/` as the root directory. To see it start, from this folder:

```sh
make logs     # wait for: farol relay on :8080, notifications on
```

It needs the Railway CLI, logged in and linked to the farol-relay project.

## Notifications

The relay asks Apple or Firebase to notify a phone when the host says an agent is waiting or has finished. Each provider is optional: without its credentials, the relay and the other provider still work.

The same key keeps a phone's Live Activity up to date: how many agents wait and how many work. Apple and the relay see only those two numbers. The session names go along sealed, and only the phone opens them.

| Variable | What it is |
| --- | --- |
| `APNS_KEY` | The text of the `.p8` key from Apple's developer site |
| `APNS_KEY_ID` | That key's id |
| `APNS_TEAM_ID` | The developer team's id |
| `APNS_TOPIC` | The app's bundle id, `sh.farol.ios` unless set |
| `FCM_SERVICE_ACCOUNT_JSON` | JSON for a service account in the Android Firebase project with permission to send FCM messages; set on the relay host only |

A notification says "An agent is waiting for you." or "An agent finished." and nothing else. The relay never learns which session, and a Mac cannot make it send other words.

Android sends `notify.token` as UTF-8 bytes `fcm:v1:<FCM registration token>`. The host already stores these bytes unchanged and sends them as hex in the relay push frame, so no host protocol change is needed. Unmarked tokens keep using APNs. The relay sends Android a Firebase HTTP v1 **data** message, without a Firebase `notification` field: `notice` (`waiting` or `done`), `sealed` (the host's existing encrypted session content), and `host` (the relay room, which Android uses to find its paired host). Android opens `sealed` and displays the local notification. Live Activities remain Apple-only.

The service account's `project_id` selects the Firebase project. The relay exchanges its signed assertion for a short-lived OAuth access token and caches that token. Put the JSON credential in Railway's `FCM_SERVICE_ACCOUNT_JSON` variable, not in the Android repository or app. The Android app's `google-services.json` is a different, client-side configuration file and does not let a sender call FCM.

## What it logs, and what it limits

The log says how busy the relay is and nothing about who uses it: a line when it starts, a line of counts when they change, and a line when a provider refuses a notification, with its error status. A room's name, an address and a phone's token are never written.

To stay useful under abuse it turns away what a real Mac or phone never does:

| Limit | Value |
| --- | --- |
| Connections in total | 5000 |
| Connections from one address | 40 |
| Size of one message | 256 KB |
| What one connection may send | 512 KB a second, after a burst of 2 MB |
| Notifications one host may ask for | 20 APNs and 20 FCM a minute |
| Phones in one room | 8 |

## How it is laid out

| File | What it holds |
| --- | --- |
| `main.go` | Starts the server |
| `relay.go` | Rooms, and who a message goes to |
| `peer.go` | One connection: reading, writing and its limits |
| `push.go` | Asking Apple to notify an iPhone |
| `pushers.go`, `fcm.go` | Routing Android tokens to Firebase and sending FCM data messages |
| `limits.go` | Counting who is connected, and refusing past the limits |
| `frames.go` | The one byte that says what a message is |

`GET /healthz` answers `ok` for health checks.
