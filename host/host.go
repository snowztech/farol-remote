package main

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/snowztech/farol-remote/host/agent"
)

// A host is this machine as a paired phone sees it: the part a Mac running Farol plays, with tmux in place of the app.
// It knows nothing of sockets. What the relay sent comes in through receive, and what to send goes out through send.
type host struct {
	mu       sync.Mutex
	key      deviceKey
	store    store
	terminal terminal
	// Sends one message to the relay. Set for as long as there is a connection.
	send   func([]byte)
	phones map[uint32]*phone
	// The commands each device already had carried out, kept across its connections, since a phone sends again on a new one.
	seen map[string]*seenCommands
	// The agents' logs, read where they were left.
	transcripts map[string]*agent.Transcript
	// The last paste from a phone into each session, until the Return that sends it.
	pasted map[string]paste
	// Pictures from phones. Made the first time one comes.
	pictures *inbox
	// The session each `open` made, so one a phone sends again after a dropped connection is answered and not opened twice.
	opened map[string]string
	// When phones were last sent what changed.
	looked time.Time
	// What each session was doing at the last look, to notify phones of a change. Nil before the first.
	statuses map[string]string
	// Phones sent a Live Activity start and not yet the started activity's address. Another start would only stack a second one.
	starting map[string]bool
}

type paste struct {
	text string
	at   time.Time
}

// One phone's connection, by the number the relay gave it.
type phone struct {
	channel *channel
	device  *device
	// Encrypted both ways.
	open bool
	// It said a version this host speaks, which is when it is told anything.
	greeted  bool
	watching string
	// What it was last sent, so only a change is sent again.
	sessions []byte
	screen   *string
	reading  *reading
	// It said when it shows a terminal, so its screen goes only then. An older phone never says, and always gets it.
	saysTerminal bool
	// The session whose terminal it shows.
	terminal string
	// What it was last told the watched session waits on. Nil until told, so it is always told once, even that it waits on nothing.
	request []byte
	// The session it was told is an empty conversation, an agent started there with no log yet, so it is told once.
	toldEmpty string
}

func newHost(key deviceKey, store store, terminal terminal) *host {
	return &host{key: key, store: store, terminal: terminal, send: func([]byte) {}, phones: map[uint32]*phone{}, seen: map[string]*seenCommands{},
		transcripts: map[string]*agent.Transcript{}, pasted: map[string]paste{}, opened: map[string]string{}, starting: map[string]bool{}}
}

// connected is called with each new connection to the relay, which numbers the phones again.
func (h *host) connected(send func([]byte)) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.send, h.phones = send, map[uint32]*phone{}
}

// receive takes one message from the relay.
func (h *host) receive(message []byte) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(message) < 5 {
		return
	}
	id := binary.BigEndian.Uint32(message[1:5])
	switch message[0] {
	case frameJoined:
		h.phones[id] = &phone{}
	case frameLeft:
		delete(h.phones, id)
	case frameData:
		var frame wireFrame
		if json.Unmarshal(message[5:], &frame) != nil {
			return
		}
		if h.phones[id] == nil {
			h.phones[id] = &phone{}
		}
		h.handle(id, h.phones[id], frame)
	}
}

