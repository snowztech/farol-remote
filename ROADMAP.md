# Roadmap

Remote shipped first, with Farol on the Mac as the host. What comes next depends on what people use.

1. **Remote.** Done. Pair an iPhone with Farol on the Mac, read and answer sessions, and get told when an agent waits or finishes.
2. **Host.** Shipped, still experimental. `farol-host` runs agents on a machine of your own that stays on, so the Mac can be closed or off. The phone keeps a list of named hosts, so adding a server never replaces the Mac, and each host says what it can do, so the phone shows only that. It does what Farol on the Mac does from the phone: conversations, approvals and questions, commands, pictures, the terminal, notifications and the Live Activity, new sessions, and changes.
3. **Other phones.** Not started. A web client, for Android and anything with a browser, if people ask for it. It would check itself against the same sample messages the Swift and Go code already share.

Farol won't run your agents for you. They run on your own Mac or server, and the relay only passes messages it can't read.

## Before Host leaves experimental

- [ ] **The server proves who it is to the relay.** The relay puts a server and its phones in a room named after the server's key. Today anyone who learns that name can connect as the server and knock the real one off. They can't read or send anything without the keys, but they could keep the server offline from your phone. The relay should make a host prove it holds its key before it joins.
- [ ] **The server forgets a phone that's gone.** When someone deletes the app, Apple refuses its notifications. The relay only logs that, so the server keeps asking to notify a phone that no longer exists. The relay should pass the refusal back so the server drops it.
- [ ] **New tasks get their own worktree.** On the Mac, a new task gets its own branch and folder, so agents working in parallel never touch each other's files. On a server, a new session opens a tmux window in a folder. It should work like the Mac.

## The relay

relay.farol.sh is one small server on Railway. It keeps rooms in memory and stores nothing, and the Mac, the phone and `farol-host` all reconnect by themselves when it restarts. One server is enough for now. These keep it that way, and say what to do when it isn't.

- [ ] **Know when it's down.** An outside check calls `/healthz` every minute and alerts by email or on the phone, from somewhere other than Railway, so an outage there can't hide itself.
- [ ] **Never ship a broken one.** Railway's deploy check waits for `/healthz` before a new version replaces the running one.
- [ ] **See it coming.** The log already prints connection counts when they change. When they get near the limit of 5000, it's time to grow.
- [ ] **Grow on one machine first.** A bigger Railway instance and higher limits go a long way, since the relay only passes bytes along.
- [ ] **Then more than one.** A host and its phones have to meet on the same server. Either send each room to a server picked from its name, or share rooms between servers through something like Redis. Still nothing stored.
- [ ] **Browsers, if a web client happens.** Accept connections from the web app's address, and send Web Push next to Apple's notifications.

## Not covered yet

These apply to the Mac and the server alike, and the [security page](docs/security.md#not-covered-yet) lists them too.

- **Older traffic, if a key is stolen later.** Someone who recorded encrypted traffic from a phone to a host, and later stole that host's private key, could read what they recorded. A fresh key for each connection, thrown away after, would close this.
- **No outside review.** Nobody else has reviewed the pairing and the encryption. It's all in one small repo, so if you know this area, a read is very welcome.
