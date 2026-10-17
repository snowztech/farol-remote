package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
)

// `farol-host setup` makes a server ready in one go: the agents report what they do, and the host runs by itself from boot.

// agentHooks are a coding agent, the file it reads its hooks from, and which of its events mean what.
// Claude Code and Codex use the same shape for hooks, and these are the events Farol on a Mac uses for each.
type agentHooks struct {
	name   string
	file   string
	events []hookEvent
	// Said after connecting, when the agent has a step of its own left.
	note string
}

type hookEvent struct {
	name, status, matcher string
}

func agents(home string) []agentHooks {
	return []agentHooks{
		{name: "Claude Code", file: filepath.Join(env("CLAUDE_CONFIG_DIR", filepath.Join(home, ".claude")), "settings.json"), events: []hookEvent{
			// Names the conversation's log the moment the agent starts, so a phone shows it before the first message.
			// Compaction also starts a session event in the middle of a turn, so it must not clear the status.
			{"SessionStart", "start", "startup|resume|clear"},
			{"UserPromptSubmit", "working", ""},
			{"PostToolUse", "working", ""},
			// Notification also fires as an idle reminder after a finished turn. Only permission prompts and questions mean waiting.
			{"Notification", "waiting", "permission_prompt|elicitation_dialog"},
			// These say what is asked. They decide nothing, so the prompt shows as usual.
			{"PermissionRequest", "waiting", ""},
			{"PreToolUse", "waiting", "^AskUserQuestion$"},
			{"Stop", "done", ""},
			{"SessionEnd", "clear", ""},
			{"SubagentStart", "subagent-start", ""},
			{"SubagentStop", "subagent-stop", ""},
		}},
		{name: "Codex", file: filepath.Join(env("CODEX_HOME", filepath.Join(home, ".codex")), "hooks.json"), note: "It asks you to approve the hooks the next time it starts.", events: []hookEvent{
			// Compaction also starts a session event in the middle of a turn, so it must not clear the status.
			{"SessionStart", "start", "startup|resume|clear"},
			{"UserPromptSubmit", "working", ""},
			{"PostToolUse", "working", ""},
			{"PermissionRequest", "waiting", ""},
			{"PreToolUse", "waiting", "^request_user_input$"},
			{"Stop", "done", ""},
			{"Interrupt", "clear", ""},
			{"SessionEnd", "clear", ""},
			{"SubagentStart", "subagent-start", ""},
			{"SubagentStop", "subagent-stop", ""},
		}},
	}
}

// A hook of ours, wherever farol-host was installed when it was added. The ones Farol on a Mac adds call $FAROL_CLI, and are left alone.
var ourHook = regexp.MustCompile(`farol-(host|remote)"? status [a-z-]+$`)

// connectHooks puts the hooks that call `farol-host status` into an agent's settings and leaves the rest alone.
// Ours from an earlier run are replaced, so running it again changes nothing.
func connectHooks(settings map[string]any, events []hookEvent, program string) map[string]any {
	hooks, _ := settings["hooks"].(map[string]any)
	if hooks == nil {
		hooks = map[string]any{}
	}
	for event, value := range hooks {
		groups, ok := value.([]any)
		if !ok {
			continue
		}
		kept := []any{}
		for _, each := range groups {
			group, _ := each.(map[string]any)
			entries, ok := group["hooks"].([]any)
			if !ok {
				kept = append(kept, each)
				continue
			}
			others := []any{}
			for _, entry := range entries {
				command, _ := entry.(map[string]any)["command"].(string)
				if !ourHook.MatchString(command) {
					others = append(others, entry)
				}
			}
			// A group that held only ours goes with them.
			if len(others) > 0 {
				group["hooks"] = others
				kept = append(kept, group)
			}
		}
		if hooks[event] = kept; len(kept) == 0 {
			delete(hooks, event)
		}
	}
	for _, event := range events {
		group := map[string]any{"hooks": []any{map[string]any{"type": "command", "command": fmt.Sprintf("%q status %s", program, event.status)}}}
		if event.matcher != "" {
			group["matcher"] = event.matcher
		}
		groups, _ := hooks[event.name].([]any)
		hooks[event.name] = append(groups, group)
	}
	settings["hooks"] = hooks
	return settings
}

