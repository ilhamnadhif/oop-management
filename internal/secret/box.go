// Package secret seals the few values the app must read back but no one should
// read off a spreadsheet. A password that signs in to another system cannot be
// hashed, since the app has to send it; it is encrypted instead, under a key
// that lives in the deployment and never in the sheet that holds the result.
package secret

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// sealedPrefix marks the format, so a later change of it can tell old values
// from new ones.
const sealedPrefix = "v1."

// MinKeyLength is the shortest key accepted, enough to rule out a word someone
// chose by hand.
const MinKeyLength = 32

// ErrCannotOpen means a value could not be read back: the key has changed, the
// value belongs to another owner, or the cell was edited.
var ErrCannotOpen = errors.New("secret cannot be opened")

// Box seals and opens values under one key.
type Box struct {
	aead cipher.AEAD
}

// NewBox prepares a box. The key may be any string of at least MinKeyLength
// characters; it is hashed to the size AES-256 needs.
func NewBox(key string) (*Box, error) {
	if len(key) < MinKeyLength {
		return nil, fmt.Errorf("secret key must be at least %d characters", MinKeyLength)
	}
	sum := sha256.Sum256([]byte(key))
	block, err := aes.NewCipher(sum[:])
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Box{aead: aead}, nil
}

// Seal encrypts plain for owner. Owner is authenticated rather than hidden: a
// value copied onto another owner's row will not open there.
func (b *Box) Seal(plain, owner string) (string, error) {
	nonce := make([]byte, b.aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("read nonce: %w", err)
	}
	sealed := b.aead.Seal(nonce, nonce, []byte(plain), []byte(owner))
	return sealedPrefix + base64.RawURLEncoding.EncodeToString(sealed), nil
}

// Open decrypts what Seal wrote for the same owner.
func (b *Box) Open(sealed, owner string) (string, error) {
	encoded, ok := strings.CutPrefix(strings.TrimSpace(sealed), sealedPrefix)
	if !ok {
		return "", ErrCannotOpen
	}
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil || len(raw) < b.aead.NonceSize() {
		return "", ErrCannotOpen
	}
	nonce, body := raw[:b.aead.NonceSize()], raw[b.aead.NonceSize():]
	plain, err := b.aead.Open(nil, nonce, body, []byte(owner))
	if err != nil {
		return "", ErrCannotOpen
	}
	return string(plain), nil
}
