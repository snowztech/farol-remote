package main

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

// The changes view: what changed in a session's repository, as Farol on a Mac's review panel compares it, in its Diff.
// Your own work since the branch left its base, or uncommitted work on the base branch itself, untracked files included.

// A change is one changed file, written the way a phone reads a RemoteChange.
type change struct {
	Path    string `json:"path"`
	OldPath string `json:"oldPath,omitempty"`
	// modified, added, deleted or renamed.
	Status  string `json:"status"`
	Added   int    `json:"added"`
	Removed int    `json:"removed"`
	Binary  bool   `json:"binary"`
	hunks   []hunk
}

type hunk struct {
	oldStart, newStart int
	// Each with its sign first: "+", "-" or " ".
	lines []string
}

// git runs git in a folder. Longer than tmux gets, since a diff of a large repository takes a moment.
func git(dir string, args ...string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "git", append([]string{"-C", dir, "-c", "core.quotePath=false"}, args...)...).Output()
	return string(out), err
}

func gitLine(dir string, args ...string) (string, error) {
	out, err := git(dir, args...)
	return strings.TrimSpace(out), err
}

// baseBranch is the branch work gets merged into: what origin/HEAD points to, else a local main or master.
func baseBranch(dir string) string {
	if remote, err := gitLine(dir, "rev-parse", "--abbrev-ref", "origin/HEAD"); err == nil && remote != "origin/HEAD" {
		return remote
	}
	for _, name := range []string{"main", "master"} {
		if _, err := gitLine(dir, "rev-parse", "--verify", "--quiet", name); err == nil {
			return name
		}
	}
	return ""
}

// scope is the base to compare with, and how the phone says it. No base means uncommitted work: on the base branch itself,
// "since the branch left" means nothing.
func scope(dir string) (base, label string) {
	base = baseBranch(dir)
	branch, _ := gitLine(dir, "branch", "--show-current")
	if base == "" || branch == "" || branch == base || "origin/"+branch == base {
		return "", "Uncommitted"
	}
	return base, "Since " + base
}

// changes are the files changed against the base, or against HEAD without one, and the untracked files.
func changes(dir, base string) ([]change, error) {
	since := "HEAD"
	if base != "" {
		var err error
		if since, err = gitLine(dir, "merge-base", "HEAD", base); err != nil {
			return nil, err
		}
	}
	out, err := git(dir, "diff", "--no-color", "--no-ext-diff", "--find-renames", since)
	if err != nil {
		return nil, err
	}
	files := parseDiff(out)
	untracked, _ := git(dir, "ls-files", "--others", "--exclude-standard")
	// Capped so a folder full of untracked build output cannot stall it.
	for i, path := range strings.Split(strings.TrimSpace(untracked), "\n") {
		if path != "" && i < 200 {
			files = append(files, addedFile(dir, path))
		}
	}
	return files, nil
}

// addedFile is an untracked file, as a new file with every line added.
func addedFile(dir, path string) change {
	file := change{Path: path, Status: "added"}
	text, err := os.ReadFile(filepath.Join(dir, path))
	if err != nil || len(text) > 2<<20 || bytes.IndexByte(text, 0) >= 0 || !utf8.Valid(text) {
		file.Binary = true
		return file
	}
	lines := strings.Split(strings.TrimSuffix(string(text), "\n"), "\n")
	if len(text) == 0 {
		return file
	}
	added := hunk{oldStart: 0, newStart: 1}
	for _, line := range lines {
		added.lines = append(added.lines, "+"+line)
	}
	file.hunks, file.Added = []hunk{added}, len(lines)
	return file
}

