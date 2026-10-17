package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// fakeTerminal is one session that remembers what was typed into it.
type fakeTerminal struct {
	typed []string
	// What the hooks said of its agent.
	log     string
	started bool
}

const fakeSession = "11111111-2222-3333-4444-555555555555"

func (f *fakeTerminal) sessions() []session {
	return []session{{ID: fakeSession, Name: "demo", Status: "idle"}}
}

func (f *fakeTerminal) screen(id string) (string, bool) {
	return "typed: " + strings.Join(f.typed, ","), id == fakeSession
}

func (f *fakeTerminal) history(id string) (string, bool) { return "earlier", id == fakeSession }

func (f *fakeTerminal) conversation(id string) (string, bool) {
	if id != fakeSession {
		return "", false
	}
	return f.log, f.started
}

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

// toHost gives the host one frame as phone 1, then has the phone read the answers.
func (b *bench) toHost(frame wireFrame) {
	b.t.Helper()
	payload, _ := json.Marshal(frame)
	b.host.receive(toPhone(1, payload))
	b.read()
}

// tick has the host look again, as it does a few times a second, and returns what the phone was told.
func (b *bench) tick() []string {
	b.t.Helper()
	before := len(b.events)
	b.host.tick()
	b.read()
	return b.events[before:]
}

