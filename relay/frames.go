package main

import "encoding/binary"

// The first byte of every message says what it is.
const (
	// Bytes from one side for the other. To and from the Mac, the phone's number follows.
	frameData byte = iota
	// To the Mac: a phone with this number arrived, or went.
	frameJoined
	frameLeft
	// To a phone: its Mac is here, or is not.
	frameOnline
	frameOffline
	// From the Mac: have Apple notify one of its phones, which may not be connected at all.
	framePush
)

func numbered(kind byte, id uint32, payload []byte) []byte {
	message := make([]byte, 5, 5+len(payload))
	message[0] = kind
	binary.BigEndian.PutUint32(message[1:], id)
	return append(message, payload...)
}
