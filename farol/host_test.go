package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
)

// fakeTerminal is one session that remembers what was typed into it.
type fakeTerminal struct {
	typed []string
}

const fakeSession = "11111111-2222-3333-4444-555555555555"

func (f *fakeTerminal) sessions() []session {
	return []session{{ID: fakeSession, Name: "demo", Status: "idle"}}
}

func (f *fakeTerminal) screen(id string) (string, bool) {
	return "typed: " + strings.Join(f.typed, ","), id == fakeSession
}

func (f *fakeTerminal) history(id string) (string, bool) { return "earlier", id == fakeSession }

func (f *fakeTerminal) paste(id, text string) bool {
	if id == fakeSession {
		f.typed = append(f.typed, text)
	}
	return id == fakeSession
}

func (f *fakeTerminal) keys(id, text string) bool { return f.paste(id, text) }

func (f *fakeTerminal) press(id, key string) bool { return f.paste(id, "<"+key+">") }

// A bench is a host and a phone with the relay's job done by hand: what one side sends is given to the other.
type bench struct {
	t        *testing.T
	host     *host
	terminal *fakeTerminal
	phoneKey deviceKey
	phone    *channel
	// What the host sent and the phone has not read yet.
	inbox [][]byte
	// What the phone was told, opened, and what it was refused in the clear.
	events  []string
	refused string
	paired  bool
}

func newBench(t *testing.T) *bench {
	t.Helper()
	t.Setenv("FAROL_HOST_DIR", t.TempDir())
	store, err := openStore()
	if err != nil {
		t.Fatal(err)
	}
	hostKey, err := store.key()
	if err != nil {
		t.Fatal(err)
	}
	phoneKey, _ := newDeviceKey()
	b := &bench{t: t, terminal: &fakeTerminal{}, phoneKey: phoneKey}
	b.host = newHost(hostKey, store, b.terminal)
	b.host.connected(func(message []byte) { b.inbox = append(b.inbox, message) })
	return b
}

// toHost gives the host one frame as phone 1, then has the phone read every answer and reply to it.
func (b *bench) toHost(frame wireFrame) {
	b.t.Helper()
	payload, _ := json.Marshal(frame)
	b.host.receive(toPhone(1, payload))
	for len(b.inbox) > 0 {
		message := b.inbox[0]
		b.inbox = b.inbox[1:]
		var answer wireFrame
		if err := json.Unmarshal(message[5:], &answer); err != nil {
			b.t.Fatal(err)
		}
		switch {
		case answer.Paired != nil:
			b.paired = true
		case answer.Refused != nil:
			b.refused = answer.Refused.Value
		case answer.Hello != nil:
			encapsulated, err := b.phone.connect(answer.Hello.Value)
			if err != nil {
				b.t.Fatal(err)
			}
			b.toHost(wireFrame{Key: &one[[]byte]{encapsulated}})
		case answer.Key != nil:
			if err := b.phone.accept(answer.Key.Value); err != nil {
				b.t.Fatal(err)
			}
		case answer.Sealed != nil:
			plain, err := b.phone.open(answer.Sealed.Value)
			if err != nil {
				b.t.Fatal(err)
			}
			b.events = append(b.events, string(plain))
		}
	}
}

// scan pairs the phone as scanning a new code would.
func (b *bench) scan() {
	b.t.Helper()
	shown, err := b.host.store.newOffer("wss://relay.farol.sh", b.host.key, time.Now())
	if err != nil {
		b.t.Fatal(err)
	}
	b.toHost(wireFrame{Pair: &one[pairingRequest]{requestFor(b.t, shown, b.phoneKey, "Test phone")}})
}

// requestFor is the phone's side of pairing, which only the tests need in Go.
func requestFor(t *testing.T, shown offer, phone deviceKey, name string) pairingRequest {
	t.Helper()
	host, _ := scheme.UnmarshalBinaryPublicKey(shown.Mac)
	sender, _ := suite.NewSender(host, []byte("farol pair 1"))
	encapsulated, sealer, err := sender.SetupAuthPSK(nil, phone.secret, shown.Secret, []byte("farol pair secret"))
	if err != nil {
		t.Fatal(err)
	}
	sealed, _ := sealer.Seal([]byte(name), nil)
	return pairingRequest{Device: phone.public, Encapsulated: encapsulated, Sealed: sealed}
}

// connect opens a new encrypted connection, as a phone does each time it reaches the relay.
func (b *bench) connect() {
	b.t.Helper()
	b.phone = newChannel(b.phoneKey, "phone")
	b.toHost(wireFrame{Hello: &one[channelHello]{b.phone.hello()}})
}

// ask seals a command the way the phone does. It returns what the phone was told since.
func (b *bench) ask(id, action string) []string {
	b.t.Helper()
	sealed, err := b.phone.seal([]byte(fmt.Sprintf(`{"id":%q,"action":%s}`, id, action)))
	if err != nil {
		b.t.Fatal(err)
	}
	before := len(b.events)
	b.toHost(wireFrame{Sealed: &one[[]byte]{sealed}})
	return b.events[before:]
}

func (b *bench) ready() {
	b.t.Helper()
	b.scan()
	b.connect()
	b.ask("hello", `{"hello":{"version":1}}`)
}

func watch(session string) string { return fmt.Sprintf(`{"watch":{"session":%q}}`, session) }

func send(session, text string) string {
	return fmt.Sprintf(`{"send":{"session":%q,"text":%q}}`, session, text)
}

