// Package creds implements lynxai's encrypted credential vault and audit log.
//
// Encryption: AES-256-GCM with per-row nonce. Data key derived via HKDF-SHA256
// from a master key stored at <data_dir>/master.key (0600 perms). HKDF info
// string is fixed at "lynxai.credentials.v1" — bumping it forces re-keying.
package creds

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/hkdf"
)

const (
	hkdfInfo = "lynxai.credentials.v1"
	nonceLen = 12
	keyLen   = 32 // AES-256
)

// deriveKey returns a 32-byte data key derived from master via HKDF-SHA256.
func deriveKey(master []byte) []byte {
	r := hkdf.New(sha256.New, master, nil, []byte(hkdfInfo))
	out := make([]byte, keyLen)
	if _, err := io.ReadFull(r, out); err != nil {
		panic(fmt.Errorf("hkdf read: %w", err)) // cannot happen with hkdf.New
	}
	return out
}

// encrypt returns ciphertext + random nonce for plaintext, sealed with key.
func encrypt(key, plaintext []byte) (ciphertext, nonce []byte, err error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, nil, fmt.Errorf("gcm new: %w", err)
	}
	nonce = make([]byte, nonceLen)
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, nil, fmt.Errorf("nonce read: %w", err)
	}
	ciphertext = gcm.Seal(nil, nonce, plaintext, nil)
	return ciphertext, nonce, nil
}

// ErrDecryptFailed indicates the ciphertext won't open with the given key/nonce.
var ErrDecryptFailed = errors.New("creds: decryption failed")

// decrypt opens ciphertext sealed by encrypt. Returns ErrDecryptFailed on any
// authentication failure (wrong key, tampered ciphertext, wrong nonce).
func decrypt(key, nonce, ciphertext []byte) ([]byte, error) {
	if len(nonce) != nonceLen {
		return nil, fmt.Errorf("creds: nonce length %d, want %d", len(nonce), nonceLen)
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("aes new: %w", err)
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gcm new: %w", err)
	}
	pt, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return nil, ErrDecryptFailed
	}
	return pt, nil
}
