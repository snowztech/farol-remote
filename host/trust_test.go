package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"strings"
	"testing"
)

func TestTrustLetsAMacInAndSaysWhereThisHostIs(t *testing.T) {
	t.Setenv("FAROL_HOST_DIR", t.TempDir())
	store, _ := openStore()
	host, _ := store.key()
	first, second := bytes.Repeat([]byte{1}, 32), bytes.Repeat([]byte{2}, 32)

	link, err := trust(store, "wss://relay.example", host, base64.StdEncoding.EncodeToString(first), "Lucas's Mac")
	if err != nil {
		t.Fatal(err)
	}
	text, _ := base64.RawURLEncoding.DecodeString(strings.TrimPrefix(link, "https://farol.sh/pair#"))
	var shown offer
	if json.Unmarshal(text, &shown) != nil || shown.Relay != "wss://relay.example" || !bytes.Equal(shown.Mac, host.public) || len(shown.Secret) != 0 {
		t.Fatalf("link %s says %+v", link, shown)
	}

	// The same Mac added again, with a new key, takes the place of the old one.
	if _, err := trust(store, "wss://relay.example", host, base64.StdEncoding.EncodeToString(second), "Lucas's Mac"); err != nil {
		t.Fatal(err)
	}
	if devices := store.devices(); len(devices) != 1 || !bytes.Equal(devices[0].Key, second) {
		t.Fatalf("devices %+v", devices)
	}
}

func TestTrustRefusesWhatIsNotAKeyAndAName(t *testing.T) {
	t.Setenv("FAROL_HOST_DIR", t.TempDir())
	store, _ := openStore()
	host, _ := store.key()
	for _, args := range [][2]string{{"not base64", "Mac"}, {base64.StdEncoding.EncodeToString([]byte("short")), "Mac"}, {base64.StdEncoding.EncodeToString(bytes.Repeat([]byte{1}, 32)), " "}} {
		if _, err := trust(store, "wss://relay.example", host, args[0], args[1]); err == nil {
			t.Errorf("trusted %q %q", args[0], args[1])
		}
	}
	if len(store.devices()) != 0 {
		t.Fatal("a refused Mac was let in")
	}
}
