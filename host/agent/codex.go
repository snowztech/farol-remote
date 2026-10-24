package agent

import (
	"bytes"
	"encoding/json"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

// codexLog is Codex's log: a message or a tool call under each line's `payload`, one after the other. The top of the log names the folder.
type codexLog struct {
	list []Message
	// Where the session works, to show paths from there.
	folder string
}

func (l *codexLog) messages() []Message { return l.list }

func (l *codexLog) read(line []byte) bool {
	// Most lines are Codex's own bookkeeping, like token counts, and are not parsed.
	if !bytes.Contains(line, []byte(`"type":"response_item"`)) && !bytes.Contains(line, []byte(`"type":"session_meta"`)) {
		return false
	}
	var record struct {
		Type    string `json:"type"`
		Payload *struct {
			Type    string `json:"type"`
			ID      string `json:"id"`
			CallID  string `json:"call_id"`
			Role    string `json:"role"`
			Name    string `json:"name"`
			Folder  string `json:"cwd"`
			Content []struct {
				Type string `json:"type"`
				Text string `json:"text"`
			} `json:"content"`
			// Strings for the calls read here. Other payloads may put anything there.
			Input     json.RawMessage `json:"input"`
			Arguments json.RawMessage `json:"arguments"`
		} `json:"payload"`
	}
	if json.Unmarshal(line, &record) != nil || record.Payload == nil {
		return false
	}
	payload := record.Payload
	if record.Type == "session_meta" {
		l.folder = payload.Folder
		return false
	}
	// Ids only need to tell messages apart, and a message without one is numbered after the others.
	id := payload.ID
	if id == "" {
		id = payload.CallID
	}
	if id == "" {
		id = "codex." + strconv.Itoa(len(l.list))
	}
	var message *Message
	switch payload.Type {
	case "message":
		// Codex puts its own context in your turn, and the images you paste, wrapped in tags like <environment_context>.
		var texts []string
		image := false
		for _, part := range payload.Content {
			if text := strings.TrimSpace(part.Text); !strings.HasPrefix(text, "<") {
				texts = append(texts, text)
			}
			image = image || part.Type == "input_image"
		}
		text := strings.TrimSpace(strings.Join(texts, "\n"))
		if text == "" && image {
			text = "An image"
		}
		if text == "" {
			return false
		}
		switch payload.Role {
		case "user":
			message = &Message{ID: id, Role: You, Text: text}
		case "assistant":
			message = &Message{ID: id, Role: Agent, Text: text}
		}
	case "custom_tool_call":
		message = codexTool(rawString(payload.Input), "", l.folder, id)
	case "function_call":
		message = codexTool(rawString(payload.Arguments), payload.Name, l.folder, id)
	}
	if message == nil {
		return false
	}
	l.list = append(l.list, *message)
	return true
}

func rawString(raw json.RawMessage) string {
	var text string
	json.Unmarshal(raw, &text)
	return text
}

var (
	patchFile   = regexp.MustCompile(`^\*\*\* (?:Update|Add|Delete) File: (.+)$`)
	execCommand = regexp.MustCompile(`"?cmd"?\s*:\s*"((?:[^"\\]|\\.)*)"`)
)

// codexTool is what a Codex tool call did: an edit, from the patch it applied, or the commands it ran.
// Its calls are a small script, `tools.exec_command({"cmd": "…"})`, or a patch between `*** Begin Patch` and `*** End Patch`.
func codexTool(input, name, folder, id string) *Message {
	// Codex asks with request_user_input, and while it works with request_user_input_async.
	if strings.HasPrefix(name, "request_user_input") {
		var arguments struct {
			Questions []any `json:"questions"`
		}
		if json.Unmarshal([]byte(input), &arguments) == nil {
			if asked := question(arguments.Questions, id); asked != nil {
				return asked
			}
		}
	}
	if patch, ok := codexPatch(input); ok {
		var files, diff []string
		for _, line := range strings.Split(patch, "\n") {
			if file := patchFile.FindStringSubmatch(line); file != nil {
				files = append(files, relative(file[1], folder))
				diff = append(diff, "@@ "+relative(file[1], folder))
			} else if line != "" && !strings.HasPrefix(line, "*** ") {
				diff = append(diff, line)
			}
		}
		text := strings.Join(files, ", ")
		if text == "" {
			text = "A patch"
		}
		return &Message{ID: id, Role: Tool, Text: text, Tool: "Edit", Detail: limit(strings.Join(diff, "\n") + "\n")}
	}
	var commands []string
	for _, match := range execCommand.FindAllStringSubmatch(input, -1) {
		commands = append(commands, unescaped(match[1]))
	}
	if len(commands) > 0 {
		line := firstLine(commands[0])
		if len(commands) > 1 {
			line += " and " + strconv.Itoa(len(commands)-1) + " more"
		}
		return &Message{ID: id, Role: Tool, Text: line, Tool: "Bash", Detail: strings.Join(commands, "\n")}
	}
	// A call that only waits on another, or keeps Codex's own plan, says nothing worth a line.
	if name == "" || slices.Contains([]string{"wait", "update_plan"}, name) {
		return nil
	}
	return &Message{ID: id, Role: Tool, Text: name, Tool: name}
}

// codexPatch is the patch in a tool call, as written between its markers, with the escapes of the string it came in undone.
func codexPatch(input string) (string, bool) {
	start := strings.Index(input, "*** Begin Patch")
	if start < 0 {
		return "", false
	}
	const end = "*** End Patch"
	length := strings.Index(input[start:], end)
	if length < 0 {
		return "", false
	}
	raw := input[start : start+length+len(end)]
	if strings.Contains(raw, `\n`) {
		return unescaped(raw), true
	}
	return raw, true
}

// unescaped is a JSON string's escapes undone, as `\n` in a command or a patch.
func unescaped(text string) string {
	var plain string
	if json.Unmarshal([]byte(`"`+text+`"`), &plain) != nil {
		return text
	}
	return plain
}
