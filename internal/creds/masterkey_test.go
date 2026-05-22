package creds

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadOrCreateMasterKey_CreatesOnFirstRun(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")

	k1, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("first call: %v", err)
	}
	if len(k1) != 32 {
		t.Fatalf("want 32-byte key, got %d", len(k1))
	}

	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if info.Mode().Perm() != 0o600 {
		t.Errorf("perms = %o, want 0600", info.Mode().Perm())
	}

	k2, err := LoadOrCreateMasterKey(path)
	if err != nil {
		t.Fatalf("second call: %v", err)
	}
	if string(k1) != string(k2) {
		t.Fatal("second load returned different key")
	}
}
