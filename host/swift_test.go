package main

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// testdata/swift.json was written by the Swift package: two keys made for the test, and what they sealed for each other.
// These tests are what says a real phone and this host understand each other.
type swiftVectors struct {
	MacKey, PhoneKey           []byte
	MacPublic, PhonePublic     []byte
	Offer, Link, Pair, Room    string
	MacHello, PhoneHello       string
	KeyFromPhone, KeyFromMac   []byte
	SealedByPhone, SealedByMac [][]byte
	Commands, Events           []string
}

func vectors(t *testing.T) swiftVectors {
	t.Helper()
	text, err := os.ReadFile("testdata/swift.json")
	if err != nil {
		t.Fatal(err)
	}
	var v swiftVectors
	if err := json.Unmarshal(text, &v); err != nil {
		t.Fatal(err)
	}
	return v
}

func read[T any](t *testing.T, text string) T {
	t.Helper()
	var value T
	if err := json.Unmarshal([]byte(text), &value); err != nil {
		t.Fatalf("%s: %v", text, err)
	}
	return value
}

func TestOpensAPairingRequestFromSwift(t *testing.T) {
	v := vectors(t)
	host, err := loadDeviceKey(v.MacKey)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(host.public, v.MacPublic) || room(host.public) != v.Room {
		t.Fatal("the key or the room is not what Swift makes of it")
	}
	shown := read[offer](t, v.Offer)
	request := read[wireFrame](t, v.Pair).Pair.Value
	during := shown.expires().Add(-time.Minute)

	paired, err := acceptPairing(request, shown, host, during)
	if err != nil {
		t.Fatal(err)
	}
	if paired.Name != "Vector phone" || !reflect.DeepEqual(paired.Key, v.PhonePublic) {
		t.Fatalf("paired %+v", paired)
	}
	if _, err := acceptPairing(request, shown, host, shown.expires()); err != errOfferExpired {
		t.Fatalf("a code that ran out: %v", err)
	}
	other := shown
	other.Secret = make([]byte, 32)
	if _, err := acceptPairing(request, other, host, during); err != errUnknownDevice {
		t.Fatalf("a request made from another code: %v", err)
	}
}

func TestTheLinkReadsBackAsTheOffer(t *testing.T) {
	shown := read[offer](t, vectors(t).Offer)
	link := shown.link()
	text, err := base64.RawURLEncoding.DecodeString(link[len("https://farol.sh/pair#"):])
	if err != nil {
		t.Fatal(err)
	}
	if back := read[offer](t, string(text)); !reflect.DeepEqual(back, shown) {
		t.Fatalf("got %+v", back)
	}
}

func TestOpensWhatSwiftSealsBothWays(t *testing.T) {
	v := vectors(t)
	hostKey, _ := loadDeviceKey(v.MacKey)
	phoneKey, _ := loadDeviceKey(v.PhoneKey)
	hostHello, phoneHello := read[channelHello](t, v.MacHello), read[channelHello](t, v.PhoneHello)

	// The host's side, with the number it used in the recording.
	host := newChannel(hostKey, "mac")
	host.nonce = hostHello.Nonce
	if _, err := host.connect(phoneHello); err != nil {
		t.Fatal(err)
	}
	if err := host.accept(v.KeyFromPhone); err != nil {
		t.Fatal(err)
	}
	for i, sealed := range v.SealedByPhone {
		plain, err := host.open(sealed)
		if err != nil {
			t.Fatalf("command %d: %v", i, err)
		}
		if string(plain) != v.Commands[i] {
			t.Fatalf("command %d opened as %s", i, plain)
		}
	}

	// The phone's side of the same recording, which shows the other direction opens too.
	phone := newChannel(phoneKey, "phone")
	phone.nonce = phoneHello.Nonce
	if _, err := phone.connect(hostHello); err != nil {
		t.Fatal(err)
	}
	if err := phone.accept(v.KeyFromMac); err != nil {
		t.Fatal(err)
	}
	for i, sealed := range v.SealedByMac {
		plain, err := phone.open(sealed)
		if err != nil || string(plain) != v.Events[i] {
			t.Fatalf("event %d: %s %v", i, plain, err)
		}
	}
}

func TestAMessageOpensOnlyInItsPlace(t *testing.T) {
	v := vectors(t)
	hostKey, _ := loadDeviceKey(v.MacKey)
	host := newChannel(hostKey, "mac")
	host.nonce = read[channelHello](t, v.MacHello).Nonce
	host.connect(read[channelHello](t, v.PhoneHello))
	host.accept(v.KeyFromPhone)
	if _, err := host.open(v.SealedByPhone[1]); err != errNotThisOne {
		t.Fatalf("the second message opened first: %v", err)
	}
}

