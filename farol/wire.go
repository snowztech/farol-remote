package main

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"time"
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
		Notify  *json.RawMessage `json:"notify"`
		Options *json.RawMessage `json:"options"`
		Task    *json.RawMessage `json:"task"`
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
}

// What the host tells the phone. The sessions and the screen are always sent in full, so a phone that missed some has nothing to replay.

func event(name string, body any) []byte {
	text, _ := json.Marshal(map[string]any{name: body})
	return text
}

func helloEvent() []byte { return event("hello", map[string]int{"version": protocolVersion}) }

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

func doneEvent(command string) []byte {
	return event("done", map[string]string{"command": command})
}

func refusedEvent(command, reason string) []byte {
	return event("refused", map[string]string{"command": command, "reason": reason})
}

// The relay's own wrapping, one byte that says what a message is. It matches relay/frames.go.
const (
	frameData byte = iota
	frameJoined
	frameLeft
)

func toPhone(id uint32, payload []byte) []byte {
	message := make([]byte, 5, 5+len(payload))
	message[0] = frameData
	binary.BigEndian.PutUint32(message[1:], id)
	return append(message, payload...)
}
