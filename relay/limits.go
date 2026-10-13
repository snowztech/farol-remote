package main

import (
	"net"
	"net/http"
	"strings"
	"sync"
)

const (
	// More than a relay this size can serve well. Past it, new connections are turned away so the ones inside keep working.
	maxConnections = 5000
	// A Mac and a few phones behind one address is normal. Hundreds from one address is someone filling the relay.
	maxPerAddress = 40
)

// A door counts who is connected, in total and from each address, and refuses past the limits.
type door struct {
	mu         sync.Mutex
	total      int
	perAddress map[string]int
	maxTotal   int
	maxEach    int
}

func newDoor() *door {
	return &door{perAddress: map[string]int{}, maxTotal: maxConnections, maxEach: maxPerAddress}
}

func (d *door) enter(address string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.total >= d.maxTotal || d.perAddress[address] >= d.maxEach {
		return false
	}
	d.total++
	d.perAddress[address]++
	return true
}

func (d *door) leave(address string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.total--
	if d.perAddress[address]--; d.perAddress[address] <= 0 {
		delete(d.perAddress, address)
	}
}

func (d *door) count() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.total
}

// address is where a request came from. Behind a host's proxy that is the first address the proxy passes on.
// It is only counted, never logged.
func address(req *http.Request) string {
	if forwarded := req.Header.Get("X-Forwarded-For"); forwarded != "" {
		first, _, _ := strings.Cut(forwarded, ",")
		return strings.TrimSpace(first)
	}
	host, _, err := net.SplitHostPort(req.RemoteAddr)
	if err != nil {
		return req.RemoteAddr
	}
	return host
}
