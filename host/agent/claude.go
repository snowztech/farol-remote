package agent

import (
	"bytes"
	"encoding/json"
	"strconv"
	"strings"
)

// claudeLog is Claude Code's log. Each record names the one it follows with `parentUuid`, so the conversation is a tree:
// tools run side by side each hang off the call that started them, and a prompt you rewound to sits beside the one it replaced.
// Messages are shown in the order they were written, without what a rewind left behind.
type claudeLog struct {
	// The records that gave messages, in the order they were written.
	records  []claudeRecord
	children map[string][]string
	// The prompt written last after each record. A rewind writes a new one after the same record.
	prompts map[string]string
	// Prompts a rewind replaced, and all that followed from them.
	abandoned map[string]bool
	shown     []Message
}

type claudeRecord struct {
	id       string
	messages []Message
}

func newClaudeLog() *claudeLog {
	return &claudeLog{children: map[string][]string{}, prompts: map[string]string{}, abandoned: map[string]bool{}}
}

func (l *claudeLog) messages() []Message {
	if l.shown == nil {
		l.shown = []Message{}
		for _, record := range l.records {
			if !l.abandoned[record.id] {
				l.shown = append(l.shown, record.messages...)
			}
		}
	}
	return l.shown
}

func (l *claudeLog) read(line []byte) bool {
	// Only records in the conversation name a parent. The rest, like settings and file snapshots, are not parsed.
	if !bytes.Contains(line, []byte(`"parentUuid"`)) {
		return false
	}
	var record struct {
		Parent    *string `json:"parentUuid"`
		ID        string  `json:"uuid"`
		Sidechain bool    `json:"isSidechain"`
		Meta      bool    `json:"isMeta"`
		Summary   bool    `json:"isCompactSummary"`
		Type      string  `json:"type"`
		Folder    string  `json:"cwd"`
		Message   *struct {
			Content json.RawMessage `json:"content"`
		} `json:"message"`
	}
	if json.Unmarshal(line, &record) != nil || record.Sidechain || record.ID == "" {
		return false
	}
	id := record.ID
	if parent := record.Parent; parent != nil {
		l.children[*parent] = append(l.children[*parent], id)
		if l.abandoned[*parent] {
			l.abandoned[id] = true
		}
	}
	var messages []Message
	// Claude Code writes notes to itself as your turn, and its summary of a compacted conversation, and marks them.
	if (record.Type == "user" || record.Type == "assistant") && !record.Meta && !record.Summary && record.Message != nil {
		messages = claudeMessages(record.Type, record.Message.Content, record.Folder, id)
	}
	changed := len(messages) > 0
	if parent := record.Parent; parent != nil && hasYou(messages) {
		if earlier, ok := l.prompts[*parent]; ok {
			l.abandon(earlier)
			changed = true
		}
		l.prompts[*parent] = id
	}
	if len(messages) > 0 {
		l.records = append(l.records, claudeRecord{id, messages})
	}
	if changed {
		l.shown = nil
	}
	return changed
}

func hasYou(messages []Message) bool {
	for _, message := range messages {
		if message.Role == You {
			return true
		}
	}
	return false
}

func (l *claudeLog) abandon(id string) {
	left := []string{id}
	for len(left) > 0 {
		next := left[len(left)-1]
		left = left[:len(left)-1]
		if l.abandoned[next] {
			continue
		}
		l.abandoned[next] = true
		left = append(left, l.children[next]...)
	}
}

type claudeBlock struct {
	Type  string         `json:"type"`
	Text  string         `json:"text"`
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// claudeMessages are the messages in one record: your prompt, or the agent's answer and the tools it called.
func claudeMessages(kind string, content json.RawMessage, folder, id string) []Message {
	var blocks []claudeBlock
	var text string
	if json.Unmarshal(content, &text) == nil {
		blocks = []claudeBlock{{Type: "text", Text: text}}
	} else {
		var raw []json.RawMessage
		json.Unmarshal(content, &raw)
		// Each block on its own, so one this does not know the shape of loses only itself.
		blocks = make([]claudeBlock, len(raw))
		for i := range raw {
			json.Unmarshal(raw[i], &blocks[i])
		}
	}
	var messages []Message
	for place, block := range blocks {
		id := id + "." + strconv.Itoa(place)
		text := strings.TrimSpace(block.Text)
		switch {
		case kind == "user" && block.Type == "text":
			// What Claude Code adds to a turn by itself, like a command it ran or a notice, comes wrapped in a tag.
			if text != "" && !strings.HasPrefix(text, "<") {
				messages = append(messages, Message{ID: id, Role: You, Text: text})
			}
		case kind == "user" && block.Type == "image":
			messages = append(messages, Message{ID: id, Role: You, Text: "An image"})
		case kind == "assistant" && block.Type == "text":
			if text != "" {
				messages = append(messages, Message{ID: id, Role: Agent, Text: text})
			}
		case kind == "assistant" && block.Type == "tool_use":
			if block.Name != "" {
				messages = append(messages, claudeTool(block.Name, block.Input, folder, id))
			}
		}
		// Thinking, and what tools gave back, which the agent's own answer says in fewer words, are left out.
	}
	return messages
}

// claudeTool is a tool the agent ran, in one line, with what it did in full where that is worth reading.
func claudeTool(name string, input map[string]any, folder, id string) Message {
	str := func(key string) string {
		value, _ := input[key].(string)
		return value
	}
	or := func(values ...string) string {
		for _, value := range values {
			if value != "" {
				return value
			}
		}
		return ""
	}
	path := str("file_path")
	if path != "" {
		path = relative(path, folder)
	}
	message := Message{ID: id, Role: Tool, Tool: name}
	switch name {
	case "Bash":
		command := str("command")
		message.Text, message.Detail = or(str("description"), firstLine(command), "A command"), command
	case "Edit", "MultiEdit":
		edits, _ := input["edits"].([]any)
		if edits == nil {
			edits = []any{input}
		}
		var diff strings.Builder
		for _, each := range edits {
			edit, _ := each.(map[string]any)
			old, _ := edit["old_string"].(string)
			new, _ := edit["new_string"].(string)
			diff.WriteString(unified(old, new))
		}
		message.Text, message.Detail = or(path, "A file"), limit(diff.String())
	case "Write":
		message.Text, message.Detail = or(path, "A file"), limit(unified("", str("content")))
	case "Read":
		message.Text = or(path, "A file")
	case "Grep", "Glob":
		message.Text = or(str("pattern"), name)
	case "WebSearch":
		message.Text = or(str("query"), name)
	case "WebFetch":
		message.Text = or(str("url"), name)
	case "Task", "Agent":
		message.Text = or(str("description"), name)
	case "TodoWrite":
		message.Text = "Updated the to-do list"
	default:
		message.Text = or(str("skill"), str("description"), name)
	}
	return message
}
