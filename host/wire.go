package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// Everything here is written the way the Swift package writes it, since a phone reads it with that package.
// Swift names a value that has no name of its own "_0".

const protocolVersion = 1

// one is a value Swift wrote without a name.
type one[T any] struct {
	Value T `json:"_0"`
}

// A wireFrame is what travels through the relay between a host and a phone. Only Sealed carries anything private.
type wireFrame struct {
	Pair    *one[pairingRequest] `json:"pair,omitempty"`
	Paired  *struct{}            `json:"paired,omitempty"`
	Refused *one[string]         `json:"refused,omitempty"`
	Hello   *one[channelHello]   `json:"hello,omitempty"`
	Key     *one[[]byte]         `json:"key,omitempty"`
	Sealed  *one[[]byte]         `json:"sealed,omitempty"`
}

type pairingRequest struct {
	Device       []byte `json:"device"`
	Encapsulated []byte `json:"encapsulated"`
	Sealed       []byte `json:"sealed"`
}

// A channelHello is the first thing each side sends on a new connection, in the clear: who it is, and a number made for this connection.
type channelHello struct {
	Key   []byte `json:"key"`
	Nonce []byte `json:"nonce"`
}

// A device is a phone this host lets in.
type device struct {
	Key  []byte `json:"key"`
	Name string `json:"name"`
}

// An offer is what the QR code carries to the phone. Seeing it is what proves the phone was in front of this terminal.
type offer struct {
	Relay string `json:"relay"`
	Mac   []byte `json:"mac"`
	// Made for this one code. It never goes to the relay, so the relay cannot pair a device of its own.
	Secret []byte `json:"secret"`
	// Seconds since the start of 2001, which is how Swift writes a date.
	Expires int64 `json:"expires"`
}

var year2001 = time.Date(2001, 1, 1, 0, 0, 0, 0, time.UTC)

func (o offer) expires() time.Time {
	return year2001.Add(time.Duration(o.Expires) * time.Second)
}

// link is what the QR code says. The offer rides after the #, the part of a link a browser never sends to the server.
func (o offer) link() string {
	text, _ := json.Marshal(o)
	return "https://farol.sh/pair#" + base64.RawURLEncoding.EncodeToString(text)
}

// room is where a host and its phones meet on the relay: the hash of the host's public key, which only they know.
func room(public []byte) string {
	sum := sha256.Sum256(public)
	return hex.EncodeToString(sum[:])
}

// A command is something the phone asks of the host. The id is the phone's own, so a command sent twice is carried out once.
type command struct {
	ID     string `json:"id"`
	Action struct {
		Hello *struct {
			Version int `json:"version"`
		} `json:"hello"`
		// No session means the phone shows the list, and no screen is read.
		Watch *struct {
			Session string `json:"session"`
		} `json:"watch"`
		// Goes in whole, as a paste, and waits for a Return.
		Send *typed `json:"send"`
		// Typed as keys, one after the other.
		Type *typed `json:"type"`
		Key  *struct {
			Session string `json:"session"`
			Key     string `json:"key"`
		} `json:"key"`
		History *struct {
			Session string `json:"session"`
		} `json:"history"`
		Answer *struct {
			Session string `json:"session"`
			Request string `json:"request"`
			Choice  int    `json:"choice"`
		} `json:"answer"`
		// The phone shows the session's terminal, or stopped showing it.
		Terminal *struct {
			Session string `json:"session"`
			Open    bool   `json:"open"`
		} `json:"terminal"`
		// Control with a letter, like Control-C.
		Control *struct {
			Session string `json:"session"`
			Key     string `json:"key"`
		} `json:"control"`
		Commands *struct {
			Session string `json:"session"`
		} `json:"commands"`
		// One part of a picture, in order.
		Attach *struct {
			Session    string `json:"session"`
			Attachment string `json:"attachment"`
			Part       int    `json:"part"`
			Parts      int    `json:"parts"`
			Data       []byte `json:"data"`
		} `json:"attach"`
		// No message to go before asks for the latest page.
		Conversation *struct {
			Session string  `json:"session"`
			Before  *string `json:"before"`
		} `json:"conversation"`
		// Where Apple can notify this phone.
		Notify *struct {
			Token   []byte   `json:"token"`
			Sandbox bool     `json:"sandbox"`
			Notices []string `json:"notices"`
			Key     []byte   `json:"key"`
			Starts  []byte   `json:"starts"`
		} `json:"notify"`
		// Where Apple can reach the Live Activity this phone started.
		Activity *struct {
			Token   []byte `json:"token"`
			Sandbox bool   `json:"sandbox"`
		} `json:"activity"`
		Options *json.RawMessage `json:"options"`
		Task    *json.RawMessage `json:"task"`
		// A new session in a project from `options`, running the agent with that id from it, or a plain shell.
		Open *struct {
			Project string  `json:"project"`
			Agent   *string `json:"agent"`
		} `json:"open"`
		Close *struct {
			Session string `json:"session"`
		} `json:"close"`
		Changes *struct {
			Session string `json:"session"`
		} `json:"changes"`
		Diff *struct {
			Session string `json:"session"`
			Path    string `json:"path"`
		} `json:"diff"`
		// The phone forgot this host, and is not to be let in, notified, or have its Live Activity updated any more.
		Forget *struct{} `json:"forget"`
	} `json:"action"`
}