func same(t *testing.T, told []string, want ...[]byte) {
	t.Helper()
	wanted := make([]string, len(want))
	for i, w := range want {
		wanted[i] = string(w)
	}
	if strings.Join(told, "\n") != strings.Join(wanted, "\n") {
		t.Fatalf("the phone was told %v, wanted %v", told, wanted)
	}
}

func TestAPhoneThatScannedTheCodeSeesTheSessions(t *testing.T) {
	b := newBench(t)
	b.scan()
	if devices := b.host.store.devices(); !b.paired || len(devices) != 1 || devices[0].Name != "Test phone" {
		t.Fatalf("paired %v, devices %+v", b.paired, devices)
	}
	if _, shown := b.host.store.offer(); shown {
		t.Fatal("the code is still there after it was used")
	}
	b.connect()
	same(t, b.ask("hello", `{"hello":{"version":1}}`), helloEvent(), sessionsEvent(b.terminal.sessions()))
}

func TestACodeWorksOnce(t *testing.T) {
	b := newBench(t)
	shown, _ := b.host.store.newOffer("wss://relay.farol.sh", b.host.key, time.Now())
	b.toHost(wireFrame{Pair: &one[pairingRequest]{requestFor(t, shown, b.phoneKey, "First")}})
	// A second device with a photo of the same code, after the first one used it.
	late, _ := newDeviceKey()
	b.paired = false
	b.toHost(wireFrame{Pair: &one[pairingRequest]{requestFor(t, shown, late, "Someone else")}})
	if devices := b.host.store.devices(); b.paired || b.refused == "" || len(devices) != 1 {
		t.Fatalf("paired %v, refused %q, devices %+v", b.paired, b.refused, devices)
	}
}

func TestAPhoneThatNeverPairedIsTurnedAway(t *testing.T) {
	b := newBench(t)
	b.connect()
	if b.refused != "This device is not paired with this host." {
		t.Fatalf("refused %q", b.refused)
	}
}

func TestWatchingASessionShowsItsScreenAndTypingChangesIt(t *testing.T) {
	b := newBench(t)
	b.ready()
	same(t, b.ask("w", watch(fakeSession)), screenEvent(fakeSession, "typed: "))
	same(t, b.ask("s", send(fakeSession, "yes")), doneEvent("s"), screenEvent(fakeSession, "typed: yes"))
	b.ask("k", fmt.Sprintf(`{"key":{"session":%q,"key":"enter"}}`, fakeSession))
	same(t, b.ask("h", fmt.Sprintf(`{"history":{"session":%q}}`, fakeSession)), historyEvent(fakeSession, "earlier"))
	if got := strings.Join(b.terminal.typed, ","); got != "yes,<enter>" {
		t.Fatalf("typed %s", got)
	}
}

func TestACommandSentTwiceIsTypedOnce(t *testing.T) {
	b := newBench(t)
	b.ready()
	b.ask("same", send(fakeSession, "yes"))
	// The phone sends again on a new connection, where the host still knows the id.
	b.connect()
	b.ask("hello", `{"hello":{"version":1}}`)
	same(t, b.ask("same", send(fakeSession, "yes")), doneEvent("same"))
	if len(b.terminal.typed) != 1 {
		t.Fatalf("typed %v", b.terminal.typed)
	}
}

func TestNothingIsDoneBeforeTheVersionsAreKnown(t *testing.T) {
	b := newBench(t)
	b.scan()
	b.connect()
	if told := b.ask("early", send(fakeSession, "yes")); len(told) != 0 || len(b.terminal.typed) != 0 {
		t.Fatalf("told %v, typed %v", told, b.terminal.typed)
	}
	// A phone that speaks another version is told ours, and then nothing more.
	same(t, b.ask("hello", `{"hello":{"version":2}}`), helloEvent())
	if len(b.host.phones) != 0 {
		t.Fatal("a phone of another version was kept")
	}
}

func TestWhatTheHostCannotDoIsRefusedAndNotLeftUnanswered(t *testing.T) {
	b := newBench(t)
	b.ready()
	for id, c := range map[string]struct{ action, reason string }{
		"newer":  {`{"rename":{}}`, "The host does not know how to do that. Update farol on it."},
		"task":   {`{"options":{}}`, "This host cannot start tasks from the phone yet."},
		"closed": {send("00000000-0000-0000-0000-000000000000", "x"), "That session is closed."},
	} {
		same(t, b.ask(id, c.action), refusedEvent(id, c.reason))
	}
}

func TestAForgottenPhoneIsCutOffAtOnce(t *testing.T) {
	b := newBench(t)
	b.ready()
	b.host.store.saveDevices(nil)
	b.host.tick()
	if len(b.host.phones) != 0 {
		t.Fatal("still connected")
	}
	b.connect()
	if b.refused == "" {
		t.Fatal("let back in")
	}
}

func TestHistoryThatIsTooLongKeepsItsLastWholeLines(t *testing.T) {
	if got := lastLines("one\ntwo\nthree", 9); got != "three" {
		t.Fatalf("got %q", got)
	}
	if got := lastLines("short", 9); got != "short" {
		t.Fatalf("got %q", got)
	}
}

func TestASessionKeepsItsIdAndANewOneGetsAnother(t *testing.T) {
	first := sessionID("$0", "1800000000")
	if first != sessionID("$0", "1800000000") || first == sessionID("$0", "1800000001") {
		t.Fatal("ids do not follow the session")
	}
	if len(first) != 36 || first != strings.ToUpper(first) {
		t.Fatalf("not an id a phone reads: %s", first)
	}
}
