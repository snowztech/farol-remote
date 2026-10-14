# Roadmap

Remote comes first, with Farol on the Mac as the host. The rest waits until people use it.

1. **Remote.** Pair an iPhone with Farol on the Mac, read and answer sessions, and get told when an agent waits or finishes. The Mac stays on.
2. **Host.** `farol-host` runs agents on a machine of your own that stays on, so the Mac can be closed or off. Before it leaves experimental, the phone keeps a list of named hosts so adding a server never replaces the Mac, each host says what it can do so the phone shows only that, and the pairing and the encryption have had an outside review.
3. **Other phones.** A web client, for Android and anything with a browser, if people ask for it. It would check itself against the same sample messages the Swift and Go code already share.

A service that runs agents for you is not planned. Claude Code and Codex already run in their own clouds.

Each step waits until people use the one before it.

## Not done yet

What has to be true before Host leaves experimental:

- A host proves itself to the relay. Today someone who learned a room's name could connect as that host and knock the real one off. They could read and send nothing, since the encryption is tied to keys they do not have.
- The relay tells a host when Apple no longer knows a phone, so the host stops asking to notify one that removed the app.
- An outside review of the pairing and the encryption.