func (h *host) handle(id uint32, p *phone, frame wireFrame) {
	switch {
	case frame.Pair != nil:
		paired, err := device{}, errOfferExpired
		if shown, ok := h.store.offer(); ok {
			paired, err = acceptPairing(frame.Pair.Value, shown, h.key, time.Now())
		}
		if err == nil {
			err = h.store.add(paired)
		}
		if err != nil {
			h.refuse(id, "This code is no longer valid. Run farol-host pair for a new one.")
			return
		}
		h.store.spendOffer()
		// Said every time, so a pairing you did not make does not go unseen.
		log.Printf("paired with %q, which can now see and type into your sessions", paired.Name)
		h.write(id, wireFrame{Paired: &struct{}{}})
	case frame.Hello != nil:
		known := find(h.store.devices(), frame.Hello.Value.Key)
		if known == nil {
			h.refuse(id, "This device is not paired with this machine.")
			return
		}
		ch := newChannel(h.key, "mac")
		encapsulated, err := ch.connect(frame.Hello.Value)
		if err != nil {
			h.refuse(id, "This device is not paired with this machine.")
			return
		}
		*p = phone{channel: ch, device: known}
		h.write(id, wireFrame{Hello: &one[channelHello]{ch.hello()}})
		h.write(id, wireFrame{Key: &one[[]byte]{encapsulated}})
	case frame.Key != nil:
		if p.channel == nil || p.channel.accept(frame.Key.Value) != nil {
			delete(h.phones, id)
			return
		}
		p.open = true
	case frame.Sealed != nil:
		if !p.open {
			delete(h.phones, id)
			return
		}
		plain, err := p.channel.open(frame.Sealed.Value)
		if err != nil {
			delete(h.phones, id)
			return
		}
		h.run(id, p, plain)
	default:
		// Something did not add up, and this connection is over.
		delete(h.phones, id)
	}
}

func find(devices []device, key []byte) *device {
	for i := range devices {
		if bytes.Equal(devices[i].Key, key) {
			return &devices[i]
		}
	}
	return nil
}

func (h *host) run(id uint32, p *phone, plain []byte) {
	var c command
	json.Unmarshal(plain, &c)
	a := c.Action
	switch {
	case a.Hello != nil:
		h.tell(id, p, helloEvent(hostName()))
		if a.Hello.Version != protocolVersion {
			delete(h.phones, id)
			return
		}
		p.greeted = true
		h.refresh()
	case !p.greeted:
		// Nothing is done for a phone before both sides know they speak the same version.
	case a.Watch != nil:
		p.watching, p.screen, p.reading, p.toldEmpty, p.request = a.Watch.Session, nil, nil, "", nil
		h.refresh()
	case a.Conversation != nil:
		h.conversation(id, p, c.ID, a.Conversation.Session, a.Conversation.Before)
	case a.Answer != nil:
		h.answer(id, p, c.ID, a.Answer.Session, a.Answer.Request, a.Answer.Choice)
	case a.Terminal != nil:
		p.saysTerminal, p.screen = true, nil
		if a.Terminal.Open {
			p.terminal = a.Terminal.Session
		} else if p.terminal == a.Terminal.Session {
			p.terminal = ""
		}
		h.refresh()
	case a.Control != nil:
		key := a.Control.Key
		if len(key) != 1 || key[0] < 'a' || key[0] > 'z' {
			h.tell(id, p, refusedEvent(c.ID, "Only Control with a letter can be sent."))
			return
		}
		h.act(id, p, c.ID, func() bool { return h.terminal.press(a.Control.Session, "C-"+key) })
	case a.Commands != nil:
		h.commands(id, p, c.ID, a.Commands.Session)
	case a.Attach != nil:
		h.attach(id, p, c.ID, a.Attach.Session, a.Attach.Attachment, a.Attach.Part, a.Attach.Parts, a.Attach.Data)
	case a.Notify != nil:
		n := a.Notify
		if err := h.store.savePushTarget(pushTarget{Device: p.device.Key, Token: n.Token, Sandbox: n.Sandbox, Notices: n.Notices, Key: n.Key, Starts: n.Starts}); err != nil {
			log.Printf("where to notify %q could not be kept: %v", p.device.Name, err)
		}
	case a.Activity != nil:
		h.activityStarted(p.device.Key, a.Activity.Token, a.Activity.Sandbox, h.terminal.sessions())
	case a.History != nil:
		if text, ok := h.terminal.history(a.History.Session); ok {
			h.tell(id, p, historyEvent(a.History.Session, lastLines(text, maxHistory)))
		}
	case a.Send != nil:
		h.act(id, p, c.ID, func() bool {
			h.pasted[a.Send.Session] = paste{a.Send.Text, time.Now()}
			return h.terminal.paste(a.Send.Session, a.Send.Text)
		})
	case a.Type != nil:
		h.act(id, p, c.ID, func() bool { return h.terminal.keys(a.Type.Session, a.Type.Text) })
	case a.Key != nil:
		if _, known := tmuxKeys[a.Key.Key]; !known {
			h.tell(id, p, refusedEvent(c.ID, "This machine does not know that key. Update farol-host on it."))
			return
		}
		h.act(id, p, c.ID, func() bool { return h.press(a.Key.Session, a.Key.Key) })
	case a.Options != nil:
		h.options(id, p)
	case a.Open != nil:
		h.open(id, p, c.ID, a.Open.Project, a.Open.Agent)
	case a.Close != nil:
		h.close(id, p, c.ID, a.Close.Session)
	case a.Changes != nil:
		h.changes(id, p, c.ID, a.Changes.Session)
	case a.Diff != nil:
		h.diff(id, p, c.ID, a.Diff.Session, a.Diff.Path)
	case a.Forget != nil:
		if err := h.store.forget(p.device.Key); err != nil {
			log.Printf("%q could not be forgotten: %v", p.device.Name, err)
			h.tell(id, p, refusedEvent(c.ID, "This machine could not forget this phone. Run farol-host forget on it."))
			return
		}
		h.tell(id, p, doneEvent(c.ID))
		delete(h.phones, id)
	case a.Task != nil:
		h.tell(id, p, refusedEvent(c.ID, "This machine cannot start tasks from the phone yet."))
	case c.ID != "":
		// One that opens and does not read is from a newer phone. It is told so, so it does not wait for an answer.
		h.tell(id, p, refusedEvent(c.ID, "This machine does not know how to do that. Update farol-host on it."))
	}
}

