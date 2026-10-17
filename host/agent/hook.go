package agent

import "encoding/json"

// LogIn is the file a hook's input says the agent logs its conversation to. Claude Code and Codex write that input as JSON,
// with `session_id` and `transcript_path`. Empty when it names none, as Codex's does before the first turn.
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
