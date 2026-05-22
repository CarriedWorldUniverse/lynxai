package creds

import (
	"crypto/rand"
	"fmt"
	"io"
	"os"
)

const masterKeyLen = 32

// LoadOrCreateMasterKey reads a 32-byte master key from path, creating it
// (with 0600 perms) if absent. Returns the raw key bytes.
func LoadOrCreateMasterKey(path string) ([]byte, error) {
	data, err := os.ReadFile(path)
	if err == nil {
		if len(data) != masterKeyLen {
			return nil, fmt.Errorf("master key at %s has wrong length %d (want %d)", path, len(data), masterKeyLen)
		}
		return data, nil
	}
	if !os.IsNotExist(err) {
		return nil, fmt.Errorf("read master key: %w", err)
	}

	key := make([]byte, masterKeyLen)
	if _, err := io.ReadFull(rand.Reader, key); err != nil {
		return nil, fmt.Errorf("generate master key: %w", err)
	}
	if err := os.WriteFile(path, key, 0o600); err != nil {
		return nil, fmt.Errorf("write master key: %w", err)
	}
	return key, nil
}
