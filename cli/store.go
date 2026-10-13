package main

import (
	"bytes"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

// A store is the folder this host keeps its key and its paired phones in. Only the user who runs the host can read it.
// `farol-remote pair` and the running host are two processes, and this folder is how one tells the other of a code.
type store struct {
	dir string
}

// openStore uses FAROL_REMOTE_DIR when set, and the user's config folder otherwise.
func openStore() (store, error) {
	dir := os.Getenv("FAROL_REMOTE_DIR")
	if dir == "" {
		config, err := os.UserConfigDir()
		if err != nil {
			return store{}, err
		}
		dir = filepath.Join(config, "farol-remote")
	}
	return store{dir}, os.MkdirAll(dir, 0o700)
}

func (s store) write(name string, content []byte) error {
	// Written beside and moved into place, so the other process never reads half a file.
	beside := filepath.Join(s.dir, name+".new")
	if err := os.WriteFile(beside, content, 0o600); err != nil {
		return err
	}
	return os.Rename(beside, filepath.Join(s.dir, name))
}

// key is made the first time it is asked for, and the same from then on.
func (s store) key() (deviceKey, error) {
	text, err := os.ReadFile(filepath.Join(s.dir, "key"))
	if errors.Is(err, fs.ErrNotExist) {
		key, err := newDeviceKey()
		if err != nil {
			return deviceKey{}, err
		}
		return key, s.write("key", []byte(base64.StdEncoding.EncodeToString(key.stored())))
	}
	if err != nil {
		return deviceKey{}, err
	}
	stored, err := base64.StdEncoding.DecodeString(string(bytes.TrimSpace(text)))
	if err != nil {
		return deviceKey{}, err
	}
	return loadDeviceKey(stored)
}

func (s store) devices() []device {
	var devices []device
	if text, err := os.ReadFile(filepath.Join(s.dir, "devices.json")); err == nil {
		json.Unmarshal(text, &devices)
	}
	return devices
}

func (s store) saveDevices(devices []device) error {
	text, _ := json.MarshalIndent(devices, "", "  ")
	return s.write("devices.json", text)
}

// add remembers a phone. One that pairs again takes the place of its older self.
func (s store) add(paired device) error {
	return s.saveDevices(append(without(s.devices(), paired.Key), paired))
}

func without(devices []device, key []byte) []device {
	kept := []device{}
	for _, d := range devices {
		if !bytes.Equal(d.Key, key) {
			kept = append(kept, d)
		}
	}
	return kept
}

// newOffer makes a code that works once and lasts two minutes, and leaves it where the running host finds it.
func (s store) newOffer(relay string, host deviceKey, now time.Time) (offer, error) {
	secret := make([]byte, 32)
	rand.Read(secret)
	shown := offer{Relay: relay, Mac: host.public, Secret: secret, Expires: int64(now.Add(2*time.Minute).Sub(year2001) / time.Second)}
	text, _ := json.Marshal(shown)
	return shown, s.write("offer.json", text)
}

// offer is the code on screen, if one is.
func (s store) offer() (offer, bool) {
	var shown offer
	text, err := os.ReadFile(filepath.Join(s.dir, "offer.json"))
	if err != nil || json.Unmarshal(text, &shown) != nil {
		return offer{}, false
	}
	return shown, true
}

func (s store) spendOffer() {
	os.Remove(filepath.Join(s.dir, "offer.json"))
}

// Kept open for as long as the host runs, which is what holds the lock.
var lockFile *os.File

// lock is true for the one host that may run from this folder.
func (s store) lock() bool {
	file, err := os.OpenFile(filepath.Join(s.dir, "lock"), os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return false
	}
	lockFile = file
	return syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB) == nil
}
