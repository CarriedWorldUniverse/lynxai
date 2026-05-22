package creds

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"sync"
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
	// Windows doesn't honor unix perm bits — os.FileInfo.Mode().Perm() reports
	// 0666 regardless of what WriteFile/Chmod requested. The 0600 contract still
	// holds on the OSes where it matters (linux, darwin, docker target).
	if runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
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

func TestLoadOrCreateMasterKey_ConcurrentFirstStartAgreesOnOneKey(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "master.key")

	const N = 8
	keys := make([][]byte, N)
	errs := make([]error, N)
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func(i int) {
			defer wg.Done()
			keys[i], errs[i] = LoadOrCreateMasterKey(path)
		}(i)
	}
	wg.Wait()

	for i, err := range errs {
		if err != nil {
			t.Fatalf("worker %d: %v", i, err)
		}
	}
	// All workers must agree on one key. Whichever one is on disk wins.
	want := keys[0]
	if len(want) != 32 {
		t.Fatalf("key len = %d", len(want))
	}
	for i := 1; i < N; i++ {
		if !bytes.Equal(want, keys[i]) {
			t.Fatalf("worker %d disagrees with worker 0 on master key", i)
		}
	}
}
