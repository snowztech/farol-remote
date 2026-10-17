package main

import (
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// Runs against a real tmux, on a server of its own, and is skipped where there is none.
func TestListsReadsAndTypesIntoARealSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	// Short, since the path of tmux's socket has a limit a test's own folder can pass.
	dir, err := os.MkdirTemp("/tmp", "farol")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Cleanup(func() {
		run("", "tmux", "kill-server")
		os.RemoveAll(dir)
	})
	if _, err := run("", "tmux", "new-session", "-d", "-s", "demo", "-x", "80", "-y", "24", "cat"); err != nil {
		t.Fatal(err)
	}

	tm := newTmux([]byte("host"))
	list := tm.sessions()
	if len(list) != 1 || list[0].Name != "demo" || list[0].Status != "idle" {
		t.Fatalf("sessions %+v", list)
	}
	id := list[0].ID

	// cat shows what it is given, so what is typed can be read back off the screen.
	if !tm.paste(id, "pasted") || !tm.press(id, "enter") || !tm.keys(id, "typed") || !tm.press(id, "enter") {
		t.Fatal("could not type")
	}
	shows := func(want string) bool {
		for range 40 {
			if text, ok := tm.screen(strings.ToLower(id)); ok && strings.Contains(text, want) {
				return true
			}
			time.Sleep(50 * time.Millisecond)
		}
		return false
	}
	if !shows("pasted") || !shows("typed") {
		text, _ := tm.screen(id)
		t.Fatalf("the screen shows %q", text)
	}
	if _, ok := tm.history(id); !ok {
		t.Fatal("no history")
	}
	made, ok := tm.open(dir, "new.one", "echo opened")
	if list := tm.sessions(); !ok || len(list) != 2 || !slices.ContainsFunc(list, func(s session) bool { return s.ID == made }) {
		t.Fatalf("opened %v %v, sessions %+v", made, ok, list)
	}
	if !tm.close(made) || len(tm.sessions()) != 1 {
		t.Fatal("not closed")
	}
	if !tm.press(id, "C-l") || tm.press(id, "C-Enter") {
		t.Fatal("Control with a letter, and nothing else")
	}
	if _, ok := tm.screen("00000000-0000-0000-0000-000000000000"); ok {
		t.Fatal("read a session that does not exist")
	}

	pane, _ := run("", "tmux", "list-panes", "-t", "demo", "-F", "#{pane_id}")
	pane = strings.TrimSpace(pane)
	const named = `{"session_id":"s","transcript_path":"/logs/a b.jsonl"}`
	const asks = `{"hook_event_name":"PermissionRequest","tool_use_id":"toolu_1","tool_name":"Bash","tool_input":{"command":"rm -rf build\nmake","description":"Clean and build"}}`
	// What each hook says, with what it got on its input, and what the session then shows.
	for _, step := range []struct {
		event, hook, status, log, request string
		started                           bool
	}{
		{"start", "", "idle", "", "", true},
		{"working", named, "working", "/logs/a b.jsonl", "", true},
		{"waiting", asks, "waiting", "/logs/a b.jsonl", "toolu_1", true},
		// A notification that it still waits keeps what it asks.
		{"waiting", `{"hook_event_name":"Notification"}`, "waiting", "/logs/a b.jsonl", "toolu_1", true},
		{"subagent-stop", "", "waiting", "/logs/a b.jsonl", "toolu_1", true},
		{"done", "", "done", "/logs/a b.jsonl", "", true},
		// The conversation stays readable after the agent ends.
		{"clear", "", "idle", "/logs/a b.jsonl", "", false},
		// A new agent's is not the one before it.
		{"start", "", "idle", "", "", true},
	} {
		if !setStatus(pane, step.event, []byte(step.hook)) {
			t.Fatalf("%s is not a status", step.event)
		}
		status := tm.sessions()[0].Status
		agent, _ := tm.agentIn(id)
		request := ""
		if agent.request != nil {
			request = agent.request.ID
		}
		if status != step.status || agent.log != step.log || agent.started != step.started || request != step.request {
			t.Fatalf("after %s the session is %s, with %+v", step.event, status, agent)
		}
	}
	setStatus(pane, "waiting", []byte(asks))
	tm.sessions()
	if agent, _ := tm.agentIn(id); agent.request == nil || agent.request.Detail != "rm -rf build\nmake" {
		t.Fatalf("the request read back as %+v", agent.request)
	}
	tm.answered(id)
	tm.sessions()
	if agent, _ := tm.agentIn(id); agent.request != nil {
		t.Fatal("an answered request is still there")
	}
	if setStatus(pane, "dancing", nil) {
		t.Fatal("took a status that is not one")
	}
}
