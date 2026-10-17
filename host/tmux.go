package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// A terminal is where the sessions run. The host only ever asks it these few things, which a test can answer without tmux.
type terminal interface {
	sessions() []session
	// screen is what the session shows now. False when the session is gone.
	screen(id string) (string, bool)
	// history is what scrolled off the top, oldest first.
	history(id string) (string, bool)
	// agentIn is what the hooks of the agent in the session said of it. False when the session is gone.
	agentIn(id string) (agentState, bool)
	// answered forgets what the agent asked, once an answer is on its way, so a second tap is refused rather than typed into what it asks next.
	answered(id string)
	// paste puts text in whole and waits for a Return.
	paste(id, text string) bool
	// keys types text one key after the other.
	keys(id, text string) bool
	press(id, key string) bool
}

// The names tmux gives the keys a phone can press.
var tmuxKeys = map[string]string{
	"enter": "Enter", "escape": "Escape", "tab": "Tab", "backspace": "BSpace",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "interrupt": "C-c",
}

// tmux runs the sessions. Every tmux session on this machine is one the phone can see, and its active pane is the one it reads and types into.
type tmux struct {
	// This host's public key, which every session id is made with, so two hosts a phone sees never share one.
	host []byte
	// The tmux target of each session the phone was told about, by the id it was told.
	targets map[string]string
	// What the hooks said of each session's agent, as of the last look.
	agents map[string]agentState
	// The branch of each folder, asked of git every few seconds and not on every look.
	branches map[string]branch
}

type agentState struct {
	// Where the session's active pane is.
	folder string
	status string
	log    string
	// An agent runs there, from its start to its end.
	started bool
	// What it waits on you for, when its hook said.
	request *agent.Request
}

type branch struct {
	name  string
	asked time.Time
}

func newTmux(host []byte) *tmux {
	return &tmux{host: host, targets: map[string]string{}, agents: map[string]agentState{}, branches: map[string]branch{}}
}

func run(input string, name string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, name, args...)
	command.Stdin = strings.NewReader(input)
	out, err := command.Output()
	return string(out), err
}

func (t *tmux) sessions() []session {
	// No tmux server means no sessions, which tmux says as an error.
	out, _ := run("", "tmux", "list-sessions", "-F", "#{session_id}\t#{session_created}\t#{pane_current_path}\t#{"+statusOption+"}\t#{"+logOption+"}\t#{"+agentOption+"}\t#{"+requestOption+"}\t#{session_name}")
	list, targets, agents := []session{}, map[string]string{}, map[string]agentState{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		// The name is last, since it is the one field that may hold a tab.
		fields := strings.SplitN(line, "\t", 8)
		if len(fields) != 8 {
			continue
		}
		id := sessionID(t.host, fields[0], fields[1])
		targets[id] = fields[0] + ":"
		agents[id] = agentState{folder: fields[2], status: fields[3], log: fields[4], started: fields[5] != "", request: readRequest(fields[6])}
		one := session{ID: id, Name: fields[7], Status: "idle"}
		if fields[3] != "" {
			one.Status = fields[3]
		}
		if name := t.branch(fields[2]); name != "" {
			one.Branch = &name
		}
		list = append(list, one)
	}
	t.targets, t.agents = targets, agents
	return list
}

// sessionID is the id a phone knows a session by. tmux counts sessions from zero after a restart,
// so the time the session was made is part of it, and an old id never means a new session.
// The host's key is part of it too: a phone sees several hosts, and two of them may each make a first session in the same second.
func sessionID(host []byte, tmuxID, created string) string {
	sum := sha256.Sum256(append(append([]byte("farol-host "), host...), " "+tmuxID+" "+created...))
	return strings.ToUpper(fmt.Sprintf("%x-%x-%x-%x-%x", sum[:4], sum[4:6], sum[6:8], sum[8:10], sum[10:16]))
}

func (t *tmux) branch(dir string) string {
	if dir == "" {
		return ""
	}
	known, ok := t.branches[dir]
	if !ok || time.Since(known.asked) > 5*time.Second {
		out, _ := run("", "git", "-C", dir, "branch", "--show-current")
		known = branch{strings.TrimSpace(out), time.Now()}
		t.branches[dir] = known
	}
	return known.name
}

// target is the tmux name of a session, by the id the phone uses. A phone writes ids in capitals, and nothing says it must.
func (t *tmux) target(id string) (string, bool) {
	target, ok := t.targets[strings.ToUpper(id)]
	if !ok {
		// It may have been made since the last look.
		t.sessions()
		target, ok = t.targets[strings.ToUpper(id)]
	}
	return target, ok
}

