package agent

import (
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// The same cases as Farol's AgentCommandsTests on the Mac.

// homeWith is a home folder of its own, with the files given, by path.
func homeWith(t *testing.T, files map[string]string) string {
	t.Helper()
	home := t.TempDir()
	for path, text := range files {
		file := filepath.Join(home, path)
		os.MkdirAll(filepath.Dir(file), 0o700)
		if err := os.WriteFile(file, []byte(strings.ReplaceAll(text, "HOME", home)), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func names(commands []Command, mention bool) []string {
	var list []string
	for _, command := range commands {
		if command.Mention == mention {
			list = append(list, command.Name)
		}
	}
	return list
}

func find(commands []Command, name string) Command {
	i := slices.IndexFunc(commands, func(c Command) bool { return c.Name == name })
	if i < 0 {
		return Command{}
	}
	return commands[i]
}

func TestClaudeListsItsOwnThenYoursTheProjectsAndPluginsTurnedOn(t *testing.T) {
	home := homeWith(t, map[string]string{
		".claude/commands/ship.md":               "---\ndescription: Push and open a pull request\n---\nDo it.",
		".claude/commands/git/sync.md":           "# Sync\nFetch and rebase on main.",
		".claude/skills/farol/SKILL.md":          "---\nname: farol\ndescription: Farol terminal settings\n---\n",
		".claude/skills/hidden/SKILL.md":         "---\nname: hidden\nuser-invocable: false\n---\n",
		".claude/settings.json":                  `{"enabledPlugins": {"design@market": true, "off@market": false}}`,
		".claude/plugins/installed_plugins.json": `{"plugins": {"design@market": [{"installPath": "HOME/plugins/design"}], "off@market": [{"installPath": "HOME/plugins/off"}]}}`,
		"plugins/design/skills/review/SKILL.md":  "---\nname: review\ndescription: Review a design\n---\n",
		"plugins/off/skills/nope/SKILL.md":       "---\nname: nope\n---\n",
		"shelf/.claude/commands/release.md":      "Cut a release.",
	})
	commands := Commands(filepath.Join(home, ".claude/projects/shelf/abc.jsonl"), filepath.Join(home, "shelf"), home)
	list := names(commands, false)
	for _, name := range []string{"compact", "ship", "git:sync", "farol", "release", "design:review"} {
		if !slices.Contains(list, name) {
			t.Fatalf("%s is missing from %v", name, list)
		}
	}
	if slices.Contains(list, "hidden") || slices.Contains(list, "off:nope") || !sort.StringsAreSorted(list) {
		t.Fatalf("listed %v", list)
	}
	if find(commands, "ship").Detail != "Push and open a pull request" || find(commands, "git:sync").Detail != "Fetch and rebase on main." || !find(commands, "model").Menu {
		t.Fatalf("details %+v %+v", find(commands, "ship"), find(commands, "git:sync"))
	}
}

func TestCodexListsItsOwnYourPromptsAndItsSkillsToMention(t *testing.T) {
	home := homeWith(t, map[string]string{
		".codex/prompts/standup.md":             "---\ndescription: Write my standup\n---\n",
		".agents/skills/archify/SKILL.md":       "---\nname: archify\ndescription: Draw the architecture\n---\n",
		".codex/skills/farol/SKILL.md":          "---\nname: farol\n---\n",
		".codex/skills/.system/plan/SKILL.md":   "---\nname: plan\n---\n",
		".codex/skills/quiet/SKILL.md":          "---\nname: quiet\n---\n",
		"shelf/.agents/skills/release/SKILL.md": "---\nname: release\n---\n",
		".codex/config.toml":                    "model = \"gpt-5\"\n\n[[skills.config]]\npath = \"HOME/.codex/skills/quiet/SKILL.md\"\nenabled = false\n",
	})
	commands := Commands(filepath.Join(home, ".codex/sessions/2026/10/16/rollout.jsonl"), filepath.Join(home, "shelf"), home)
	slash, mentions := names(commands, false), names(commands, true)
	if !slices.Contains(slash, "prompts:standup") || !slices.Contains(slash, "approvals") || slices.Contains(slash, "plugin") {
		t.Fatalf("slash %v", slash)
	}
	sort.Strings(mentions)
	if !slices.Equal(mentions, []string{"archify", "farol", "plan", "release"}) {
		t.Fatalf("mentions %v", mentions)
	}
}

func TestWithoutALogItIsClaudeCodeInItsDefaultFolder(t *testing.T) {
	home := homeWith(t, map[string]string{".claude/skills/farol/SKILL.md": "---\nname: farol\n---\n"})
	list := names(Commands("", "", home), false)
	if !slices.Contains(list, "farol") || !slices.Contains(list, "clear") {
		t.Fatalf("listed %v", list)
	}
}
