# Farol Remote

How [Farol](https://github.com/snowztech/farol) on a Mac and a phone talk to each other, so you can answer your coding agents when you are away from the Mac.

Everything that touches security is here, in one small place to read:

- **`Sources/FarolRemote`** is the Swift package both apps use: the messages, pairing with a QR code, and the encryption.
- **`relay/`** is the server in between, in Go. It joins a Mac with its phones and passes bytes it cannot read. It stores nothing.
- **`Sources/RemoteProbe`** is a phone in a terminal, for testing without the iOS app.
- **`farol/`** is the command for a machine that stays on, in Go. `farol host` plays the Mac's part there, with tmux for the sessions. See [farol/README.md](farol/README.md).

## How it is kept private

- A phone is paired by scanning a code on the Mac. The code works once and lasts two minutes, and what makes it valid never reaches the relay.
- After that, each side knows the other's public key. Every connection is encrypted in both directions with HPKE, from Apple's CryptoKit, and tied to numbers made for that connection, so a recording of an older one does not open.
- The relay only ever sees a public key, a room name that is a hash of the Mac's key, and encrypted bytes.

One limit to know: if a Mac's own key were stolen later, traffic recorded from a phone to that Mac could be read. The design has not had an outside review yet.

## Not done yet

- A Mac proves itself to the relay. Today someone who learned a room's name could connect as that Mac and knock the real one off. They could read and send nothing, since the encryption is tied to keys they do not have.
- The relay tells a Mac when Apple no longer knows a phone, so the Mac stops asking to notify one that removed the app.
- An outside review of the pairing and the encryption.

## Try it

```sh
swift test                      # the package
cd relay && go test -race ./... # the relay
```

To run a relay of your own, see [relay/README.md](relay/README.md).

## License

[MIT](LICENSE).
