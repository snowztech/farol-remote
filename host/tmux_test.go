package main

import (
	"os"
	"os/exec"
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
	if _, ok := tm.screen("00000000-0000-0000-0000-000000000000"); ok {
		t.Fatal("read a session that does not exist")
	}

	pane, _ := run("", "tmux", "list-panes", "-t", "demo", "-F", "#{pane_id}")
	pane = strings.TrimSpace(pane)
	// What each hook says, the log it names, and what the session then shows.
	for _, step := range []struct {
		event, log, status, conversation string
		started                          bool
	}{
		{"start", "", "idle", "", true},
		{"working", "/logs/a b.jsonl", "working", "/logs/a b.jsonl", true},
		{"waiting", "", "waiting", "/logs/a b.jsonl", true},
		{"subagent-stop", "", "waiting", "/logs/a b.jsonl", true},
		{"done", "", "done", "/logs/a b.jsonl", true},
		// The conversation stays readable after the agent ends.
		{"clear", "", "idle", "/logs/a b.jsonl", false},
		// A new agent's is not the one before it.
		{"start", "", "idle", "", true},
	} {
		if !setStatus(pane, step.event, step.log) {
			t.Fatalf("%s is not a status", step.event)
		}
		status := tm.sessions()[0].Status
		log, started := tm.conversation(id)
		if status != step.status || log != step.conversation || started != step.started {
			t.Fatalf("after %s the session is %s, with log %q, started %v", step.event, status, log, started)
		}
	}
	if setStatus(pane, "dancing", "") {
		t.Fatal("took a status that is not one")
	}
}
