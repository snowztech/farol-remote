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
	// A new session is a window in the tmux session already there, as a tab.
	made, ok := tm.open(dir, "new-one", "echo opened")
	list = tm.sessions()
	if !ok || len(list) != 2 || !slices.ContainsFunc(list, func(s session) bool { return s.ID == made && s.Name == "demo · new-one" }) {
		t.Fatalf("opened %v %v, sessions %+v", made, ok, list)
	}
	if windows, _ := run("", "tmux", "list-windows", "-t", "demo", "-F", "#{window_id}"); len(strings.Fields(windows)) != 2 {
		t.Fatalf("windows of demo: %q", windows)
	}
	ran := false
	for i := 0; i < 40 && !ran; i++ {
		text, _ := tm.screen(made)
		ran = strings.Contains(text, "\nopened")
		time.Sleep(50 * time.Millisecond)
	}
	if !ran {
		text, _ := tm.screen(made)
		t.Fatalf("the command did not run in the new window: %q", text)
	}
	// What an agent's hooks say of one window is not said of the others.
	first, _ := run("", "tmux", "list-panes", "-t", "demo:0", "-F", "#{pane_id}")
	setStatus(strings.TrimSpace(first), "working", nil)
	for _, s := range tm.sessions() {
		if want := map[bool]string{true: "idle", false: "working"}[s.ID == made]; s.Status != want {
			t.Fatalf("%s is %s", s.Name, s.Status)
		}
	}
	setStatus(strings.TrimSpace(first), "clear", nil)
	if !tm.close(made) || len(tm.sessions()) != 1 || tm.sessions()[0].Name != "demo" {
		t.Fatalf("not closed: %+v", tm.sessions())
	}
	if !tm.press(id, "C-l") || tm.press(id, "C-Enter") {
		t.Fatal("Control with a letter, and nothing else")
	}
	if _, ok := tm.screen("00000000-0000-0000-0000-000000000000"); ok {
		t.Fatal("read a session that does not exist")
	}

	pane, _ := run("", "tmux", "list-panes", "-t", "demo:0", "-F", "#{pane_id}")
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

// With no tmux running, a new session starts one, named after its project.
func TestOpensTheFirstSessionWhenTmuxRunsNone(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "farol")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Cleanup(func() {
		run("", "tmux", "kill-server")
		os.RemoveAll(dir)
	})
	tm := newTmux([]byte("host"))
	made, ok := tm.open(dir, "api", "")
	if list := tm.sessions(); !ok || len(list) != 1 || list[0].ID != made || list[0].Name != "api" {
		t.Fatalf("opened %v %v, sessions %+v", made, ok, list)
	}
}

// Hooks older than windows kept what they said on the session, where every window would read it.
func TestForgetsWhatOlderHooksKeptOnTheSession(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "farol")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Cleanup(func() {
		run("", "tmux", "kill-server")
		os.RemoveAll(dir)
	})
	run("", "tmux", "new-session", "-d", "-s", "old", "cat")
	run("", "tmux", "set-option", "-t", "old", statusOption, "working")
	if list := newTmux([]byte("host")).sessions(); len(list) != 1 || list[0].Status != "idle" {
		t.Fatalf("sessions %+v", list)
	}
}

func TestASessionStoppedFromItsTerminalGoesBackToIdle(t *testing.T) {
	if _, err := exec.LookPath("tmux"); err != nil {
		t.Skip("tmux is not installed")
	}
	dir, err := os.MkdirTemp("/tmp", "farol")
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("TMUX_TMPDIR", dir)
	t.Cleanup(func() {
		run("", "tmux", "kill-server")
		os.RemoveAll(dir)
	})
	if _, err := run("", "tmux", "new-session", "-d", "-s", "agent", "cat"); err != nil {
		t.Fatal(err)
	}
	pane, _ := run("", "tmux", "display-message", "-p", "-t", "agent", "#{pane_id}")
	log := dir + "/session.jsonl"
	setStatus(strings.TrimSpace(pane), "working", []byte(`{"session_id":"s","transcript_path":"`+log+`"}`))
	tm := newTmux([]byte("host"))
	if list := tm.sessions(); list[0].Status != "working" {
		t.Fatalf("sessions %+v", list)
	}

	// Claude Code runs no hook when it is stopped, and only writes it down.
	stop := `{"parentUuid":"a1","type":"user","uuid":"u2","timestamp":"` + time.Now().Add(time.Second).UTC().Format(time.RFC3339Nano) +
		`","message":{"role":"user","content":[{"type":"text","text":"[Request interrupted by user]"}]}}`
	os.WriteFile(log, []byte(stop+"\n"), 0o600)
	if list := tm.sessions(); list[0].Status != "idle" {
		t.Fatalf("sessions %+v", list)
	}
	if kept, _ := run("", "tmux", "show-options", "-w", "-v", "-t", "agent:", statusOption); strings.TrimSpace(kept) != "" {
		t.Fatalf("tmux still says %q", kept)
	}
}