type typed struct {
	Session string `json:"session"`
	Text    string `json:"text"`
}

// A session as the phone lists it.
type session struct {
	ID     string  `json:"id"`
	Name   string  `json:"name"`
	Branch *string `json:"branch,omitempty"`
	Status string  `json:"status"`
	// The agent running there, like "claude" or "codex". Empty for a shell.
	Agent string `json:"agent,omitempty"`
	// What it waits on, in one line, when its hook said.
	Asking string `json:"asking,omitempty"`
}

// What the host tells the phone. The sessions and the screen are always sent in full, so a phone that missed some has nothing to replay.

func event(name string, body any) []byte {
	var text bytes.Buffer
	encoder := json.NewEncoder(&text)
	// Go would write each < > and & of a screen or a diff in six bytes, which a page of messages cannot spare. Swift writes them as they are.
	encoder.SetEscapeHTML(false)
	encoder.Encode(map[string]any{name: body})
	return bytes.TrimSuffix(text.Bytes(), []byte("\n"))
}

// What this host can do beyond screens and typing, by the names RemoteFeature gives them.
var features = []string{"conversation", "requests", "commands", "attachments", "terminal", "notifications", "sessions", "changes"}

// helloEvent says the version this host speaks, what to call it, and what it can do.
func helloEvent(name string) []byte {
	return event("hello", map[string]any{"version": protocolVersion, "features": features, "name": name})
}

func sessionsEvent(sessions []session) []byte {
	if sessions == nil {
		sessions = []session{}
	}
	return event("sessions", one[[]session]{sessions})
}

func screenEvent(session, text string) []byte {
	return event("screen", typed{session, text})
}

func historyEvent(session, text string) []byte {
	return event("history", typed{session, text})
}

// Where messages go in the conversation a phone has: in place of all of it, before or after one message, or in place of the ones with their ids.
var latest = map[string]struct{}{"latest": {}}

var changed = map[string]struct{}{"changed": {}}

func placeBefore(id string) any { return map[string]one[string]{"before": {id}} }

func placeAfter(id string) any { return map[string]one[string]{"after": {id}} }

func conversationEvent(session string, messages []agent.Message, place any, more bool) []byte {
	if messages == nil {
		messages = []agent.Message{}
	}
	return event("conversation", map[string]any{"session": session, "messages": messages, "place": place, "more": more})
}

// requestEvent says what a session waits on. Nothing is said by leaving it out, as Swift does.
func requestEvent(session string, request *agent.Request) []byte {
	body := map[string]any{"session": session}
	if request != nil {
		body["request"] = request
	}
	return event("request", body)
}

func commandsEvent(session string, commands []agent.Command) []byte {
	return event("commands", map[string]any{"session": session, "commands": commands})
}

// attachedEvent answers the last part of a picture with where it was put, which the phone puts in a reply.
func attachedEvent(command, attachment, path string) []byte {
	return event("attached", map[string]string{"command": command, "attachment": attachment, "path": path})
}

// optionsEvent says what a new session can be opened in and with. The first agent is the one to offer.
func optionsEvent(projects, agents []choice) []byte {
	if projects == nil {
		projects = []choice{}
	}
	body := map[string]any{"projects": projects, "agents": agents}
	if len(agents) > 0 {
		body["agent"] = agents[0].ID
	}
	return event("options", body)
}

// openedEvent answers `open` with the session it made, so the phone goes straight to it.
func openedEvent(command, session string) []byte {
	return event("opened", map[string]string{"command": command, "session": session})
}

// changesEvent lists the changed files, and what they are compared with, like "Uncommitted" or "Since main".
func changesEvent(session, scope string, files []change) []byte {
	if files == nil {
		files = []change{}
	}
	return event("changes", map[string]any{"session": session, "scope": scope, "files": files})
}

func diffEvent(session, path, text string) []byte {
	return event("diff", map[string]string{"session": session, "path": path, "text": text})
}

func doneEvent(command string) []byte {
	return event("done", map[string]string{"command": command})
}

func refusedEvent(command, reason string) []byte {
	return event("refused", map[string]string{"command": command, "reason": reason})
}

// The most the relay passes in one message, as relay/peer.go says.
const maxMessage = 256 << 10

// The relay's own wrapping, one byte that says what a message is. It matches relay/frames.go.
const (
	frameData byte = iota
	frameJoined
	frameLeft
	frameOnline
	frameOffline
	// Have Apple notify one of this host's phones, which may not be connected at all.
	framePush
)

func toPhone(id uint32, payload []byte) []byte {
	message := make([]byte, 5, 5+len(payload))
	message[0] = frameData
	binary.BigEndian.PutUint32(message[1:], id)
	return append(message, payload...)
}