// connectFile does connectHooks to a file. A file that is not what it should be is left as it is, and the error says so.
func connectFile(path string, events []hookEvent, program string) error {
	settings := map[string]any{}
	before, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if len(bytes.TrimSpace(before)) > 0 {
		// Numbers are kept as they were written, so nothing else in the file changes its look.
		decoder := json.NewDecoder(bytes.NewReader(before))
		decoder.UseNumber()
		if err := decoder.Decode(&settings); err != nil {
			return fmt.Errorf("%s is not a JSON object, so it was left alone: %w", path, err)
		}
	}
	var after bytes.Buffer
	encoder := json.NewEncoder(&after)
	encoder.SetIndent("", "  ")
	// Other hooks in the file say && and >, which must stay readable.
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(connectHooks(settings, events, program)); err != nil {
		return err
	}
	if bytes.Equal(before, after.Bytes()) {
		return nil
	}
	// The file as it was stays next to it, in case this was not what you wanted.
	if before != nil {
		if err := os.WriteFile(path+".farol-backup", before, 0o600); err != nil {
			return err
		}
	}
	beside := path + ".farol-new"
	if err := os.WriteFile(beside, after.Bytes(), 0o600); err != nil {
		return err
	}
	return os.Rename(beside, path)
}

const serviceName = "farol-host"

// unit is what tells systemd to keep the host running. For root it is a service of the machine, and for anyone else one of their own.
// The PATH is the one setup was run with, so the service finds the same tmux and git you do.
func unit(program, relay, path, home, dir string, system bool) string {
	wanted := "default.target"
	if system {
		wanted = "multi-user.target"
	}
	text := fmt.Sprintf(`[Unit]
Description=Farol Host, which lets a paired phone see and type into tmux sessions
After=network-online.target
Wants=network-online.target

[Service]
ExecStart=%q run
Environment=%q
Environment=%q
Environment=%q
`, program, "PATH="+path, "HOME="+home, "FAROL_RELAY="+relay)
	if dir != "" {
		text += fmt.Sprintf("Environment=%q\n", "FAROL_HOST_DIR="+dir)
	}
	// A session the phone opens with no tmux running starts tmux from here. Stopping only the host keeps it, and its agents, through a restart or an update.
	return text + fmt.Sprintf(`KillMode=process
Restart=always
RestartSec=5

[Install]
WantedBy=%s
`, wanted)
}

// service is where the unit goes, and what systemctl is told to reach it. False for a machine without systemd.
func service() (file string, scope []string, ok bool) {
	if _, err := exec.LookPath("systemctl"); err != nil || runtime.GOOS != "linux" {
		return "", nil, false
	}
	if os.Geteuid() == 0 {
		return filepath.Join("/etc/systemd/system", serviceName+".service"), nil, true
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config/systemd/user", serviceName+".service"), []string{"--user"}, true
}

// installService has systemd start the host now and at every boot. False is for a machine without systemd.
func installService(program, relay string) (bool, error) {
	file, scope, ok := service()
	if !ok {
		return false, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return true, err
	}
	system := scope == nil
	if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
		return true, err
	}
	// The service had the program's old name. It is stopped and removed, so the two never run at once.
	old := strings.Replace(file, serviceName, "farol-remote", 1)
	if _, err := os.Stat(old); err == nil {
		_ = exec.Command("systemctl", append(scope, "disable", "--now", "farol-remote")...).Run()
		_ = os.Remove(old)
	}
	text := unit(program, relay, os.Getenv("PATH"), home, os.Getenv("FAROL_HOST_DIR"), system)
	if err := os.WriteFile(file, []byte(text), 0o644); err != nil {
		return true, err
	}
	// Restarted and not only started, so a farol-host that was just updated is the one that runs.
	for _, step := range [][]string{{"daemon-reload"}, {"enable", serviceName}, {"restart", serviceName}} {
		if out, err := exec.Command("systemctl", append(scope, step...)...).CombinedOutput(); err != nil {
			return true, fmt.Errorf("systemctl %s: %s", step[0], bytes.TrimSpace(out))
		}
	}
	if !system {
		// Without this, a service of your own stops when you log out and waits for you at boot.
		who, _ := user.Current()
		if who == nil || exec.Command("loginctl", "enable-linger", who.Username).Run() != nil {
			fmt.Println("To keep it running after you log out, run: sudo loginctl enable-linger $USER")
		}
	}
	return true, nil
}

