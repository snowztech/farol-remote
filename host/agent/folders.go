package agent

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Folder is one config folder of Claude Code or Codex. Someone with a work and a personal account has several.
// As Farol on a Mac finds them, in AgentFolder.
type Folder struct {
	// "claude" or "codex", which is also the command that starts it.
	Kind string
	Dir  string
}

var (
	kinds      = []string{"claude", "codex"}
	kindTitles = map[string]string{"claude": "Claude Code", "codex": "Codex"}
	// The variable that points the agent at another folder.
	kindVariables = map[string]string{"claude": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME"}
	// The file whose presence says a look-alike folder really is a config folder.
	kindConfigs = map[string]string{"claude": "settings.json", "codex": "config.toml"}
)

func (f Folder) name() string { return strings.TrimLeft(filepath.Base(f.Dir), ".") }

// Default is the folder the agent uses when nothing picks another.
func (f Folder) Default() bool { return f.name() == f.Kind }

// Label is "Claude Code" for ~/.claude, "Claude Code (work)" for ~/.claude-work.
func (f Folder) Label() string {
	if f.Default() {
		return kindTitles[f.Kind]
	}
	return kindTitles[f.Kind] + " (" + strings.TrimPrefix(f.name(), f.Kind+"-") + ")"
}

// Command starts the agent on this folder. The default folder starts plainly, since the agent finds it by itself.
func (f Folder) Command() string {
	if f.Default() {
		return f.Kind
	}
	return kindVariables[f.Kind] + "=" + shellQuoted(f.Dir) + " " + f.Kind
}

func shellQuoted(text string) string {
	return "'" + strings.ReplaceAll(text, "'", `'\''`) + "'"
}

// Folders is the default folder of each agent, every `<default>-*` folder that holds the agent's config file, and the folder the environment names.
// The config file keeps out look-alikes such as backups.
func Folders(home string, env func(string) string) []Folder {
	entries, _ := os.ReadDir(home)
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	sort.Strings(names)
	var found []Folder
	for _, kind := range kinds {
		dirs := []string{filepath.Join(home, "."+kind)}
		prefix := "." + kind + "-"
		for _, name := range names {
			if len(name) > len(prefix) && strings.HasPrefix(name, prefix) {
				if _, err := os.Stat(filepath.Join(home, name, kindConfigs[kind])); err == nil {
					dirs = append(dirs, filepath.Join(home, name))
				}
			}
		}
		if value := env(kindVariables[kind]); value != "" {
			dirs = append(dirs, value)
		}
		seen := map[string]bool{}
		for _, dir := range dirs {
			if dir = filepath.Clean(dir); !seen[dir] {
				seen[dir] = true
				found = append(found, Folder{kind, dir})
			}
		}
	}
	return found
}
