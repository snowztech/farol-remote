package main

import (
	"crypto/rand"
	"errors"
	"time"

	"github.com/cloudflare/circl/hpke"
	"github.com/cloudflare/circl/kem"
)

// The same choices as the Swift package, which is what lets the two open each other's messages.
var (
	suite  = hpke.NewSuite(hpke.KEM_X25519_HKDF_SHA256, hpke.KDF_HKDF_SHA256, hpke.AEAD_ChaCha20Poly1305)
	scheme = hpke.KEM_X25519_HKDF_SHA256.Scheme()
)

var (
	errOfferExpired  = errors.New("the pairing code ran out")
	errUnknownDevice = errors.New("not a device this host paired")
	errNotThisOne    = errors.New("not a message this connection made, in this order")
)

// A deviceKey is this host's own key. The public half is how a phone knows it.
type deviceKey struct {
	secret kem.PrivateKey
	public []byte
}

func newDeviceKey() (deviceKey, error) {
	_, secret, err := scheme.GenerateKeyPair()
	if err != nil {
		return deviceKey{}, err
	}
	return keyFrom(secret)
}

func loadDeviceKey(stored []byte) (deviceKey, error) {
	secret, err := scheme.UnmarshalBinaryPrivateKey(stored)
	if err != nil {
		return deviceKey{}, err
	}
	return keyFrom(secret)
}

func keyFrom(secret kem.PrivateKey) (deviceKey, error) {
	public, err := secret.Public().MarshalBinary()
	return deviceKey{secret, public}, err
}

func (k deviceKey) stored() []byte {
	raw, _ := k.secret.MarshalBinary()
	return raw
}

// acceptPairing opens what a phone sent after scanning the code. It opens only for the key the request names,
// and only if that phone knew the secret in the code.
func acceptPairing(request pairingRequest, shown offer, host deviceKey, now time.Time) (device, error) {
	if !now.Before(shown.expires()) {
		return device{}, errOfferExpired
	}
	phone, err := scheme.UnmarshalBinaryPublicKey(request.Device)
	if err != nil {
		return device{}, errUnknownDevice
	}
	receiver, err := suite.NewReceiver(host.secret, []byte("farol pair 1"))
	if err != nil {
		return device{}, err
	}
	opener, err := receiver.SetupAuthPSK(request.Encapsulated, shown.Secret, []byte("farol pair secret"), phone)
	if err != nil {
		return device{}, errUnknownDevice
	}
	name, err := opener.Open(request.Sealed, nil)
	if err != nil {
		return device{}, errUnknownDevice
	}
	return device{Key: request.Device, Name: string(name)}, nil
}

// A channel is one side of an encrypted connection. Each direction is sealed to the other side's key and signed with ours,
// and both are tied to the two numbers from the hellos, so a recording of an older connection does not open.
type channel struct {
	me     deviceKey
	side   string
	nonce  []byte
	peer   *channelHello
	sealer hpke.Sealer
	opener hpke.Opener
}

// newChannel is for one connection. side is "mac" for a host and "phone" for a phone, the names the Swift package uses.
func newChannel(me deviceKey, side string) *channel {
	nonce := make([]byte, 32)
	rand.Read(nonce)
	return &channel{me: me, side: side, nonce: nonce}
}

func (c *channel) hello() channelHello {
	return channelHello{Key: c.me.public, Nonce: c.nonce}
}

// connect is called with the other side's hello, once the caller knows its key is one it paired.
// What it returns goes to the other side, which lets it open what we seal.
func (c *channel) connect(peer channelHello) ([]byte, error) {
	theirs, err := scheme.UnmarshalBinaryPublicKey(peer.Key)
	if err != nil {
		return nil, errUnknownDevice
	}
	c.peer = &peer
	sender, err := suite.NewSender(theirs, c.info(c.side))
	if err != nil {
		return nil, err
	}
	encapsulated, sealer, err := sender.SetupAuth(rand.Reader, c.me.secret)
	c.sealer = sealer
	return encapsulated, err
}

// accept takes what the other side's connect returned.
func (c *channel) accept(encapsulated []byte) error {
	if c.peer == nil {
		return errNotThisOne
	}
	theirs, err := scheme.UnmarshalBinaryPublicKey(c.peer.Key)
	if err != nil {
		return errNotThisOne
	}
	other := "phone"
	if c.side == "phone" {
		other = "mac"
	}
	receiver, err := suite.NewReceiver(c.me.secret, c.info(other))
	if err != nil {
		return err
	}
	if c.opener, err = receiver.SetupAuth(encapsulated, theirs); err != nil {
		return errNotThisOne
	}
	return nil
}

func (c *channel) seal(message []byte) ([]byte, error) {
	if c.sealer == nil {
		return nil, errNotThisOne
	}
	return c.sealer.Seal(message, nil)
}

func (c *channel) open(sealed []byte) ([]byte, error) {
	if c.opener == nil {
		return nil, errNotThisOne
	}
	plain, err := c.opener.Open(sealed, nil)
	if err != nil {
		return nil, errNotThisOne
	}
	return plain, nil
}

// info is the same bytes on both ends for one direction: who writes, then the host's number, then the phone's.
func (c *channel) info(writer string) []byte {
	host, phone := c.nonce, c.peer.Nonce
	if c.side == "phone" {
		host, phone = phone, host
	}
	return append(append([]byte("farol remote 1 "+writer), host...), phone...)
}
