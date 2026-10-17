package agent

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// The same cases as Farol's AgentFolderTests on the Mac.

func homeOf(t *testing.T, folders map[string][]string) string {
	t.Helper()
	home := t.TempDir()
	for folder, files := range folders {
		os.MkdirAll(filepath.Join(home, folder), 0o700)
		for _, file := range files {
			os.WriteFile(filepath.Join(home, folder, file), nil, 0o600)
		}
	}
	return home
}

func labels(folders []Folder) []string {
	var list []string
	for _, f := range folders {
		list = append(list, f.Label())
	}
	return list
}

func noEnv(string) string { return "" }

func TestFindsEveryFolderWithItsConfigFile(t *testing.T) {
	home := homeOf(t, map[string][]string{
		".claude": {"settings.json"}, ".claude-work": {"settings.json"}, ".claude-personal": {"settings.json"},
		".claude-backup": {}, ".codex": {"config.toml"}, ".codex-work": {"config.toml"}, ".codex-old": {"auth.json"},
	})
	want := []string{"Claude Code", "Claude Code (personal)", "Claude Code (work)", "Codex", "Codex (work)"}
	if got := labels(Folders(home, noEnv)); !slices.Equal(got, want) {
		t.Fatalf("found %v", got)
	}
}

func TestTheDefaultFolderIsAlwaysThere(t *testing.T) {
	if got := labels(Folders(homeOf(t, nil), noEnv)); !slices.Equal(got, []string{"Claude Code", "Codex"}) {
		t.Fatalf("found %v", got)
	}
}

func TestFindsTheFoldersTheEnvironmentNamesOnce(t *testing.T) {
	home := homeOf(t, map[string][]string{".claude": {}, ".claude-work": {"settings.json"}})
	env := map[string]string{"CLAUDE_CONFIG_DIR": filepath.Join(home, ".claude-work"), "CODEX_HOME": "/work/codex-acme"}
	want := []string{"Claude Code", "Claude Code (work)", "Codex", "Codex (acme)"}
	if got := labels(Folders(home, func(name string) string { return env[name] })); !slices.Equal(got, want) {
		t.Fatalf("found %v", got)
	}
}

func TestCommandSetsTheFolderOnlyOffTheDefault(t *testing.T) {
	for folder, want := range map[Folder]string{
		{"claude", "/h/.claude"}:         "claude",
		{"claude", "/h/.claude-work"}:    "CLAUDE_CONFIG_DIR='/h/.claude-work' claude",
		{"codex", "/h/it's/.codex-work"}: `CODEX_HOME='/h/it'\''s/.codex-work' codex`,
	} {
		if got := folder.Command(); got != want {
			t.Fatalf("%q, wanted %q", got, want)
		}
	}
}
