# Security policy

Farol Remote lets a phone type into terminals on a Mac, so a flaw here matters. Please report one privately, not in a public issue.

## Report a vulnerability

Use [Report a vulnerability](https://github.com/snowztech/farol-remote/security/advisories/new) on this repository. Only the maintainers can read it. Say what you found, how to reproduce it, and what an attacker could do with it.

You will get an answer within a few days. Once it is fixed and released, you are credited in the advisory unless you would rather not be.

## What is in scope

- `Sources/FarolRemote`: pairing, the encrypted channel and the messages
- `relay/`, and the relay running at `relay.farol.sh`
- `host/`, the experimental `farol-host`

Flaws in Farol for Mac or Farol for iPhone that come from how they use this code count too.

## Known limits

Some limits are known and listed in [docs/security.md](docs/security.md#not-covered-yet). A report that shows one of them is worse than described is still welcome.