func (t *tmux) capture(id string, args ...string) (string, bool) {
	target, ok := t.target(id)
	if !ok {
		return "", false
	}
	out, err := run("", "tmux", append([]string{"capture-pane", "-p", "-t", target}, args...)...)
	return strings.TrimRight(out, "\n"), err == nil
}

func (t *tmux) screen(id string) (string, bool) { return t.capture(id) }

func (t *tmux) history(id string) (string, bool) { return t.capture(id, "-S", "-", "-E", "-1") }

func (t *tmux) agentIn(id string) (agentState, bool) {
	// Looked up as a target is, which reads the sessions again for one made since the last look.
	_, ok := t.target(id)
	return t.agents[strings.ToUpper(id)], ok
}

func (t *tmux) answered(id string) {
	if target, ok := t.target(id); ok {
		run("", "tmux", "set-option", "-u", "-t", target, requestOption)
		agent := t.agents[strings.ToUpper(id)]
		agent.request = nil
		t.agents[strings.ToUpper(id)] = agent
	}
}

func (t *tmux) paste(id, text string) bool {
	target, ok := t.target(id)
	if !ok {
		return false
	}
	if _, err := run(text, "tmux", "load-buffer", "-b", "farol-host", "-"); err != nil {
		return false
	}
	// -p pastes the way a terminal does, so an agent takes it as one piece and not as keys.
	_, err := run("", "tmux", "paste-buffer", "-d", "-p", "-b", "farol-host", "-t", target)
	return err == nil
}

func (t *tmux) keys(id, text string) bool {
	target, ok := t.target(id)
	if !ok {
		return false
	}
	_, err := run("", "tmux", "send-keys", "-l", "-t", target, "--", text)
	return err == nil
}

func (t *tmux) press(id, key string) bool {
	target, ok := t.target(id)
	name, known := tmuxKeys[key]
	if !ok || !known {
		return false
	}
	_, err := run("", "tmux", "send-keys", "-t", target, name)
	return err == nil
}

// Where a session's agent is kept: on the tmux session itself, so the host has nothing of its own to store or to lose.
const (
	statusOption = "@farol_status"
	// The file the agent logs its conversation to, as its hooks name it.
	logOption = "@farol_log"
	// Set while an agent runs, from its start to its end. Before its first message it may have no log, and a phone shows it as an empty conversation.
	agentOption = "@farol_agent"
	// What a waiting agent asks, as JSON in base64, which keeps it to one line of tmux's list.
	requestOption = "@farol_request"
)

func writeRequest(request *agent.Request) string {
	text, _ := json.Marshal(request)
	return base64.StdEncoding.EncodeToString(text)
}

func readRequest(option string) *agent.Request {
	text, err := base64.StdEncoding.DecodeString(option)
	if err != nil || option == "" {
		return nil
	}
	var request agent.Request
	if json.Unmarshal(text, &request) != nil {
		return nil
	}
	return &request
}

// What each word an agent's hook says leaves the session showing. Empty goes back to idle.
var statuses = map[string]string{"working": "working", "waiting": "waiting", "done": "done", "clear": "", "start": "", "subagent-start": "working"}

// setStatus is `farol-host status`, called by an agent's hooks from inside a session. pane is tmux's own name for where it runs,
// and hook what the agent gave the hook on its input. False is for a word that is not one of the hooks'.
func setStatus(pane, event string, hook []byte) bool {
	status, known := statuses[event]
	if !known && event != "subagent-stop" {
		return false
	}
	// One tmux call for all of it, each command after a ";".
	var args []string
	set := func(option, value string) {
		if value == "" {
			args = append(args, "set-option", "-u", "-t", pane, option, ";")
		} else {
			args = append(args, "set-option", "-t", pane, option, value, ";")
		}
	}
	// ponytail: a background agent that stops leaves the status as it is. Counting them, as the Mac does, would keep it working until the last one ends.
	if event != "subagent-stop" {
		set(statusOption, status)
	}
	switch log := agent.LogIn(hook); {
	case log != "":
		set(logOption, log)
	case event == "start":
		// A new agent that has not said where it logs yet. The conversation of the one before it is not this one's.
		set(logOption, "")
	}
	switch event {
	case "start":
		set(agentOption, "1")
	case "clear":
		set(agentOption, "")
	}
	switch request := agent.RequestIn(hook); {
	case event == "waiting" && request != nil:
		set(requestOption, writeRequest(request))
	case event == "waiting", strings.HasPrefix(event, "subagent-"):
		// A notification that the agent still waits says nothing new, so what it asks is kept.
	default:
		set(requestOption, "")
	}
	if len(args) > 0 {
		run("", "tmux", args[:len(args)-1]...)
	}
	return true
}
