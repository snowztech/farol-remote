package main

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/snowztech/farol-remote/host/agent"
)

// New sessions from the phone, as Farol on a Mac opens a tab. The phone picks a project and an agent from this host's own lists,
// by id, so it never names a folder or sends a command to run.

// A choice is something the phone picks from, written the way it reads a RemoteChoice.
type choice struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// projects are the folders sessions are open in, their repository's when they have one, then the home folder.
// Home is there so a phone can open the first session when there is none.
func (h *host) projects() []choice {
	home, _ := os.UserHomeDir()
	var folders []string
	for _, s := range h.terminal.sessions() {
		asked, _ := h.terminal.agentIn(s.ID)
		if asked.folder == "" {
			continue
		}
		folder := asked.folder
		if root, err := run("", "git", "-C", folder, "rev-parse", "--show-toplevel"); err == nil {
			folder = strings.TrimSpace(root)
		}
		folders = append(folders, folder)
	}
	var list []choice
	for _, folder := range append(folders, home) {
		if folder == "" || slices.ContainsFunc(list, func(c choice) bool { return c.ID == folder }) {
			continue
		}
		name := filepath.Base(folder)
		if folder == home {
			name = "Home"
		}
		list = append(list, choice{folder, name})
	}
	return list
}

// agentFolders are the agents a session can start with. A folder that is not there is an agent never run on this machine.
func agentFolders() []agent.Folder {
	home, _ := os.UserHomeDir()
	return slices.DeleteFunc(agent.Folders(home, os.Getenv), func(f agent.Folder) bool {
		_, err := os.Stat(f.Dir)
		return err != nil
	})
}

func (h *host) options(id uint32, p *phone) {
	agents := []choice{}
	for _, f := range agentFolders() {
		agents = append(agents, choice{f.Dir, f.Label()})
	}
	h.tell(id, p, optionsEvent(h.projects(), agents))
}

// open makes a new session in a project from the list, running the agent with that id from the list, or a plain shell without one.
func (h *host) open(id uint32, p *phone, command, project string, agentID *string) {
	if made, ok := h.opened[command]; ok {
		h.tell(id, p, openedEvent(command, made))
		return
	}
	run := ""
	if agentID != nil {
		i := slices.IndexFunc(agentFolders(), func(f agent.Folder) bool { return f.Dir == *agentID })
		if i < 0 {
			h.tell(id, p, refusedEvent(command, "This machine could not open that session. Pick the agent again and retry."))
			return
		}
		run = agentFolders()[i].Command()
	}
	listed := slices.ContainsFunc(h.projects(), func(c choice) bool { return c.ID == project })
	made, ok := "", false
	if listed {
		made, ok = h.terminal.open(project, sessionName(project), run)
	}
	if !ok {
		h.tell(id, p, refusedEvent(command, "This machine could not open that session. Pick the project again and retry."))
		return
	}
	h.opened[command] = made
	h.tell(id, p, openedEvent(command, made))
	h.refresh()
}

// close closes a session, once, however many times the command arrives. One already gone is closed as far as the phone cares.
func (h *host) close(id uint32, p *phone, command, session string) {
	if h.seenBy(p).insert(command) {
		h.terminal.close(session)
	}
	h.tell(id, p, doneEvent(command))
	h.refresh()
}

// tmux turns a dot or a colon in a name into something else, and those name its targets, so a session is named without them.
var unnamed = regexp.MustCompile(`[^A-Za-z0-9_-]+`)

func sessionName(project string) string {
	if name := strings.Trim(unnamed.ReplaceAllString(filepath.Base(project), "-"), "-"); name != "" {
		return name
	}
	return "session"
}