// act types into a session once, however many times the command arrives.
func (h *host) act(id uint32, p *phone, command string, do func() bool) {
	if h.seenBy(p).insert(command) && !do() {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	h.tell(id, p, doneEvent(command))
	h.refresh()
}

// seenBy is what the phone's device already had carried out.
func (h *host) seenBy(p *phone) *seenCommands {
	seen := h.seen[string(p.device.Key)]
	if seen == nil {
		seen = &seenCommands{ids: map[string]bool{}}
		h.seen[string(p.device.Key)] = seen
	}
	return seen
}

// press presses a key. Return right after a paste waits for the agent to take the paste in, or it is lost and the reply stays unsent.
func (h *host) press(session, key string) bool {
	pasted, ok := h.pasted[session]
	if key != "enter" || !ok {
		return h.terminal.press(session, key)
	}
	delete(h.pasted, session)
	time.AfterFunc(returnDelay(pasted.text, time.Since(pasted.at)), func() {
		h.mu.Lock()
		defer h.mu.Unlock()
		h.terminal.press(session, key)
	})
	return true
}

// returnDelay is how long Return waits after a paste. An agent takes a paste in as a whole and opens any picture named in it,
// and a Return that comes meanwhile is lost. More lines take longer, up to a few screens' worth, and a picture much longer.
func returnDelay(text string, elapsed time.Duration) time.Duration {
	lines := strings.Split(text, "\n")
	pictures := 0
	for _, line := range lines {
		if strings.Contains(line, "/Farol/Attachments/") {
			pictures++
		}
	}
	settle := 250*time.Millisecond + time.Duration(min(len(lines), 8))*150*time.Millisecond + time.Duration(min(pictures, 4))*600*time.Millisecond
	return max(0, settle-elapsed)
}

// due says when to look again: often while a phone shows a terminal, so typing there feels live, and less the rest of the time.
func (h *host) due(now time.Time) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	// Without a phone it only looks for a session to notify of.
	every := time.Second
	if len(h.phones) > 0 {
		every = 400 * time.Millisecond
	}
	for _, p := range h.phones {
		if p.terminal != "" {
			every = 120 * time.Millisecond
		}
	}
	if now.Sub(h.looked) < every {
		return false
	}
	h.looked = now
	return true
}

