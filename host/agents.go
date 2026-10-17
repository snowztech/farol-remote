package main

import (
	"bytes"
	"errors"
	"os"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// What a phone reads of an agent and answers it: its conversation, from the log its hooks named, and what it asks.
// Done as Farol on a Mac does it, in RemoteBridge.

// The conversation a phone reads, and the last message it has, so only what follows is sent.
type reading struct {
	session, log, last string
}

// conversation answers a phone that asks for a session's conversation: the latest page, which it then follows while it watches, or a page further back.
func (h *host) conversation(id uint32, p *phone, command, session string, before *string) {
	asked, _ := h.terminal.agentIn(session)
	log, started := asked.log, asked.started
	switch {
	case log == "" && started && before == nil:
		// An agent that has not said anything yet has no log, and its conversation is empty rather than missing.
		p.toldEmpty = session
		h.tell(id, p, conversationEvent(session, nil, latest, false))
	case log == "":
		h.tell(id, p, refusedEvent(command, "This session has no agent conversation to show."))
	case before == nil:
		h.open(id, p, session, log)
	default:
		messages := h.messages(log)
		end := slices.IndexFunc(messages, func(m agent.Message) bool { return m.ID == *before })
		if end < 0 {
			h.tell(id, p, refusedEvent(command, "That part of the conversation was rewound."))
			return
		}
		start := pageStart(messages, end)
		h.tell(id, p, conversationEvent(session, messages[start:end], placeBefore(*before), start > 0))
	}
}

func (h *host) open(id uint32, p *phone, session, log string) {
	p.reading = &reading{session: session, log: log}
	h.sendLatest(id, p, h.messages(log))
}

func (h *host) sendLatest(id uint32, p *phone, messages []agent.Message) {
	start := pageStart(messages, len(messages))
	p.reading.last = lastID(messages)
	h.tell(id, p, conversationEvent(p.reading.session, messages[start:], latest, start > 0))
}

// follow sends a phone what the agent in the session it watches wrote since it last heard.
// The log is known only once a hook names it, and the phone shows the terminal until then, so it gets the conversation as soon as there is one.
func (h *host) follow(id uint32, p *phone) {
	asked, _ := h.terminal.agentIn(p.watching)
	log, started := asked.log, asked.started
	switch r := p.reading; {
	case r != nil && r.session == p.watching && r.log == log:
		messages := h.messages(log)
		last := lastID(messages)
		if last == r.last {
			return
		}
		// After a rewind, or more than a page, it gets the latest page instead.
		if i := slices.IndexFunc(messages, func(m agent.Message) bool { return m.ID == r.last }); r.last != "" && i >= 0 && pageStart(messages, len(messages)) <= i+1 {
			h.tell(id, p, conversationEvent(r.session, messages[i+1:], placeAfter(r.last), false))
			r.last = last
		} else {
			h.sendLatest(id, p, messages)
		}
	case log != "":
		// The first log named, or a new one: Claude Code starts one after /clear.
		h.open(id, p, p.watching, log)
	case started && p.toldEmpty != p.watching:
		// An agent just started, with nothing said: the phone leaves the terminal for its empty conversation.
		p.reading, p.toldEmpty = nil, p.watching
		h.tell(id, p, conversationEvent(p.watching, nil, latest, false))
	}
}

// messages reads what the agent added to a log since the last look. A few logs stay read, so opening a session again is quick.
// ponytail: the first read of a long log holds the host for as long as it takes, a second or so for a very long session. Read it aside if that shows.
func (h *host) messages(log string) []agent.Message {
	transcript := h.transcripts[log]
	if transcript == nil {
		if len(h.transcripts) >= 8 {
			h.forgetTranscripts()
		}
		transcript = &agent.Transcript{Path: log}
		h.transcripts[log] = transcript
	}
	transcript.Refresh()
	return transcript.Messages()
}

// forgetTranscripts keeps only the logs a phone reads now.
func (h *host) forgetTranscripts() {
	for log := range h.transcripts {
		read := false
		for _, p := range h.phones {
			read = read || (p.reading != nil && p.reading.log == log)
		}
		if !read {
			delete(h.transcripts, log)
		}
	}
}

func lastID(messages []agent.Message) string {
	if len(messages) == 0 {
		return ""
	}
	return messages[len(messages)-1].ID
}

// pageStart is where a page ending at end starts: up to 60 messages, and well under the relay's 256 KB a message.
func pageStart(messages []agent.Message, end int) int {
	start, size := end, 0
	for start > 0 && end-start < 60 {
		message := messages[start-1]
		size += len(message.Text) + len(message.Detail) + 100
		if size > 150_000 && start < end {
			break
		}
		start--
	}
	return start
}

// tellRequest tells a phone what the session it watches waits on, when that changed. Nothing is a change too, so the phone stops showing what was asked.
func (h *host) tellRequest(id uint32, p *phone) {
	asked, _ := h.terminal.agentIn(p.watching)
	if event := requestEvent(p.watching, asked.request); !bytes.Equal(event, p.request) {
		p.request = event
		h.tell(id, p, event)
	}
}

// answer types the number of an answer, once, and only while the agent still asks what the phone answered.
func (h *host) answer(id uint32, p *phone, command, session, request string, choice int) {
	if !h.seenBy(p).insert(command) {
		h.tell(id, p, doneEvent(command))
		return
	}
	asked, _ := h.terminal.agentIn(session)
	if asked.request == nil || asked.request.ID != request || asked.status != "waiting" {
		h.tell(id, p, refusedEvent(command, "That was already answered."))
		return
	}
	// Gone at once, so a second tap is refused rather than typed into whatever the agent asks next.
	h.terminal.answered(session)
	h.terminal.keys(session, strconv.Itoa(choice))
	h.tell(id, p, doneEvent(command))
	h.refresh()
}

// commands tells a phone what the agent in a session takes after a slash, from its config folder and the session's project.
func (h *host) commands(id uint32, p *phone, command, session string) {
	asked, ok := h.terminal.agentIn(session)
	if !ok {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	project := asked.folder
	if root, err := run("", "git", "-C", asked.folder, "rev-parse", "--show-toplevel"); err == nil {
		project = strings.TrimSpace(root)
	}
	home, _ := os.UserHomeDir()
	h.tell(id, p, commandsEvent(session, agent.Commands(asked.log, project, home)))
}

// attach takes one part of a picture. Each is answered, so the phone sends the next, and the last with where the picture is.
func (h *host) attach(id uint32, p *phone, command, session, attachment string, part, parts int, data []byte) {
	if _, ok := h.terminal.agentIn(session); !ok {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	if h.pictures == nil {
		h.pictures = newInbox()
	}
	file, err := h.pictures.take(attachment, part, parts, data, time.Now())
	switch {
	case errors.Is(err, errNotAnImage):
		h.tell(id, p, refusedEvent(command, "Only a photo or a screenshot can be attached."))
	case errors.Is(err, errTooLarge):
		h.tell(id, p, refusedEvent(command, "That picture is too large to attach."))
	case err != nil:
		h.tell(id, p, refusedEvent(command, "This machine could not keep that picture. Attach it again."))
	case file == "":
		h.tell(id, p, doneEvent(command))
	default:
		h.tell(id, p, attachedEvent(command, attachment, file))
	}
}
