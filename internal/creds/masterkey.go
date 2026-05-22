// Package creds — masterkey.go
package creds

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

const masterKeyLen = 32

// LoadOrCreateMasterKey reads a 32-byte master key from path, creating it
// (with 0600 perms) if absent. Returns the raw key bytes.
//
// Creation is atomic: the key is written to a temp file in the same directory
// and renamed into place, so concurrent first-starts can't end up with two
// different keys silently overwriting each other. If another process won the
// race, this function returns the bytes that process wrote.
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	if data, err := readKey(path); err == nil {
		return data, nil
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".masterkey-*")
	if err != nil {
		return nil, fmt.Errorf("create temp master key: %w", err)
	}
	tmpName := tmp.Name()
	// Best-effort cleanup if anything below fails (no-op after successful rename).
	defer func() { _ = os.Remove(tmpName) }()

	key := make([]byte, masterKeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("chmod temp master key: %w", err)
	}
	if _, err := tmp.Write(key); err != nil {
		tmp.Close()
		return nil, fmt.Errorf("write temp master key: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return nil, fmt.Errorf("close temp master key: %w", err)
	}

	// os.Link fails with EEXIST if path already exists — this gives us the
	// "create exclusive" semantics rename lacks, so a late writer can't clobber
	// the winner. If the link fails because someone else won the race, we honor
	// their key by re-reading what's on disk.
	if err := os.Link(tmpName, path); err != nil {
		if os.IsExist(err) {
			return readKey(path)
		}
		return nil, fmt.Errorf("install master key: %w", err)
	}
	// Re-read so all participants agree on the live key bytes.
	return readKey(path)
}

// readKey reads and length-validates an existing master key.
func readKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(data) != masterKeyLen {
		return nil, fmt.Errorf("master key at %s has wrong length %d (want %d)", path, len(data), masterKeyLen)
	}
	return data, nil
}
