package main

import (
	"context"
	"time"

	"github.com/coder/websocket"
)

const (
	// A screen of text is a few kilobytes, so this is generous and still small.
	maxMessage = 256 << 10
	maxPhones  = 8
	// What one connection may send, so the relay is no use as a free tunnel.
	bytesPerSecond = 512 << 10
	burstBytes     = 2 << 20
	// A peer too slow to take this many messages is cut off, not waited for.
	queued = 64
	// How many notifications one Mac may ask for in a minute. Agents do not wait or finish faster than this.
	pushesPerMinute = 20
	pingEvery       = 30 * time.Second
	writeTimeout    = 10 * time.Second
)

type peer struct {
	conn *websocket.Conn
	out  chan []byte
	// Closed with the connection, so whoever queues for it stops.
	gone   chan struct{}
	cancel context.CancelFunc
}

// send queues a message, and drops the peer when it cannot keep up.
func (p *peer) send(message []byte) {
	select {
	case p.out <- message:
	case <-p.gone:
	default:
		p.cancel()
	}
}

// write is the only place that writes to the connection, and keeps it alive with pings.
func (p *peer) write(ctx context.Context) {
	ping := time.NewTicker(pingEvery)
	defer ping.Stop()
	for {
		var err error
		select {
		case <-ctx.Done():
			return
		case message := <-p.out:
			deadline, done := context.WithTimeout(ctx, writeTimeout)
			err = p.conn.Write(deadline, websocket.MessageBinary, message)
			done()
		case <-ping.C:
			deadline, done := context.WithTimeout(ctx, writeTimeout)
			err = p.conn.Ping(deadline)
			done()
		}
		if err != nil {
			p.cancel()
			return
		}
	}
}

// read hands each message to handle until the connection ends or sends more than it may.
func (p *peer) read(ctx context.Context, handle func([]byte)) {
	allowance, last := float64(burstBytes), time.Now()
	for {
		_, message, err := p.conn.Read(ctx)
		if err != nil {
			return
		}
		now := time.Now()
		allowance = min(burstBytes, allowance+now.Sub(last).Seconds()*bytesPerSecond) - float64(len(message))
		last = now
		if allowance < 0 {
			p.conn.Close(websocket.StatusPolicyViolation, "too much, too fast")
			return
		}
		handle(message)
	}
}