// read has the phone read every answer and reply to it.
func (b *bench) read() {
	b.t.Helper()
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
	same(t, b.ask("hello", `{"hello":{"version":1}}`), helloEvent(hostName()), sessionsEvent(b.terminal.sessions()))
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
	if b.refused != "This device is not paired with this machine." {
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
	typed := func() string {
		b.host.mu.Lock()
		defer b.host.mu.Unlock()
		return strings.Join(b.terminal.typed, ",")
	}
	// Return after a paste waits for the agent to take it in.
	if got := typed(); got != "yes" {
		t.Fatalf("typed %s", got)
	}
	time.Sleep(returnDelay("yes", 0) + 100*time.Millisecond)
	if got := typed(); got != "yes,<enter>" {
		t.Fatalf("typed %s", got)
	}
}

func TestReturnWaitsLongerAfterALongerPaste(t *testing.T) {
	if returnDelay("fix it", 0) != 400*time.Millisecond || returnDelay("a\nb\nc", 0) <= returnDelay("a", 0) {
		t.Fatal("the wait does not follow the lines")
	}
	if returnDelay(strings.Repeat("line\n", 500), 0) != returnDelay(strings.Repeat("line\n", 7)+"line", 0) {
		t.Fatal("a whole log waits longer than a few screens")
	}
	if returnDelay("a\nb\nc", 5*time.Second) != 0 {
		t.Fatal("waited after a paste long ago")
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
	same(t, b.ask("hello", `{"hello":{"version":2}}`), helloEvent(hostName()))
	if len(b.host.phones) != 0 {
		t.Fatal("a phone of another version was kept")
	}
}

func TestWhatTheHostCannotDoIsRefusedAndNotLeftUnanswered(t *testing.T) {
	b := newBench(t)
	b.ready()
	for id, c := range map[string]struct{ action, reason string }{
		"newer":  {`{"rename":{}}`, "This machine does not know how to do that. Update farol-host on it."},
		"task":   {`{"options":{}}`, "This machine cannot start tasks from the phone yet."},
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
	first := sessionID([]byte("host"), "$0", "1800000000")
	if first != sessionID([]byte("host"), "$0", "1800000000") || first == sessionID([]byte("host"), "$0", "1800000001") ||
		first == sessionID([]byte("another host"), "$0", "1800000000") {
		t.Fatal("ids do not follow the session")
	}
	if len(first) != 36 || first != strings.ToUpper(first) {
		t.Fatalf("not an id a phone reads: %s", first)
	}
}

// prompt is a line of Claude Code's log where you said something, after the line before it.
func prompt(id, parent, text string) string {
	if parent == "" {
		return fmt.Sprintf(`{"parentUuid":null,"type":"user","uuid":%q,"message":{"role":"user","content":%q}}`+"\n", id, text)
	}
	return fmt.Sprintf(`{"parentUuid":%q,"type":"user","uuid":%q,"message":{"role":"user","content":%q}}`+"\n", parent, id, text)
}

func (b *bench) writeLog(text string) {
	b.t.Helper()
	if b.terminal.log == "" {
		b.terminal.log = filepath.Join(b.t.TempDir(), "log.jsonl")
	}
	file, err := os.OpenFile(b.terminal.log, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		b.t.Fatal(err)
	}
	defer file.Close()
	file.WriteString(text)
}

func conversation(before string) string {
	if before == "" {
		return fmt.Sprintf(`{"conversation":{"session":%q}}`, fakeSession)
	}
	return fmt.Sprintf(`{"conversation":{"session":%q,"before":%q}}`, fakeSession, before)
}

func TestAPhoneReadsTheConversationAndThenWhatTheAgentWrites(t *testing.T) {
	b := newBench(t)
	b.ready()
	b.writeLog(prompt("p1", "", "fix the test"))
	b.ask("w", watch(fakeSession))
	// Watching shows it at once, since the log is known.
	first := []agent.Message{{ID: "p1.0", Role: agent.You, Text: "fix the test"}}
	same(t, b.ask("c", conversation("")), conversationEvent(fakeSession, first, latest, false))

	b.writeLog(prompt("p2", "p1", "and the docs"))
	same(t, b.tick(), conversationEvent(fakeSession, []agent.Message{{ID: "p2.0", Role: agent.You, Text: "and the docs"}}, placeAfter("p1.0"), false))
	if told := b.tick(); len(told) != 0 {
		t.Fatalf("told again %v", told)
	}
}

func TestAPhoneWatchingAnAgentThatJustStartedSeesAnEmptyConversationThenItsLog(t *testing.T) {
	b := newBench(t)
	b.ready()
	b.terminal.started = true
	same(t, b.ask("w", watch(fakeSession)), conversationEvent(fakeSession, nil, latest, false), screenEvent(fakeSession, "typed: "))
	if told := b.tick(); len(told) != 0 {
		t.Fatalf("told again %v", told)
	}
	b.writeLog(prompt("p1", "", "hello"))
	same(t, b.tick(), conversationEvent(fakeSession, []agent.Message{{ID: "p1.0", Role: agent.You, Text: "hello"}}, latest, false))
}

func TestAPhoneAsksForEarlierPages(t *testing.T) {
	b := newBench(t)
	b.ready()
	parent := ""
	for i := range 70 {
		id := fmt.Sprintf("p%d", i)
		b.writeLog(prompt(id, parent, id))
		parent = id
	}
	told := b.ask("c", conversation(""))
	var latestPage struct {
		Conversation struct {
			Messages []agent.Message
			More     bool
		}
	}
	json.Unmarshal([]byte(told[0]), &latestPage)
	if got := latestPage.Conversation; len(got.Messages) != 60 || !got.More || got.Messages[0].ID != "p10.0" {
		t.Fatalf("the latest page has %d messages from %s", len(got.Messages), got.Messages[0].ID)
	}
	told = b.ask("e", conversation("p10.0"))
	if !strings.Contains(told[0], `"place":{"before":{"_0":"p10.0"}}`) || !strings.Contains(told[0], `"more":false`) || strings.Count(told[0], `"role"`) != 10 {
		t.Fatalf("the page before: %s", told[0])
	}
	same(t, b.ask("gone", conversation("rewound")), refusedEvent("gone", "That part of the conversation was rewound."))
}

func TestASessionWithoutAnAgentHasNoConversation(t *testing.T) {
	b := newBench(t)
	b.ready()
	same(t, b.ask("c", conversation("")), refusedEvent("c", "This session has no agent conversation to show."))
}