// tick looks again, as often as due says. Without a phone connected, only for a notification to send.
func (h *host) tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.phones) == 0 {
		h.notice(h.terminal.sessions())
		return
	}
	// A phone removed with `farol-host forget` is cut off at once, and not at its next connection.
	devices := h.store.devices()
	for id, p := range h.phones {
		if p.device != nil && find(devices, p.device.Key) == nil {
			delete(h.phones, id)
		}
	}
	h.refresh()
}

// refresh sends each phone what changed: the list, and the conversation and screen of the session it has open.
// The list and the screen go in full, so a phone that missed one has nothing to replay.
func (h *host) refresh() {
	var list []byte
	for id, p := range h.phones {
		if !p.greeted {
			continue
		}
		if list == nil {
			sessions := h.terminal.sessions()
			h.notice(sessions)
			list = sessionsEvent(sessions)
		}
		if !bytes.Equal(list, p.sessions) {
			p.sessions = list
			h.tell(id, p, list)
		}
		if p.watching == "" {
			continue
		}
		h.follow(id, p)
		h.tellRequest(id, p)
		// A phone reading the conversation needs the screen only for a question the hooks did not describe.
		if asked, _ := h.terminal.agentIn(p.watching); p.saysTerminal && p.terminal != p.watching && asked.status != "waiting" {
			continue
		}
		text, ok := h.terminal.screen(p.watching)
		if !ok || (p.screen != nil && *p.screen == text) {
			continue
		}
		p.screen = &text
		h.tell(id, p, screenEvent(p.watching, text))
	}
}

func (h *host) tell(id uint32, p *phone, event []byte) {
	sealed, err := p.channel.seal(event)
	if err != nil {
		return
	}
	h.write(id, wireFrame{Sealed: &one[[]byte]{sealed}})
}

func (h *host) write(id uint32, frame wireFrame) {
	payload, _ := json.Marshal(frame)
	// The relay ends a connection that sends it more, which would cut off every phone over one message.
	if len(toPhone(id, nil))+len(payload) > maxMessage {
		log.Printf("a message of %d bytes was left unsent, over the relay's limit", len(payload))
		return
	}
	h.send(toPhone(id, payload))
}

// refuse says no in words for the person holding the phone, and ends the connection.
func (h *host) refuse(id uint32, why string) {
	h.write(id, wireFrame{Refused: &one[string]{why}})
	delete(h.phones, id)
}

// The relay passes messages up to 256 KB. Sealed and written as text, this much history stays under that.
const maxHistory = 120 << 10

// lastLines keeps the end of a text, from the start of a line, when the whole of it is too much to send.
func lastLines(text string, limit int) string {
	if len(text) <= limit {
		return text
	}
	text = text[len(text)-limit:]
	_, rest, _ := strings.Cut(text, "\n")
	return rest
}

// seenCommands are the commands already carried out, so one that arrives twice is typed once.
// A phone resends after a dropped connection, and only the last few can still be in doubt.
type seenCommands struct {
	ids   map[string]bool
	order []string
}

// insert is true the first time an id is seen.
func (s *seenCommands) insert(id string) bool {
	if s.ids[id] {
		return false
	}
	s.ids[id] = true
	if s.order = append(s.order, id); len(s.order) > 256 {
		delete(s.ids, s.order[0])
		s.order = s.order[1:]
	}
	return true
}

// hostName is what a phone calls this machine: its hostname, or "Server" when it has none.
func hostName() string {
	if name, err := os.Hostname(); err == nil && name != "" {
		return name
	}
	return "Server"
}
