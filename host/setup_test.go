package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const someHost = "/home/me/.local/bin/farol-host"

func TestConnectingAnAgentKeepsWhatWasThereAndChangesNothingTheSecondTime(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	// Someone's own hook, one from Farol on a Mac, and one of ours from an older install. Only the last may go.
	os.WriteFile(file, []byte(`{
  "model": "opus",
  "limit": 10000000000000000001,
  "hooks": {
    "Stop": [
      {"hooks": [{"type": "command", "command": "say done && echo > /tmp/x"}]},
      {"hooks": [{"type": "command", "command": "[ -z \"$FAROL_CLI\" ] || \"$FAROL_CLI\" status done"}]},
      {"hooks": [{"type": "command", "command": "\"/old/place/farol-host\" status done"}]}
    ]
  }
}`), 0o600)
	events := agents("/home/me")[0].events

	if err := connectFile(file, events, someHost); err != nil {
		t.Fatal(err)
	}
	first, _ := os.ReadFile(file)
	text := string(first)
	for _, kept := range []string{`"model": "opus"`, `10000000000000000001`, `say done && echo > /tmp/x`, `$FAROL_CLI\" status done`,
		`\"/home/me/.local/bin/farol-host\" status waiting`, `"matcher": "permission_prompt|elicitation_dialog"`} {
		if !strings.Contains(text, kept) {
			t.Fatalf("%s is missing from\n%s", kept, text)
		}
	}
	if strings.Contains(text, "/old/place") {
		t.Fatal("the hook of an older install is still there")
	}
	if strings.Count(text, someHost) != len(events) {
		t.Fatalf("%d hooks of ours, for %d events", strings.Count(text, someHost), len(events))
	}
	if backup, _ := os.ReadFile(file + ".farol-backup"); !strings.Contains(string(backup), "/old/place") {
		t.Fatal("the file as it was is not kept beside it")
	}

	if err := connectFile(file, events, someHost); err != nil {
		t.Fatal(err)
	}
	if second, _ := os.ReadFile(file); string(second) != text {
		t.Fatal("the second time changed the file")
	}
}

func TestAnAgentWithNoSettingsYetGetsThem(t *testing.T) {
	file := filepath.Join(t.TempDir(), "hooks.json")
	if err := connectFile(file, agents("/home/me")[1].events, someHost); err != nil {
		t.Fatal(err)
	}
	if text, _ := os.ReadFile(file); !strings.Contains(string(text), `status clear`) {
		t.Fatalf("got %s", text)
	}
}

func TestSettingsThatDoNotReadAreLeftAlone(t *testing.T) {
	file := filepath.Join(t.TempDir(), "settings.json")
	os.WriteFile(file, []byte(`{"hooks": `), 0o600)
	if err := connectFile(file, agents("/home/me")[0].events, someHost); err == nil {
		t.Fatal("no error")
	}
	if text, _ := os.ReadFile(file); string(text) != `{"hooks": ` {
		t.Fatalf("the file was changed to %s", text)
	}
}

func TestTheServiceRunsTheHostWithTheSamePathAndRelay(t *testing.T) {
	mine := unit(someHost, "wss://relay.farol.sh", "/usr/bin:/opt/bin", "/home/me", "", false)
	for _, line := range []string{`ExecStart="/home/me/.local/bin/farol-host"`, `Environment="PATH=/usr/bin:/opt/bin"`,
		`Environment="FAROL_RELAY=wss://relay.farol.sh"`, "Restart=always", "WantedBy=default.target"} {
		if !strings.Contains(mine, line) {
			t.Fatalf("%s is missing from\n%s", line, mine)
		}
	}
	roots := unit("/usr/local/bin/farol-host", "wss://relay.farol.sh", "/usr/bin", "/root", "/var/lib/farol", true)
	if !strings.Contains(roots, "WantedBy=multi-user.target") || !strings.Contains(roots, `Environment="FAROL_HOST_DIR=/var/lib/farol"`) {
		t.Fatalf("got\n%s", roots)
	}
}

func TestOnlyOneHostRunsFromAFolder(t *testing.T) {
	t.Setenv("FAROL_HOST_DIR", t.TempDir())
	first, _ := openStore()
	if !first.lock() {
		t.Fatal("the first host could not start")
	}
	held := lockFile
	defer held.Close()
	if second, _ := openStore(); second.lock() {
		t.Fatal("a second host started beside the first")
	}
}

func TestReplacesTheHookOfTheOldName(t *testing.T) {
	if !ourHook.MatchString(`"/home/me/.local/bin/farol-remote" status done`) || !ourHook.MatchString(`farol-host status working`) {
		t.Fatal("a hook of ours was not recognised")
	}
}
