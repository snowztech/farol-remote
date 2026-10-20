package main

import (
	"bytes"
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log"
)

// Android sends the UTF-8 bytes of this marker and its FCM registration token
// in the existing notify.token field. The host treats those bytes as opaque.
const fcmTokenPrefix = "fcm:v1:"

type pushers struct {
	apns *pusher
	fcm  *fcmPusher
}

func newPushers() (pushSender, error) {
	apns, err := newPusher()
	if err != nil {
		return nil, err
	}
	fcm, err := newFCMPusher()
	if err != nil {
		// A bad Firebase setting must not take down APNs or the relay.
		log.Printf("firebase notifications disabled: %v", err)
	}
	if apns == nil && fcm == nil {
		return nil, nil
	}
	return &pushers{apns: apns, fcm: fcm}, nil
}

func (p *pushers) send(ctx context.Context, asked []byte, room string) error {
	request, token, android, err := classifyPush(asked)
	if err != nil {
		return err
	}
	if android {
		if p.fcm == nil {
			return errors.New("firebase notifications are not configured")
		}
		return p.fcm.send(ctx, request, string(token[len(fcmTokenPrefix):]), room)
	}
	if p.apns == nil {
		return errors.New("apple notifications are not configured")
	}
	return p.apns.send(ctx, asked, room)
}

func classifyPush(asked []byte) (pushRequest, []byte, bool, error) {
	var request pushRequest
	if err := json.Unmarshal(asked, &request); err != nil {
		return request, nil, false, err
	}
	// APNs tokens are hex too. An Android token is distinguished only after
	// decoding the bytes that the host already passes through unchanged.
	if len(request.Token) > 16384 {
		return request, nil, false, errors.New("not a notification this relay sends")
	}
	token, err := hex.DecodeString(request.Token)
	if err != nil {
		return request, nil, false, errors.New("not a notification this relay sends")
	}
	return request, token, bytes.HasPrefix(token, []byte(fcmTokenPrefix)), nil
}
