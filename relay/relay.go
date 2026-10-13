package main

import (
	"context"
	"encoding/binary"
	"log"
	"net/http"
	"regexp"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type room struct {
	mac    *peer
	phones map[uint32]*peer
	next   uint32
}

type relay struct {
	mu    sync.Mutex
	rooms map[string]*room
	// Nil when the relay has no key from Apple, and then it sends no notifications.
	push *pusher
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

	pushes, since := 0, time.Now()
	mac.read(ctx, func(message []byte) {
		if len(message) > 1 && message[0] == framePush && r.push != nil {
			if time.Since(since) > time.Minute {
				pushes, since = 0, time.Now()
			}
			if pushes++; pushes > pushesPerMinute {
				return
			}
			// Apple can be slow, and the Mac's other messages must not wait for it.
			go func(payload []byte) {
				if err := r.push.send(context.Background(), payload); err != nil {
					log.Printf("notification not sent: %v", err)
				}
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
