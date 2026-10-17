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
}

func newHost(key deviceKey, store store, terminal terminal) *host {
	return &host{key: key, store: store, terminal: terminal, send: func([]byte) {}, phones: map[uint32]*phone{}, seen: map[string]*seenCommands{}}
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
		p.watching, p.screen = a.Watch.Session, nil
		h.refresh()
	case a.Notify != nil:
		// Where to notify this phone, which this host does not do yet.
	case a.History != nil:
		if text, ok := h.terminal.history(a.History.Session); ok {
			h.tell(id, p, historyEvent(a.History.Session, lastLines(text, maxHistory)))
		}
	case a.Send != nil:
		h.act(id, p, c.ID, func() bool { return h.terminal.paste(a.Send.Session, a.Send.Text) })
	case a.Type != nil:
		h.act(id, p, c.ID, func() bool { return h.terminal.keys(a.Type.Session, a.Type.Text) })
	case a.Key != nil:
		h.act(id, p, c.ID, func() bool { return h.terminal.press(a.Key.Session, a.Key.Key) })
	case a.Options != nil || a.Task != nil:
		h.tell(id, p, refusedEvent(c.ID, "This machine cannot start tasks from the phone yet."))
	case c.ID != "":
		// One that opens and does not read is from a newer phone. It is told so, so it does not wait for an answer.
		h.tell(id, p, refusedEvent(c.ID, "This machine does not know how to do that. Update farol-host on it."))
	}
}

// act types into a session once, however many times the command arrives.
func (h *host) act(id uint32, p *phone, command string, do func() bool) {
	seen := h.seen[string(p.device.Key)]
	if seen == nil {
		seen = &seenCommands{ids: map[string]bool{}}
		h.seen[string(p.device.Key)] = seen
	}
	if seen.insert(command) && !do() {
		h.tell(id, p, refusedEvent(command, "That session is closed."))
		return
	}
	h.tell(id, p, doneEvent(command))
	h.refresh()
}

// tick is called a few times a second. It does nothing while no phone is connected.
func (h *host) tick() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if len(h.phones) == 0 {
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

// refresh sends each phone what changed: the list, and the screen of the session it has open. Always in full.
func (h *host) refresh() {
	var list []byte
	for id, p := range h.phones {
		if !p.greeted {
			continue
		}
		if list == nil {
			list = sessionsEvent(h.terminal.sessions())
		}
		if !bytes.Equal(list, p.sessions) {
			p.sessions = list
			h.tell(id, p, list)
		}
		if p.watching == "" {
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
