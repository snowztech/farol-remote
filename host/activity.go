package main

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"slices"
)

// The Live Activity on each phone's lock screen: how many sessions wait for you and how many work, kept current through the relay
// while the app is closed. Done as Farol on a Mac does it, in RemoteBridge.updateActivities.

// An activityTarget is where Apple can reach the Live Activity a phone started, and the sessions it last showed, so only a change is sent.
type activityTarget struct {
	Device  []byte    `json:"device"`
	Token   []byte    `json:"token"`
	Sandbox bool      `json:"sandbox"`
	Shown   []session `json:"shown"`
}

func (s store) activityTargets() []activityTarget {
	var targets []activityTarget
	if text, err := os.ReadFile(filepath.Join(s.dir, "activities.json")); err == nil {
		json.Unmarshal(text, &targets)
	}
	return targets
}

func (s store) saveActivityTargets(targets []activityTarget) {
	text, _ := json.MarshalIndent(targets, "", "  ")
	s.write("activities.json", text)
}

// activityStarted keeps where to reach the Live Activity a phone just started.
func (h *host) activityStarted(device, token []byte, sandbox bool, sessions []session) {
	delete(h.starting, string(device))
	targets := slices.DeleteFunc(h.store.activityTargets(), func(t activityTarget) bool { return string(t.Device) == string(device) })
	h.store.saveActivityTargets(append(targets, activityTarget{Device: device, Token: token, Sandbox: sandbox}))
	h.updateActivities(sessions)
}

// updateActivities updates the Live Activity on each phone that has one, when what it shows changed, and ends it once nothing waits or works.
// When something gets busy again, it is started on each phone that is not open, and an open one starts its own.
func (h *host) updateActivities(sessions []session) {
	busy := slices.DeleteFunc(slices.Clone(sessions), func(s session) bool { return !needsYou(s) && s.Status != "working" })
	pushes := h.store.pushTargets()
	keyOf := func(device []byte) []byte {
		i := slices.IndexFunc(pushes, func(t pushTarget) bool { return string(t.Device) == string(device) })
		if i < 0 {
			return nil
		}
		return pushes[i].Key
	}
	targets, changed := h.store.activityTargets(), false
	kept := targets[:0]
	for _, target := range targets {
		if reflect.DeepEqual(target.Shown, busy) || (len(target.Shown) == 0 && len(busy) == 0) {
			kept = append(kept, target)
			continue
		}
		changed = true
		state := newActivityState(sessions, keyOf(target.Device))
		// Urgent only when one more agent waits for you. Apple caps urgent pushes, and the rest can wait for the phone's next wake.
		urgent := state.waiting > len(slices.DeleteFunc(slices.Clone(target.Shown), func(s session) bool { return !needsYou(s) }))
		event := "update"
		if state.quiet() {
			event = "end"
		}
		h.send(activityFrame(target.Token, target.Sandbox, event, urgent, state))
		if !state.quiet() {
			target.Shown = busy
			kept = append(kept, target)
		}
	}
	if changed {
		h.store.saveActivityTargets(kept)
	}
	if len(busy) == 0 {
		clear(h.starting)
		return
	}
	for _, target := range pushes {
		device := string(target.Device)
		if target.Starts == nil || h.hasOpen(target.Device) || h.starting[device] ||
			slices.ContainsFunc(kept, func(t activityTarget) bool { return string(t.Device) == device }) {
			continue
		}
		h.starting[device] = true
		h.send(activityFrame(target.Starts, target.Sandbox, "start", true, newActivityState(sessions, target.Key)))
	}
}

// hasOpen is true while the device has the app open.
func (h *host) hasOpen(device []byte) bool {
	for _, p := range h.phones {
		if p.device != nil && string(p.device.Key) == string(device) {
			return true
		}
	}
	return false
}

func needsYou(s session) bool { return s.Status == "waiting" || s.Status == "stopped" }

// An activityState is what a Live Activity shows, as RemoteActivityState: the counts, and the first few sessions that need you or work,
// sealed with the phone's key. Only a few, since Apple caps a push at 4 KB and the relay a sealed part at 2000 characters.
type activityState struct {
	waiting, working int
	sealed           string
}

func newActivityState(sessions []session, key []byte) activityState {
	var waiting, working []session
	for _, s := range sessions {
		if needsYou(s) {
			waiting = append(waiting, s)
		} else if s.Status == "working" {
			working = append(working, s)
		}
	}
	state := activityState{waiting: len(waiting), working: len(working)}
	shown := []session{}
	for _, s := range append(waiting, working...)[:min(4, len(waiting)+len(working))] {
		if name := []rune(s.Name); len(name) > 60 {
			s.Name = string(name[:60])
		}
		s.Branch = nil
		shown = append(shown, s)
	}
	if text, _ := json.Marshal(shown); key != nil {
		if sealed := seal(key, text); sealed != nil {
			state.sealed = base64.StdEncoding.EncodeToString(sealed)
		}
	}
	return state
}

func (s activityState) quiet() bool { return s.waiting == 0 && s.working == 0 }

// activityFrame asks the relay to start, update or end a Live Activity, written as the Swift package's RelayFrame.activity.
func activityFrame(token []byte, sandbox bool, event string, urgent bool, state activityState) []byte {
	request := map[string]any{"token": hex.EncodeToString(token), "sandbox": "", "activity": event,
		"waiting": state.waiting, "working": state.working, "urgent": urgent}
	if sandbox {
		request["sandbox"] = "1"
	}
	if state.sealed != "" {
		request["sealed"] = state.sealed
	}
	text, _ := json.Marshal(request)
	return append([]byte{framePush}, text...)
}
