package agent

import (
	"encoding/json"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// A Command is something an agent takes after a slash, written the way a phone reads a RemoteAgentCommand.
type Command struct {
	// Without the slash, like "compact" or "frontend-design:review".
	Name   string `json:"name"`
	Detail string `json:"detail,omitempty"`
	// It opens a picker in the agent's terminal, like /model.
	Menu bool `json:"menu"`
	// Called with a dollar sign rather than a slash, as Codex mentions a skill: `$name`.
	Mention bool `json:"mention"`
}

// Commands is what an agent takes after a slash: its own commands, and the skills and commands of yours, the project's and your plugins'.
// For the agent whose conversation is log, working in project. Without a log, Claude Code in its default folder.
// Read from the agent's config folder and the project, as Farol on a Mac does in AgentCommands.
func Commands(log, project, home string) []Command {
	codex, folder := configFolder(log, home)
	var all []Command
	if !codex {
		all = append(all, claudeCommands...)
		bases := []string{folder}
		if project != "" {
			bases = append(bases, filepath.Join(project, ".claude"))
		}
		for _, base := range bases {
			all = append(all, markdown(filepath.Join(base, "commands"), "")...)
			all = append(all, skills(filepath.Join(base, "skills"), "", nil)...)
		}
		all = append(all, plugins(folder)...)
	} else {
		all = append(all, codexCommands...)
		all = append(all, markdown(filepath.Join(folder, "prompts"), "prompts:")...)
		// Codex mentions skills with a dollar sign. They are yours, the project's, the older place in its own folder, and its own.
		roots := []string{filepath.Join(home, ".agents/skills"), filepath.Join(folder, "skills"), filepath.Join(folder, "skills/.system")}
		if project != "" {
			roots = append(roots, filepath.Join(project, ".agents/skills"))
		}
		off := disabledSkills(folder)
		for _, root := range roots {
			for _, skill := range skills(root, "", off) {
				skill.Mention = true
				all = append(all, skill)
			}
		}
	}
	seen := map[string]bool{}
	list := []Command{}
	for _, command := range all {
		key := "/" + command.Name
		if command.Mention {
			key = "$" + command.Name
		}
		if !seen[key] {
			seen[key] = true
			list = append(list, command)
		}
	}
	sort.SliceStable(list, func(i, j int) bool { return list[i].Name < list[j].Name })
	return list
}

// configFolder is the agent's config folder: Claude Code keeps its logs under `projects` in it, and Codex under `sessions`.
func configFolder(log, home string) (codex bool, folder string) {
	if before, _, ok := strings.Cut(log, "/projects/"); ok {
		return false, before
	}
	if before, _, ok := strings.Cut(log, "/sessions/"); ok {
		return true, before
	}
	return false, filepath.Join(home, ".claude")
}

// markdown is the Markdown files in a folder, each one a command named by its path, with a colon for each folder.
func markdown(directory, prefix string) []Command {
	// Both resolved, since a folder reached through a link lists its files by where they really are.
	base, err := filepath.EvalSymlinks(directory)
	if err != nil {
		return nil
	}
	var list []Command
	filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		real, err := filepath.EvalSymlinks(path)
		name, inside := strings.CutPrefix(strings.TrimSuffix(real, ".md"), base+"/")
		text, ok := head(path)
		if err != nil || !inside || !ok {
			return nil
		}
		list = append(list, Command{Name: prefix + strings.ReplaceAll(name, "/", ":"), Detail: describe(text)})
		return nil
	})
	return list
}

// skills is a folder per skill, each with a SKILL.md. One that says it is not for people to call is left out.
func skills(directory, prefix string, off map[string]bool) []Command {
	folders, _ := os.ReadDir(directory)
	var list []Command
	for _, folder := range folders {
		file := filepath.Join(directory, folder.Name(), "SKILL.md")
		if real, err := filepath.EvalSymlinks(file); err == nil && off[real] {
			continue
		}
		text, ok := head(file)
		if !ok {
			continue
		}
		fields := frontmatter(text)
		if fields["user-invocable"] == "false" {
			continue
		}
		name := fields["name"]
		if name == "" {
			name = folder.Name()
		}
		list = append(list, Command{Name: prefix + name, Detail: describe(text)})
	}
	return list
}

// plugins is the commands and skills of the plugins turned on in the config folder, each under its plugin's name.
func plugins(folder string) []Command {
	var installed struct {
		Plugins map[string][]struct {
			Path string `json:"installPath"`
		} `json:"plugins"`
	}
	var settings struct {
		Enabled map[string]bool `json:"enabledPlugins"`
	}
	if !readJSON(filepath.Join(folder, "plugins/installed_plugins.json"), &installed) || !readJSON(filepath.Join(folder, "settings.json"), &settings) {
		return nil
	}
	var list []Command
	for key, installs := range installed.Plugins {
		if !settings.Enabled[key] || len(installs) == 0 || installs[0].Path == "" {
			continue
		}
		name, _, _ := strings.Cut(key, "@")
		root := installs[0].Path
		list = append(list, markdown(filepath.Join(root, "commands"), name+":")...)
		list = append(list, skills(filepath.Join(root, "skills"), name+":", nil)...)
	}
	return list
}

