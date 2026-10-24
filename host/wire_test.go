package main

import (
	"os"
	"reflect"
	"testing"

	"github.com/snowztech/farol-remote/host/agent"
)

// testdata/wire.json is one of each command, event and key, as the Swift package writes them. Its WireTests keep it current.
type wireVectors struct {
	Keys, Commands, Events []string
}

func wire(t *testing.T) wireVectors {
	t.Helper()
	text, err := os.ReadFile("testdata/wire.json")
	if err != nil {
		t.Fatal(err)
	}
	return read[wireVectors](t, string(text))
}

func TestKnowsEveryKeyAPhoneSends(t *testing.T) {
	for _, key := range wire(t).Keys {
		if _, known := tmuxKeys[key]; !known {
			t.Errorf("the key %q is not one tmux is told how to press", key)
		}
	}
}

func TestReadsEveryCommandAPhoneSends(t *testing.T) {
	for _, text := range wire(t).Commands {
		action, read := reflect.ValueOf(read[command](t, text).Action), 0
		for i := range action.NumField() {
			if !action.Field(i).IsNil() {
				read++
			}
		}
		if read != 1 {
			t.Errorf("%s is read as %d actions", text, read)
		}
	}
}

var shelfBranch = "main"

// A waiting agent and a shell, as Swift's sample writes them, with the session and command ids it uses.
var sampleSessions = []session{
	{ID: "11111111-2222-3333-4444-555555555555", Name: "shelf", Branch: &shelfBranch, Status: "waiting", Agent: "codex", Asking: "Run touch probe.txt?"},
	{ID: "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", Name: "zsh", Status: "idle"},
}

func TestWritesEveryEventAsAPhoneReadsIt(t *testing.T) {
	session, command, attachment := "11111111-2222-3333-4444-555555555555", "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE", "99999999-8888-7777-6666-555555555555"
	written := [][]byte{
		optionsEvent([]choice{{"/home/me/shelf", "shelf"}}, []choice{{"claude", "Claude Code"}}),
		openedEvent(command, session),
		sessionsEvent(sampleSessions),
		changesEvent(session, "Uncommitted", []change{
			{Path: "b.go", OldPath: "a.go", Status: "renamed", Added: 1, Removed: 2},
			{Path: "logo.png", Status: "added", Binary: true},
		}),
		conversationEvent(session, []agent.Message{
			{ID: "a1.0", Role: agent.Agent, Text: "Done."},
			{ID: "a2.0", Role: agent.Tool, Text: "npm test", Tool: "Bash", Detail: "npm test"},
		}, placeAfter("u1.0"), false),
		conversationEvent(session, nil, latest, true),
		conversationEvent(session, []agent.Message{{ID: "a2.0", Role: agent.Tool, Text: "npm test", Tool: "Bash", Detail: "npm test", Outcome: "failed"}}, changed, false),
		diffEvent(session, "b.go", "@@ -1 +1 @@\n-a\n+b\n"),
		requestEvent(session, &agent.Request{ID: "toolu_01", Title: "Which test?", Choices: []agent.Choice{{Label: "store", Detail: "the order test"}, {Label: "all"}}}),
		requestEvent(session, nil),
		attachedEvent(command, attachment, "/home/me/.cache/Farol/Attachments/a.png"),
		commandsEvent(session, []agent.Command{{Name: "clear", Detail: "Start over"}, {Name: "deploy", Menu: true, Mention: true}}),
	}
	events := wire(t).Events
	if len(events) != len(written) {
		t.Fatalf("Swift wrote %d events, and this test writes %d", len(events), len(written))
	}
	for i, text := range written {
		if got, want := read[any](t, string(text)), read[any](t, events[i]); !reflect.DeepEqual(got, want) {
			t.Errorf("wrote %s, Swift writes %s", text, events[i])
		}
	}
}
