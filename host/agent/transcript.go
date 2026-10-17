// Package agent reads what a coding agent says about itself: the input its hooks get, and the conversation it logs.
// It is a port of what Farol on a Mac does in FarolCore, so a server's sessions read on a phone as a Mac's do.
package agent

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"strings"
	"unicode/utf8"
)

type Role string

const (
	You   Role = "you"
	Agent Role = "agent"
	Tool  Role = "tool"
)

// A Message is one step of a conversation, written the way a phone reads a RemoteMessage.
type Message struct {
	ID   string `json:"id"`
	Role Role   `json:"role"`
	// Your words, the agent's Markdown, or a tool's one line.
	Text string `json:"text"`
	Tool string `json:"tool,omitempty"`
	// A command in full, or an edit as a unified diff.
	Detail string `json:"detail,omitempty"`
}

// A Transcript is the conversation an agent logs, as Claude Code or Codex writes it: one JSON record per line, appended as the session goes.
// It is read from where it was left, a megabyte at a time, so a long session costs only what was added since and never sits whole in memory.
// A line that does not read as expected is skipped, so a change in the format loses detail, not the session.
type Transcript struct {
	Path string
	// Which agent wrote it, known from its first line.
	log    agentLog
	offset int64
	// The end of a line the agent was still writing.
	partial []byte
	// In a line too long to keep, until it ends.
	skipping bool
}

// One agent's way of logging, read a line at a time.
type agentLog interface {
	messages() []Message
	// True when the line changed the messages.
	read(line []byte) bool
}

func (t *Transcript) Messages() []Message {
	if t.log == nil {
		return nil
	}
	return t.log.messages()
}

// Refresh reads what the agent added since the last time. True when the messages changed.
func (t *Transcript) Refresh() bool {
	file, err := os.Open(t.Path)
	if err != nil {
		return false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || info.Size() == t.offset {
		return false
	}
	// A log that got shorter was rewritten, so it is read again from the start.
	if info.Size() < t.offset {
		*t = Transcript{Path: t.Path}
	}
	if _, err := file.Seek(t.offset, io.SeekStart); err != nil {
		return false
	}
	changed := false
	chunk := make([]byte, 1<<20)
	for {
		n, err := file.Read(chunk)
		if n > 0 && t.take(chunk[:n]) {
			changed = true
		}
		if err != nil || n == 0 {
			return changed
		}
	}
}

// take reads the lines a chunk ends. True when the messages changed.
func (t *Transcript) take(chunk []byte) bool {
	t.offset += int64(len(chunk))
	changed := false
	// Only the new bytes are searched for the end of a line, since one line can hold megabytes of pasted images.
	searched := len(t.partial)
	t.partial = append(t.partial, chunk...)
	start := 0
	for {
		end := bytes.IndexByte(t.partial[searched:], '\n')
		if end < 0 {
			break
		}
		end += searched
		line := t.partial[start:end]
		start, searched = end+1, end+1
		if t.skipping {
			t.skipping = false
			continue
		}
		if len(line) == 0 {
			continue
		}
		if t.log == nil {
			if isCodex(line) {
				t.log = &codexLog{}
			} else {
				t.log = newClaudeLog()
			}
		}
		if t.log.read(line) {
			changed = true
		}
	}
	t.partial = append(t.partial[:0], t.partial[start:]...)
	// A line of many megabytes is pasted images or a compacted history, nothing to show on a phone, so it is not held.
	if len(t.partial) > 8<<20 {
		t.partial = nil
		t.skipping = true
	}
	return changed
}

// Codex starts its log with a record of the session. Claude Code starts with anything else.
func isCodex(line []byte) bool {
	var record struct {
		Type string `json:"type"`
	}
	return json.Unmarshal(line, &record) == nil && record.Type == "session_meta"
}

// unified is an edit as a diff with one hunk: the old lines removed, the new ones added. Context and line numbers are not in the log.
func unified(old, new string) string {
	lines := []string{"@@"}
	for _, side := range []struct{ text, sign string }{{old, "-"}, {new, "+"}} {
		if side.text == "" {
			continue
		}
		for _, line := range strings.Split(side.text, "\n") {
			lines = append(lines, side.sign+line)
		}
	}
	return strings.Join(lines, "\n") + "\n"
}

func relative(path, folder string) string {
	if folder == "" {
		return path
	}
	if rest, ok := strings.CutPrefix(path, folder+"/"); ok {
		return rest
	}
	return path
}

// limit keeps a detail long enough for any edit worth reading on a phone, and short enough to travel in one message.
func limit(text string) string {
	const most = 20_000
	if len(text) <= most {
		return text
	}
	cut := text[:most]
	// A cut in the middle of a character leaves its first bytes, which are not text.
	for !utf8.ValidString(cut) {
		cut = cut[:len(cut)-1]
	}
	return cut + "\n… cut short, open it on the server for the rest"
}

// firstLine is the first line of a text that has something on it.
func firstLine(text string) string {
	for _, line := range strings.Split(text, "\n") {
		if line != "" {
			return line
		}
	}
	return ""
}