// parseDiff reads `git diff`. Only what the phone shows: paths, status, and hunks.
func parseDiff(out string) []change {
	var files []change
	path := func(line, prefix string) string {
		// Git ends the name with a tab when it holds spaces.
		name, _, _ := strings.Cut(strings.TrimPrefix(line, prefix), "\t")
		return name
	}
	for _, line := range strings.Split(out, "\n") {
		last := len(files) - 1
		switch {
		case strings.HasPrefix(line, "diff --git "):
			// "a/x b/x": both halves are the same unless renamed, when the rename lines say which is which.
			rest := strings.TrimPrefix(line, "diff --git a/")
			files = append(files, change{Path: rest[:max(0, (len(rest)-3)/2)], Status: "modified"})
		case last < 0:
		case strings.HasPrefix(line, "new file mode"):
			files[last].Status = "added"
		case strings.HasPrefix(line, "deleted file mode"):
			files[last].Status = "deleted"
		case strings.HasPrefix(line, "rename from "):
			files[last].OldPath, files[last].Status = path(line, "rename from "), "renamed"
		case strings.HasPrefix(line, "rename to "):
			files[last].Path = path(line, "rename to ")
		case strings.HasPrefix(line, "+++ b/"):
			files[last].Path = path(line, "+++ b/")
		case strings.HasPrefix(line, "--- a/") && files[last].Status == "deleted":
			files[last].Path = path(line, "--- a/")
		case strings.HasPrefix(line, "Binary files "):
			files[last].Binary = true
		case strings.HasPrefix(line, "@@ "):
			if h, ok := parseHunk(line); ok {
				files[last].hunks = append(files[last].hunks, h)
			}
		case len(files[last].hunks) > 0 && line != "" && strings.ContainsRune("+- ", rune(line[0])):
			h := &files[last].hunks[len(files[last].hunks)-1]
			h.lines = append(h.lines, line)
			if line[0] == '+' {
				files[last].Added++
			} else if line[0] == '-' {
				files[last].Removed++
			}
		}
		// The rest, like "\ No newline at end of file", says nothing the phone shows.
	}
	return files
}

// parseHunk reads "@@ -12,7 +12,9 @@ func main()" as old start 12 and new start 12.
func parseHunk(line string) (hunk, bool) {
	parts := strings.Fields(line)
	if len(parts) < 3 {
		return hunk{}, false
	}
	old, _, _ := strings.Cut(strings.TrimPrefix(parts[1], "-"), ",")
	new, _, _ := strings.Cut(strings.TrimPrefix(parts[2], "+"), ",")
	o, err1 := strconv.Atoi(old)
	n, err2 := strconv.Atoi(new)
	return hunk{oldStart: o, newStart: n}, err1 == nil && err2 == nil
}

// unified is one file as the phone shows it, cut short so a message stays under the relay's limit.
func unified(file change) string {
	if file.Binary {
		return "Binary file"
	}
	var text strings.Builder
	for _, h := range file.hunks {
		text.WriteString("@@ -" + strconv.Itoa(h.oldStart) + " +" + strconv.Itoa(h.newStart) + " @@\n")
		for _, line := range h.lines {
			text.WriteString(line + "\n")
			if text.Len() > 120_000 {
				return text.String() + "… cut short, open it on the server for the rest"
			}
		}
	}
	return text.String()
}

// repository is where a session's changes are: its repository's top folder, and what they are compared with. False outside git.
func (h *host) repository(session string) (dir, base, label string, ok bool) {
	asked, known := h.terminal.agentIn(session)
	if !known {
		return "", "", "", false
	}
	dir = asked.folder
	if root, err := gitLine(dir, "rev-parse", "--show-toplevel"); err == nil {
		dir = root
	}
	base, label = scope(dir)
	return dir, base, label, true
}

func (h *host) changes(id uint32, p *phone, command, session string) {
	dir, base, label, ok := h.repository(session)
	if !ok {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	files, err := changes(dir, base)
	if err != nil {
		h.tell(id, p, refusedEvent(command, "This session is not in a git repository."))
		return
	}
	// Enough to review, and far under the relay's limit for one message.
	if len(files) > 1000 {
		files, label = files[:1000], label+", first 1000 files"
	}
	h.tell(id, p, changesEvent(session, label, files))
}

func (h *host) diff(id uint32, p *phone, command, session, path string) {
	dir, base, _, ok := h.repository(session)
	if !ok {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	files, _ := changes(dir, base)
	for _, file := range files {
		if file.Path == path {
			h.tell(id, p, diffEvent(session, path, unified(file)))
			return
		}
	}
	h.tell(id, p, refusedEvent(command, "That file has no changes any more."))
}