func TestReadsCommandsAsSwiftWritesThem(t *testing.T) {
	v := vectors(t)
	const id, sessionID = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", "11111111-2222-3333-4444-555555555555"
	commands := make([]command, len(v.Commands))
	for i, text := range v.Commands {
		commands[i] = read[command](t, text)
		if commands[i].ID != id {
			t.Fatalf("command %d has id %q", i, commands[i].ID)
		}
	}
	if c := commands[0].Action; c.Hello == nil || c.Hello.Version != 1 {
		t.Fatal("hello")
	}
	if c := commands[1].Action; c.Watch == nil || c.Watch.Session != sessionID {
		t.Fatal("watch a session")
	}
	if c := commands[2].Action; c.Watch == nil || c.Watch.Session != "" {
		t.Fatal("watch the list")
	}
	if c := commands[3].Action; c.Send == nil || *c.Send != (typed{sessionID, "hi"}) {
		t.Fatal("send")
	}
	if c := commands[4].Action; c.Key == nil || c.Key.Key != "enter" || c.Key.Session != sessionID {
		t.Fatal("key")
	}
	if c := commands[5].Action; c.Type == nil || *c.Type != (typed{sessionID, "1"}) {
		t.Fatal("type")
	}
	if c := commands[6].Action; c.History == nil || c.History.Session != sessionID {
		t.Fatal("history")
	}
	if commands[7].Action.Notify == nil || commands[8].Action.Options == nil || commands[9].Action.Task == nil {
		t.Fatal("notify, options or task")
	}
}

func TestWritesEventsAsSwiftReadsThem(t *testing.T) {
	v := vectors(t)
	const id, sessionID = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", "11111111-2222-3333-4444-555555555555"
	branch := "main"
	// What Swift reads, as the Swift package's tests say it does. The recording is from before hosts said what they can do.
	for wrote, want := range map[string]string{
		string(helloEvent("host")): `{"hello":{"version":1,"features":["conversation","requests","commands","attachments","terminal","notifications","sessions","changes"],"name":"host"}}`,
		string(commandsEvent(sessionID, []agent.Command{{Name: "compact", Detail: "Summarize"}, {Name: "farol", Mention: true}})): `{"commands":{"session":"11111111-2222-3333-4444-555555555555","commands":[{"name":"compact","detail":"Summarize","menu":false,"mention":false},{"name":"farol","menu":false,"mention":true}]}}`,
		string(attachedEvent(id, picture, "/home/me/.cache/Farol/Attachments/"+picture+".png")):                                   `{"attached":{"command":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","attachment":"AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE","path":"/home/me/.cache/Farol/Attachments/AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE.png"}}`,
		string(requestEvent(sessionID, nil)): `{"request":{"session":"11111111-2222-3333-4444-555555555555"}}`,
		string(requestEvent(sessionID, &agent.Request{ID: "toolu_1", Tool: "Bash", Title: "Run the tests", Detail: "npm test", Choices: []agent.Choice{}})):                                                                          `{"request":{"session":"11111111-2222-3333-4444-555555555555","request":{"id":"toolu_1","tool":"Bash","title":"Run the tests","detail":"npm test","choices":[]}}}`,
		string(requestEvent(sessionID, &agent.Request{ID: "q", Title: "Which?", Choices: []agent.Choice{{Label: "A", Detail: "first"}, {Label: "B"}}})):                                                                              `{"request":{"session":"11111111-2222-3333-4444-555555555555","request":{"id":"q","title":"Which?","choices":[{"label":"A","detail":"first"},{"label":"B"}]}}}`,
		string(conversationEvent(sessionID, []agent.Message{{ID: "a1.0", Role: agent.Agent, Text: "<b> & co"}, {ID: "a2.0", Role: agent.Tool, Text: "Run the tests", Tool: "Bash", Detail: "npm test"}}, placeAfter("u1.0"), false)): `{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[{"id":"a1.0","role":"agent","text":"<b> & co"},{"id":"a2.0","role":"tool","text":"Run the tests","tool":"Bash","detail":"npm test"}],"place":{"after":{"_0":"u1.0"}},"more":false}}`,
		string(conversationEvent(sessionID, nil, latest, true)):            `{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[],"place":{"latest":{}},"more":true}}`,
		string(conversationEvent(sessionID, nil, placeBefore("x"), false)): `{"conversation":{"session":"11111111-2222-3333-4444-555555555555","messages":[],"place":{"before":{"_0":"x"}},"more":false}}`,
	} {
		if !reflect.DeepEqual(read[any](t, wrote), read[any](t, want)) {
			t.Fatalf("wrote %s, Swift reads %s", wrote, want)
		}
	}
	if strings.Contains(string(conversationEvent(sessionID, []agent.Message{{Text: "<b> & co"}}, latest, false)), `\u003c`) {
		t.Fatal("wrote < as Go does for HTML")
	}
	ours := [][]byte{
		nil,
		sessionsEvent([]session{{sessionID, "n", &branch, "idle"}, {sessionID, "n", nil, "working"}}),
		screenEvent(sessionID, "s"),
		historyEvent(sessionID, "h"),
		doneEvent(id),
		refusedEvent(id, "no"),
	}
	for i, text := range ours {
		if text == nil {
			continue
		}
		if got, want := read[any](t, string(text)), read[any](t, v.Events[i]); !reflect.DeepEqual(got, want) {
			t.Fatalf("event %d: wrote %s, Swift writes %s", i, text, v.Events[i])
		}
	}
}
