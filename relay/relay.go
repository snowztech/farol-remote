package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"net/http"
	"regexp"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type room struct {
	mac    *peer
	phones map[uint32]*peer
	next   uint32
}

type pushSender interface {
	send(context.Context, []byte, string) error
}

type relay struct {
	mu    sync.Mutex
	rooms map[string]*room
	// Nil when neither notification provider is configured.
	push pushSender
	door *door
	// Counted for the log, which says how much the relay does and never for whom.
	sent, failed atomic.Int64
}

func newRelay(push pushSender) *relay {
	return &relay{rooms: map[string]*room{}, push: push, door: newDoor()}
}

// A room is named by the hash of its Mac's public key, which only that Mac and its phones know.
var roomName = regexp.MustCompile(`^[0-9a-f]{64}$`)

func (r *relay) handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { w.Write([]byte("ok\n")) })
	mux.HandleFunc("GET /v1/rooms/{room}/{side}", r.join)
	return mux
}

func (r *relay) join(w http.ResponseWriter, req *http.Request) {
	name, side := req.PathValue("room"), req.PathValue("side")
	if !roomName.MatchString(name) || (side != "mac" && side != "phone") {
		http.NotFound(w, req)
		return
	}
	from := address(req)
	if !r.door.enter(from) {
		http.Error(w, "the relay is full, try again later", http.StatusServiceUnavailable)
		return
	}
	defer r.door.leave(from)
	conn, err := websocket.Accept(w, req, nil)
	if err != nil {
		return
	}
	conn.SetReadLimit(maxMessage)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	p := &peer{conn: conn, out: make(chan []byte, queued), gone: make(chan struct{}), cancel: cancel}
	defer close(p.gone)
	go p.write(ctx)

	if side == "mac" {
		r.serveMac(ctx, name, p)
	} else {
		r.servePhone(ctx, name, p)
	}
	conn.Close(websocket.StatusNormalClosure, "")
}

func (r *relay) serveMac(ctx context.Context, name string, mac *peer) {
	r.mu.Lock()
	rm := r.room(name)
	// The same Mac coming back on another network takes over from its old connection.
	if rm.mac != nil {
		rm.mac.cancel()
	}
	rm.mac = mac
	for id, phone := range rm.phones {
		mac.send(numbered(frameJoined, id, nil))
		phone.send([]byte{frameOnline})
	}
	r.mu.Unlock()

	apnsPushes, fcmPushes, since := 0, 0, time.Now()
	mac.read(ctx, func(message []byte) {
		if len(message) > 1 && message[0] == framePush && r.push != nil {
			if time.Since(since) > time.Minute {
				apnsPushes, fcmPushes, since = 0, 0, time.Now()
			}
			_, _, android, _ := classifyPush(message[1:])
			if android {
				fcmPushes++
				if fcmPushes > pushesPerMinute {
					return
				}
			} else {
				apnsPushes++
				if apnsPushes > pushesPerMinute {
					return
				}
			}
			// Apple can be slow, and the Mac's other messages must not wait for it.
			go func(payload []byte) {
				if err := r.push.send(context.Background(), payload, name); err != nil {
					r.failed.Add(1)
					log.Printf("notification not sent: %v", err)
					return
				}
				r.sent.Add(1)
			}(message[1:])
			return
		}
		if len(message) < 5 || message[0] != frameData {
			return
		}
		r.mu.Lock()
		phone := rm.phones[binary.BigEndian.Uint32(message[1:5])]
		r.mu.Unlock()
		if phone != nil {
			phone.send(append([]byte{frameData}, message[5:]...))
		}
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	if rm.mac != mac {
		return
	}
	rm.mac = nil
	for _, phone := range rm.phones {
		phone.send([]byte{frameOffline})
	}
	r.forget(name, rm)
}

func (r *relay) servePhone(ctx context.Context, name string, phone *peer) {
	r.mu.Lock()
	rm := r.room(name)
	if len(rm.phones) >= maxPhones {
		r.mu.Unlock()
		phone.conn.Close(websocket.StatusPolicyViolation, "too many devices")
		return
	}
	rm.next++
	id := rm.next
	rm.phones[id] = phone
	if rm.mac != nil {
		rm.mac.send(numbered(frameJoined, id, nil))
		phone.send([]byte{frameOnline})
	} else {
		phone.send([]byte{frameOffline})
	}
	r.mu.Unlock()

	phone.read(ctx, func(message []byte) {
		if len(message) < 1 || message[0] != frameData {
			return
		}
		r.mu.Lock()
		mac := rm.mac
		r.mu.Unlock()
		if mac != nil {
			mac.send(numbered(frameData, id, message[1:]))
		}
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	delete(rm.phones, id)
	if rm.mac != nil {
		rm.mac.send(numbered(frameLeft, id, nil))
	}
	r.forget(name, rm)
}

// room is called with the lock held.
func (r *relay) room(name string) *room {
	rm := r.rooms[name]
	if rm == nil {
		rm = &room{phones: map[uint32]*peer{}}
		r.rooms[name] = rm
	}
	return rm
}

// forget is called with the lock held, and drops a room nobody is in.
func (r *relay) forget(name string, rm *room) {
	if rm.mac == nil && len(rm.phones) == 0 && r.rooms[name] == rm {
		delete(r.rooms, name)
	}
}

// report writes how busy the relay is, every so often and only when that changed.
// Counts are all it says: no room, no address, no token ever reaches the log.
func (r *relay) report(every time.Duration) {
	last := ""
	for range time.Tick(every) {
		r.mu.Lock()
		rooms := len(r.rooms)
		r.mu.Unlock()
		now := fmt.Sprintf("%d rooms, %d connections, %d notifications sent, %d not sent", rooms, r.door.count(), r.sent.Load(), r.failed.Load())
		if now != last {
			log.Print(now)
			last = now
		}
	}
}
