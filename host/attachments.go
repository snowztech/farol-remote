package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// An inbox is the pictures a phone sends for an agent to look at, put together from their parts and kept in a folder only you can read.
// The phone is the side to be careful with: it never names the file or says what it is, and only an image under the limit is kept.
// Done as Farol on a Mac does it, in AttachmentInbox.
type inbox struct {
	folder string
	// The parts taken so far, by picture.
	pending map[string]*pendingPicture
}

type pendingPicture struct {
	data    []byte
	next    int
	started time.Time
}

const (
	// The phone makes a photo far smaller than this. Anything larger is not what attaching is for.
	maxPicture = 10 << 20
	maxParts   = 200
)

var (
	errTooLarge   = errors.New("too large")
	errNotAnImage = errors.New("not an image")
	errOutOfOrder = errors.New("out of order")
	// An attachment's id names its file, so it must be a UUID and nothing else.
	attachmentID = regexp.MustCompile(`^[0-9A-Fa-f]{8}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{4}-[0-9A-Fa-f]{12}$`)
)

// newInbox keeps pictures in the cache folder, under Farol/Attachments, where a phone tells a picture's path from words.
func newInbox() *inbox {
	caches, err := os.UserCacheDir()
	if err != nil {
		caches = os.TempDir()
	}
	box := &inbox{folder: filepath.Join(caches, "Farol", "Attachments"), pending: map[string]*pendingPicture{}}
	box.sweep(7*24*time.Hour, time.Now())
	return box
}

// take takes one part. It returns where the picture is once its last part is in, and "" before.
// A part sent again after a dropped connection is one already taken, and changes nothing.
func (b *inbox) take(attachment string, part, parts int, data []byte, now time.Time) (string, error) {
	if !attachmentID.MatchString(attachment) || parts < 1 || parts > maxParts || part < 0 || part >= parts {
		return "", errOutOfOrder
	}
	attachment = strings.ToUpper(attachment)
	if done := b.written(attachment); done != "" {
		return done, nil
	}
	// Pieces of a picture the phone gave up on are not kept for ever.
	for id, entry := range b.pending {
		if now.Sub(entry.started) >= 10*time.Minute {
			delete(b.pending, id)
		}
	}
	entry := b.pending[attachment]
	if entry == nil {
		entry = &pendingPicture{started: now}
	}
	if part < entry.next {
		return "", nil
	}
	if part != entry.next {
		return "", errOutOfOrder
	}
	if len(entry.data)+len(data) > maxPicture {
		delete(b.pending, attachment)
		return "", errTooLarge
	}
	entry.data = append(entry.data, data...)
	entry.next++
	if entry.next < parts {
		b.pending[attachment] = entry
		return "", nil
	}
	delete(b.pending, attachment)
	kind := imageKind(entry.data)
	if kind == "" {
		return "", errNotAnImage
	}
	if err := os.MkdirAll(b.folder, 0o700); err != nil {
		return "", err
	}
	file := filepath.Join(b.folder, attachment+"."+kind)
	// Written aside and moved in place, so an agent never reads half a picture.
	if err := os.WriteFile(file+".part", entry.data, 0o600); err != nil {
		return "", err
	}
	return file, os.Rename(file+".part", file)
}

// sweep removes the pictures older than age, which an agent has long since looked at.
func (b *inbox) sweep(age time.Duration, now time.Time) {
	entries, _ := os.ReadDir(b.folder)
	for _, entry := range entries {
		if info, err := entry.Info(); err == nil && now.Sub(info.ModTime()) > age {
			os.Remove(filepath.Join(b.folder, entry.Name()))
		}
	}
}

// written is the picture already written for this attachment, when its last part came twice.
func (b *inbox) written(attachment string) string {
	for _, kind := range []string{"jpg", "png", "heic"} {
		file := filepath.Join(b.folder, attachment+"."+kind)
		if _, err := os.Stat(file); err == nil {
			return file
		}
	}
	return ""
}

// imageKind is what kind of image the bytes are, by how its format begins. Empty for anything else.
func imageKind(data []byte) string {
	switch {
	case bytes.HasPrefix(data, []byte{0xFF, 0xD8, 0xFF}):
		return "jpg"
	case bytes.HasPrefix(data, []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}):
		return "png"
	case len(data) >= 12 && string(data[4:8]) == "ftyp":
		switch string(data[8:12]) {
		case "heic", "heix", "mif1", "msf1", "hevc":
			return "heic"
		}
	}
	return ""
}
