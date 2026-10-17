package agent

import (
	"crypto/rand"
	"encoding/json"
)

// What an agent's hooks get on their input, which Claude Code and Codex write as JSON.

// LogIn is the file a hook's input says the agent logs its conversation to, from `session_id` and `transcript_path`.
// Empty when it names none, as Codex's does before the first turn.
func LogIn(hookInput []byte) string {
	var input struct {
		Session string `json:"session_id"`
		Log     string `json:"transcript_path"`
	}
	if json.Unmarshal(hookInput, &input) != nil || input.Session == "" {
		return ""
	}
	return input.Log
}

// A Request is what an agent waits on you for, from its hook rather than its screen: a tool it asks to run, or a question with its answers.
// It is written the way a phone reads a RemoteRequest.
type Request struct {
	ID string `json:"id"`
	// The tool asking, like "Bash". Empty for a question.
	Tool string `json:"tool,omitempty"`
	// The command, the file, or the question, in one line.
	Title string `json:"title"`
	// What the tool would do in full: a script, or an edit as a unified diff.
	Detail string `json:"detail,omitempty"`
	// A question's answers, in the order the agent numbers them. Empty for a tool, whose answers only its screen shows.
	Choices []Choice `json:"choices"`
}

type Choice struct {
	Label  string `json:"label"`
	Detail string `json:"detail,omitempty"`
}

// RequestIn is what a hook's input says the agent asks: Claude Code's or Codex's `PermissionRequest`, or Claude Code's `PreToolUse` for `AskUserQuestion`.
// Nil for anything else.
func RequestIn(hookInput []byte) *Request {
	var input struct {
		Event  string         `json:"hook_event_name"`
		Tool   string         `json:"tool_name"`
		Input  map[string]any `json:"tool_input"`
		ID     string         `json:"tool_use_id"`
		Folder string         `json:"cwd"`
	}
	if json.Unmarshal(hookInput, &input) != nil || input.Tool == "" {
		return nil
	}
	id := input.ID
	if id == "" {
		id = rand.Text()
	}
	switch {
	case input.Tool == "AskUserQuestion":
		// Several questions are asked one after another on the same screen, which one request cannot follow.
		questions, _ := input.Input["questions"].([]any)
		if len(questions) != 1 {
			return nil
		}
		asked, _ := questions[0].(map[string]any)
		question, ok := asked["question"].(string)
		if !ok {
			return nil
		}
		request := &Request{ID: id, Title: question, Choices: []Choice{}}
		options, _ := asked["options"].([]any)
		for _, each := range options {
			option, _ := each.(map[string]any)
			if label, ok := option["label"].(string); ok {
				detail, _ := option["description"].(string)
				request.Choices = append(request.Choices, Choice{label, detail})
			}
		}
		return request
	case input.Event == "PermissionRequest":
		call := claudeTool(input.Tool, input.Input, input.Folder, id)
		return &Request{ID: id, Tool: input.Tool, Title: call.Text, Detail: call.Detail, Choices: []Choice{}}
	}
	return nil
}