// disabledSkills is the skills Codex's config turns off, by their SKILL.md: a `[[skills.config]]` table with its `path` and `enabled = false`.
func disabledSkills(folder string) map[string]bool {
	config, err := os.ReadFile(filepath.Join(folder, "config.toml"))
	if err != nil {
		return nil
	}
	off := map[string]bool{}
	tables := strings.Split(string(config), "[[skills.config]]")
	for _, table := range tables[1:] {
		disabled, path := false, ""
		for _, line := range strings.Split(table, "\n") {
			if strings.HasPrefix(line, "[") {
				break
			}
			line = strings.ReplaceAll(line, " ", "")
			disabled = disabled || line == "enabled=false"
			if value, ok := strings.CutPrefix(line, "path="); ok && path == "" {
				path = strings.Trim(value, `"'`)
			}
		}
		if disabled && path != "" {
			if real, err := filepath.EvalSymlinks(path); err == nil {
				path = real
			}
			off[path] = true
		}
	}
	return off
}

// frontmatter is the `key: value` lines between the `---` that open a file.
func frontmatter(text string) map[string]string {
	lines := strings.Split(text, "\n")
	fields := map[string]string{}
	if strings.TrimSpace(lines[0]) != "---" {
		return fields
	}
	for _, line := range lines[1:] {
		if strings.TrimSpace(line) == "---" {
			break
		}
		if key, value, ok := strings.Cut(line, ":"); ok {
			fields[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"'`)
		}
	}
	return fields
}

// describe is its description, or else its first line of prose, short enough for one row on a phone.
func describe(text string) string {
	line := frontmatter(text)["description"]
	if line == "" {
		body := text
		if strings.HasPrefix(text, "---") {
			parts := strings.Split(text, "\n---")
			body = strings.Join(parts[1:], "\n---")
		}
		for _, each := range strings.Split(body, "\n") {
			if each = strings.TrimSpace(each); each != "" && !strings.HasPrefix(each, "#") {
				line = each
				break
			}
		}
	}
	if runes := []rune(line); len(runes) > 140 {
		return string(runes[:139]) + "…"
	}
	return line
}

// head is the start of a file, which is all its name and description need.
func head(file string) (string, bool) {
	opened, err := os.Open(file)
	if err != nil {
		return "", false
	}
	defer opened.Close()
	start, err := io.ReadAll(io.LimitReader(opened, 4096))
	if err != nil {
		return "", false
	}
	return strings.ToValidUTF8(string(start), "�"), true
}

func readJSON(file string, value any) bool {
	text, err := os.ReadFile(file)
	return err == nil && json.Unmarshal(text, value) == nil
}

// Claude Code's own commands. Those that open a picker in its terminal say so.
var claudeCommands = []Command{
	{Name: "add-dir", Detail: "Add a folder the agent can work in"},
	{Name: "agents", Detail: "Manage subagents", Menu: true},
	{Name: "clear", Detail: "Start over with an empty conversation"},
	{Name: "compact", Detail: "Summarize the conversation to free up context"},
	{Name: "config", Detail: "Open the settings", Menu: true},
	{Name: "context", Detail: "Show what fills the context"},
	{Name: "cost", Detail: "Show what this session cost"},
	{Name: "doctor", Detail: "Check the installation"},
	{Name: "export", Detail: "Export the conversation", Menu: true},
	{Name: "help", Detail: "List the commands"},
	{Name: "hooks", Detail: "Manage hooks", Menu: true},
	{Name: "init", Detail: "Write a CLAUDE.md for this project"},
	{Name: "mcp", Detail: "Manage MCP servers", Menu: true},
	{Name: "memory", Detail: "Edit the memory files", Menu: true},
	{Name: "model", Detail: "Pick the model", Menu: true},
	{Name: "permissions", Detail: "Manage what it may do", Menu: true},
	{Name: "plugin", Detail: "Manage plugins", Menu: true},
	{Name: "resume", Detail: "Go back to an earlier conversation", Menu: true},
	{Name: "review", Detail: "Review a pull request"},
	{Name: "rewind", Detail: "Go back to an earlier point", Menu: true},
	{Name: "status", Detail: "Show the account and the model", Menu: true},
	{Name: "usage", Detail: "Show the plan's usage"},
}

// Codex's own commands.
var codexCommands = []Command{
	{Name: "approvals", Detail: "Choose what it may do without asking", Menu: true},
	{Name: "compact", Detail: "Summarize the conversation to free up context"},
	{Name: "diff", Detail: "Show the changes, untracked files too"},
	{Name: "init", Detail: "Write an AGENTS.md for this project"},
	{Name: "mcp", Detail: "List the MCP tools"},
	{Name: "mention", Detail: "Mention a file", Menu: true},
	{Name: "model", Detail: "Pick the model and its reasoning", Menu: true},
	{Name: "new", Detail: "Start a new conversation"},
	{Name: "review", Detail: "Review the changes", Menu: true},
	{Name: "status", Detail: "Show the session's settings and usage"},
}