// self is where this program is. Run as fh, it is the link, and this is farol-host, which the hooks and the service name.
func self() string {
	program, err := os.Executable()
	if err == nil {
		program, err = filepath.EvalSymlinks(program)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	return program
}

// setup is `farol-host setup`. It is true when the host is now running as a service.
func setup(relay string) bool {
	program := self()
	home, _ := os.UserHomeDir()
	found := false
	for _, a := range agents(home) {
		if _, err := os.Stat(filepath.Dir(a.file)); err != nil {
			continue
		}
		found = true
		if err := connectFile(a.file, a.events, program); err != nil {
			fmt.Printf("%s was not connected: %v\n", a.name, err)
			continue
		}
		fmt.Printf("%s now tells your phone when it works, waits or is done. %s\n", a.name, a.note)
	}
	if !found {
		fmt.Println("No Claude Code or Codex was found here. Run this again once one is installed, and your phone shows what it is doing.")
	}

	running, err := installService(program, relay)
	switch {
	case err != nil:
		fmt.Printf("farol-host could not be set to run by itself: %v\n", err)
		return false
	case !running:
		fmt.Println("This machine has no systemd, so keep `farol-host` running yourself, in a tmux session for example.")
		return false
	}
	fmt.Println("farol-host is running, and starts again by itself when the machine does.")
	return true
}

// The script that installs a release, the same for a first install and an upgrade.
const installer = "https://farol.sh/host.sh"

// upgrade is `farol-host upgrade`: the latest release in place of this one, then its own setup, which connects the hooks it needs and restarts the service on it.
// Sessions keep running through the restart.
func upgrade(relay string) {
	program := self()
	response, err := http.Get(installer)
	if err == nil && response.StatusCode != http.StatusOK {
		err = errors.New(response.Status)
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "farol-host: the installer could not be downloaded: %v\n", err)
		os.Exit(1)
	}
	defer response.Body.Close()
	install := exec.Command("sh")
	install.Stdin, install.Stdout, install.Stderr = response.Body, os.Stdout, os.Stderr
	// Installed where this one is, and not wherever the script would put a first install.
	install.Env = append(os.Environ(), "FAROL_BIN="+filepath.Dir(program))
	if install.Run() != nil {
		os.Exit(1)
	}
	again := exec.Command(program, "-relay", relay, "setup")
	again.Stdin, again.Stdout, again.Stderr = os.Stdin, os.Stdout, os.Stderr
	if again.Run() != nil {
		os.Exit(1)
	}
}

// uninstall is `farol-host uninstall`: setup and the install undone. The service, the hooks, this machine's key and paired phones, its pictures, and the program.
// The tmux sessions and their agents are left running.
func uninstall(store store) {
	if file, scope, ok := service(); ok {
		if text, err := os.ReadFile(file); err == nil {
			// A service set up before KillMode was in it would take the sessions it started with it when it stops.
			if !bytes.Contains(text, []byte("KillMode=process")) {
				os.WriteFile(file, bytes.Replace(text, []byte("Restart=always"), []byte("KillMode=process\nRestart=always"), 1), 0o644)
				exec.Command("systemctl", append(scope, "daemon-reload")...).Run()
			}
			exec.Command("systemctl", append(scope, "disable", "--now", serviceName)...).Run()
			os.Remove(file)
			exec.Command("systemctl", append(scope, "daemon-reload")...).Run()
			fmt.Println("Removed the farol-host service.")
		}
	}
	if !store.lock() {
		fmt.Fprintln(os.Stderr, "farol-host is still running. Stop it, then run uninstall again.")
		os.Exit(1)
	}
	program := self()
	home, _ := os.UserHomeDir()
	for _, a := range agents(home) {
		if _, err := os.Stat(a.file); err != nil {
			continue
		}
		if err := connectFile(a.file, nil, program); err != nil {
			fmt.Printf("%s's hooks were left in place: %v\n", a.name, err)
			continue
		}
		fmt.Printf("Removed the hooks from %s. Restart it so it stops calling farol-host.\n", a.file)
	}
	if err := os.RemoveAll(store.dir); err != nil {
		fmt.Printf("%s could not be removed: %v\n", store.dir, err)
	}
	if caches, err := os.UserCacheDir(); err == nil {
		os.RemoveAll(filepath.Join(caches, "Farol", "Attachments"))
		// Only when nothing else is in it.
		os.Remove(filepath.Join(caches, "Farol"))
	}
	short := filepath.Join(filepath.Dir(program), "fh")
	if link, _ := os.Readlink(short); link == "farol-host" || link == program {
		os.Remove(short)
	}
	if err := os.Remove(program); err != nil {
		fmt.Printf("%s could not be removed: %v\n", program, err)
		os.Exit(1)
	}
	fmt.Println("farol-host is uninstalled. Your phone lists this machine until you forget it there.")
}
