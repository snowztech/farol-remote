package main

import (
	"bytes"
	"context"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
)

const someRoom = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

type client struct {
	t    *testing.T
	conn *websocket.Conn
}

func serve(t *testing.T) string {
	t.Helper()
	server := httptest.NewServer(newRelay(nil).handler())
	t.Cleanup(server.Close)
	return "ws" + strings.TrimPrefix(server.URL, "http")
}

func dial(t *testing.T, base, side string) *client {
	t.Helper()
	conn, _, err := websocket.Dial(context.Background(), base+"/v1/rooms/"+someRoom+"/"+side, nil)
	if err != nil {
		t.Fatalf("dial %s: %v", side, err)
	}
	t.Cleanup(func() { conn.CloseNow() })
	return &client{t, conn}
}

func (c *client) send(message []byte) {
	c.t.Helper()
	if err := c.conn.Write(context.Background(), websocket.MessageBinary, message); err != nil {
		c.t.Fatalf("write: %v", err)
	}
}

func (c *client) expect(want []byte) {
	c.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, got, err := c.conn.Read(ctx)
	if err != nil {
		c.t.Fatalf("read: %v, wanted %v", err, want)
	}
	if !bytes.Equal(got, want) {
		c.t.Fatalf("got %v, wanted %v", got, want)
	}
}

func TestPassesBytesBothWays(t *testing.T) {
	base := serve(t)
	mac := dial(t, base, "mac")
	phone := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 1, nil))
	phone.expect([]byte{frameOnline})

	phone.send(append([]byte{frameData}, "sealed from phone"...))
	mac.expect(numbered(frameData, 1, []byte("sealed from phone")))

	mac.send(numbered(frameData, 1, []byte("sealed from mac")))
	phone.expect(append([]byte{frameData}, "sealed from mac"...))
}

func TestAPhoneOnlyGetsWhatIsMeantForIt(t *testing.T) {
	base := serve(t)
	mac := dial(t, base, "mac")
	first := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 1, nil))
	first.expect([]byte{frameOnline})
	second := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 2, nil))
	second.expect([]byte{frameOnline})

	mac.send(numbered(frameData, 2, []byte("for the second")))
	mac.send(numbered(frameData, 1, []byte("for the first")))
	second.expect(append([]byte{frameData}, "for the second"...))
	first.expect(append([]byte{frameData}, "for the first"...))
}

func TestAPhoneLearnsWhenItsMacComesAndGoes(t *testing.T) {
	base := serve(t)
	phone := dial(t, base, "phone")
	phone.expect([]byte{frameOffline})

	mac := dial(t, base, "mac")
	mac.expect(numbered(frameJoined, 1, nil))
	phone.expect([]byte{frameOnline})

	mac.conn.Close(websocket.StatusNormalClosure, "")
	phone.expect([]byte{frameOffline})
}

func TestTheMacLearnsWhenAPhoneGoes(t *testing.T) {
	base := serve(t)
	mac := dial(t, base, "mac")
	phone := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 1, nil))
	phone.conn.Close(websocket.StatusNormalClosure, "")
	mac.expect(numbered(frameLeft, 1, nil))
}

func TestAMessageThatIsTooBigEndsTheConnection(t *testing.T) {
	base := serve(t)
	mac := dial(t, base, "mac")
	phone := dial(t, base, "phone")
	mac.expect(numbered(frameJoined, 1, nil))
	phone.send(make([]byte, maxMessage+1))
	mac.expect(numbered(frameLeft, 1, nil))
}

func TestOnlyRoomsAndSidesThatExist(t *testing.T) {
	base := serve(t)
	for _, path := range []string{"/v1/rooms/short/mac", "/v1/rooms/" + someRoom + "/laptop"} {
		if _, _, err := websocket.Dial(context.Background(), base+path, nil); err == nil {
			t.Fatalf("%s was accepted", path)
		}
	}
}

func TestOneAddressCannotFillTheRelay(t *testing.T) {
	r := newRelay(nil)
	r.door.maxEach = 2
	server := httptest.NewServer(r.handler())
	t.Cleanup(server.Close)
	base := "ws" + strings.TrimPrefix(server.URL, "http")
	dial(t, base, "phone")
	second := dial(t, base, "phone")
	if _, _, err := websocket.Dial(context.Background(), base+"/v1/rooms/"+someRoom+"/phone", nil); err == nil {
		t.Fatal("a third connection from the same address was let in")
	}
	// A place frees up when someone leaves.
	second.conn.Close(websocket.StatusNormalClosure, "")
	for range 50 {
		if r.door.count() == 1 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	dial(t, base, "phone")
}

func TestCountsTheAddressAProxyPassesOn(t *testing.T) {
	req := httptest.NewRequest("GET", "/", nil)
	req.RemoteAddr = "10.0.0.1:4000"
	if got := address(req); got != "10.0.0.1" {
		t.Fatalf("got %q", got)
	}
	req.Header.Set("X-Forwarded-For", "203.0.113.7, 10.0.0.1")
	if got := address(req); got != "203.0.113.7" {
		t.Fatalf("got %q", got)
	}
}
