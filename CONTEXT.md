# Farol Remote

Farol Remote lets a paired phone read and control terminal sessions without making the relay part of their trust relationship.

## Language

**Host**:
The computer that owns and keeps sessions running. Farol on the Mac is the supported host; a server using the CLI is experimental.
_Avoid_: Device, client, Mac when the concept applies to every host

**Device**:
A phone a host allows to read and control its sessions. A device does not own sessions.
_Avoid_: Host, computer

**Session**:
A running terminal conversation owned by exactly one host.
_Avoid_: Device, connection

**Relay**:
The intermediary that passes encrypted messages between hosts and devices without reading or storing their contents.
_Avoid_: Host, server when referring to a session owner
