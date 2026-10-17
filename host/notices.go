package main

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/crypto/chacha20poly1305"
)

// Notifications when a session starts waiting or finishes, as Farol on a Mac sends them. The relay asks Apple to show them,
// and is told only which of the two it is. Which session goes along sealed, for the phone alone.

// A pushTarget is where Apple can reach one phone, as it said with `notify`.
type pushTarget struct {
	Device  []byte `json:"device"`
	Token   []byte `json:"token"`
	Sandbox bool   `json:"sandbox"`
	// The notices it wants. Nil for all of them, as from a phone that never said.
	Notices []string `json:"notices"`
	// Seals which session a notification is about. Nil for a phone that sent none.
	Key []byte `json:"key"`
}

func (s store) pushTargets() []pushTarget {
	var targets []pushTarget
	if text, err := os.ReadFile(filepath.Join(s.dir, "notify.json")); err == nil {
		json.Unmarshal(text, &targets)
	}
	return targets
}

// savePushTarget keeps where a phone is notified, in place of what it said before.
func (s store) savePushTarget(target pushTarget) error {
	targets := slices.DeleteFunc(s.pushTargets(), func(t pushTarget) bool { return string(t.Device) == string(target.Device) })
	text, _ := json.MarshalIndent(append(targets, target), "", "  ")
	return s.write("notify.json", text)
}

// notice notifies each phone that wants to know about the sessions that just started waiting or finished.
// The first look only learns what each session was doing, so starting the host notifies nobody.
func (h *host) notice(sessions []session) {
	before := h.statuses
	h.statuses = map[string]string{}
	for _, s := range sessions {
		h.statuses[s.ID] = s.Status
	}
	if before == nil {
		return
	}
	devices := h.store.devices()
	for _, s := range sessions {
		if s.Status == before[s.ID] || (s.Status != "waiting" && s.Status != "done") {
			continue
		}
		for _, target := range h.store.pushTargets() {
			// A phone forgotten since is not told.
			if find(devices, target.Device) == nil || (target.Notices != nil && !slices.Contains(target.Notices, s.Status)) {
				continue
			}
			h.send(pushFrame(target, s))
		}
	}
}

// pushFrame asks the relay to have Apple notify one phone, written as the Swift package's RelayFrame.push.
func pushFrame(target pushTarget, s session) []byte {
	request := map[string]string{"token": hex.EncodeToString(target.Token), "notice": s.Status, "sandbox": ""}
	if target.Sandbox {
		request["sandbox"] = "1"
	}
	if sealed := sealNotice(target.Key, s); sealed != nil {
		request["sealed"] = base64.StdEncoding.EncodeToString(sealed)
	}
	text, _ := json.Marshal(request)
	return append([]byte{framePush}, text...)
}

// sealNotice seals which session it is with a key only this host and the phone hold, as NoticeBox does:
// ChaCha20-Poly1305, its nonce first. A notification goes with no connection open, so the channel's own encryption cannot carry it.
func sealNotice(key []byte, s session) []byte {
	aead, err := chacha20poly1305.New(key)
	if err != nil {
		return nil
	}
	plain, _ := json.Marshal(map[string]string{"session": s.ID, "name": s.Name, "notice": s.Status})
	nonce := make([]byte, aead.NonceSize())
	rand.Read(nonce)
	return aead.Seal(nonce, nonce, plain, nil)
}
