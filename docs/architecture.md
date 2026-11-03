# Architecture

Three programs take part in Remote: a host that owns the sessions, the relay in between, and the phone. The host is Farol on the Mac. A server running `farol-host` can take its place, and is still experimental.

![The Mac, the relay, the phone and the experimental server](diagrams/architecture.png)

## The parts

| Part | Where | What it does |
| --- | --- | --- |
| **RemoteBridge** | the Farol app on the Mac | Off until Remote is turned on. Sends the session list and the open session's screen, types what the phone sends, and asks for a notification when an agent waits or finishes. |
| **MacLink** and **PhoneLink** | `Sources/FarolRemote` | Pairing and the encrypted channel, one end each. They hold no sockets: frames go in, and what to do about them comes out. |
| **RelayConnection** | `Sources/FarolRemote` | The WebSocket to the relay, on both sides. It comes back by itself after a network change and pings so a dead connection is noticed. |
| **Rooms** | `relay/` | Joins a host with its phones. A room is named by the hash of the host's public key. |
| **Pusher** | `relay/` | Asks Apple to notify a phone. It writes the words itself, so a host cannot make a phone show any text it likes. |
| **RemoteClient** | the Farol for iPhone app | The phone's side: the paired host, the connection, and what the host last said. |
| **farol-host** | `host/`, experimental | Plays the host's part on a server, with tmux windows as its sessions. It speaks the same frames, written again in Go and tested against what the Swift package seals. |

## Messages

The phone sends a `RemoteCommand`, and the host answers with a `RemoteEvent`. Both are in `RemoteMessages.swift`. Each one travels sealed inside a `WireFrame`, and the relay adds one byte in front that says what the frame is: data, joined, left, online, offline or push.

A phone that understands held question forms sends `watch(session:asks:true)`. A host advertising `questionForms` then sends `ask(session:ask:)` with the complete form, or a null ask when it closes. The phone sends one `respondAsk` decision: submit answers by question and option position, discuss the question, or answer in the terminal. The host checks the ask ID and every answer before releasing the hook. Older phones omit `asks` and never receive the new event; older hosts ignore the optional watch field.

A phone that draws the terminal itself sends `terminal(session:open:true,vt:true)`. A host advertising `vt` then also sends `vt(session:cols:rows:data:)`: the screen as VT sequences, with its colors, the modes a program set and the cursor, which the phone replays into its own libghostty-vt terminal of the same size. `history(session:vt:true)` answers with `vtHistory`, the rows above the screen with their colors. What is typed goes back as `input(session:data:)`, bytes the phone already encoded for the program, like `ESC O A` for Up in application mode. The plain `screen` still goes alongside, since the phone reads what an agent asks off it. Older phones never send `vt` and get the plain screen alone; older hosts ignore the field and the phone keeps its text terminal.

A host that also advertises `fit` takes the phone's size, sent as `fit(session:cols:rows:)`, so Claude Code and the shell draw for the phone as they would in an SSH client. The Mac does it only after a while without keyboard or mouse input, and gives the pane its own size back as soon as someone uses it, the phone closes the terminal, or the phone goes. farol-host resizes the tmux window while the phone shows it. A pane that keeps its size is rewrapped on the phone instead.

The host always sends the session list and the screen in full, so a phone that missed some has nothing to replay. The screen goes only while the phone shows the terminal, or while the agent waits on something its hooks did not describe. What a waiting agent asks comes as a `request`, and the phone answers it by its id, so a late answer is refused rather than typed into the next prompt. A command carries its own id, so one that arrives twice is carried out once.

How a connection is set up is in [security.md](security.md).
