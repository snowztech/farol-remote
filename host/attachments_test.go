package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const picture = "AAAAAAAA-BBBB-CCCC-DDDD-EEEEEEEEEEEE"

func testInbox(t *testing.T) *inbox {
	return &inbox{folder: filepath.Join(t.TempDir(), "Farol", "Attachments"), pending: map[string]*pendingPicture{}}
}

var png = append([]byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1A, '\n'}, "rest of it"...)

func TestAPictureIsPutTogetherFromItsParts(t *testing.T) {
	box, now := testInbox(t), time.Now()
	if file, err := box.take(picture, 0, 2, png[:5], now); file != "" || err != nil {
		t.Fatalf("after the first part: %q %v", file, err)
	}
	// The first part again, as a phone sends after a dropped connection.
	if file, err := box.take(picture, 0, 2, png[:5], now); file != "" || err != nil {
		t.Fatalf("the first part again: %q %v", file, err)
	}
	file, err := box.take(picture, 1, 2, png[5:], now)
	if err != nil || !strings.HasSuffix(file, "/Farol/Attachments/"+picture+".png") {
		t.Fatalf("got %q %v", file, err)
	}
	if kept, _ := os.ReadFile(file); string(kept) != string(png) {
		t.Fatal("the picture is not what was sent")
	}
	if info, _ := os.Stat(file); info.Mode().Perm() != 0o600 {
		t.Fatalf("readable by others: %v", info.Mode())
	}
	if again, _ := box.take(picture, 1, 2, png[5:], now); again != file {
		t.Fatal("the last part again did not say where the picture is")
	}
}

func TestOnlyAnImageUnderTheLimitWithAnIdForANameIsKept(t *testing.T) {
	box, now := testInbox(t), time.Now()
	if _, err := box.take(picture, 0, 1, []byte("#!/bin/sh\nrm -rf ~"), now); err != errNotAnImage {
		t.Fatalf("a script: %v", err)
	}
	if _, err := box.take("../../.bashrc", 0, 1, png, now); err != errOutOfOrder {
		t.Fatalf("a path for a name: %v", err)
	}
	if _, err := box.take(picture, 1, 3, png, now); err != errOutOfOrder {
		t.Fatalf("a part ahead of the others: %v", err)
	}
	big := make([]byte, maxPicture/2+1)
	box.take(picture, 0, 3, big, now)
	if _, err := box.take(picture, 1, 3, big, now); err != errTooLarge {
		t.Fatalf("too large: %v", err)
	}
	if entries, _ := os.ReadDir(box.folder); len(entries) != 0 {
		t.Fatalf("kept %v", entries)
	}
}

func TestOldPicturesAreSweptAway(t *testing.T) {
	box := testInbox(t)
	file, _ := box.take(picture, 0, 1, png, time.Now())
	box.sweep(7*24*time.Hour, time.Now().Add(8*24*time.Hour))
	if _, err := os.Stat(file); err == nil {
		t.Fatal("still there")
	}
}
