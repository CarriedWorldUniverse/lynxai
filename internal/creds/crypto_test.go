package creds

import (
	"bytes"
	"errors"
	"testing"
)

func TestDeriveKey_Deterministic(t *testing.T) {
	master := []byte("test-master-key-32-bytes-padded!!")
	k1 := deriveKey(master)
	k2 := deriveKey(master)
	if !bytes.Equal(k1, k2) {
		t.Fatalf("deriveKey not deterministic: %x vs %x", k1, k2)
	}
	if len(k1) != 32 {
		t.Fatalf("want 32-byte key, got %d", len(k1))
	}
}

func TestDeriveKey_DifferentMastersDifferentKeys(t *testing.T) {
	a := deriveKey([]byte("master-a-padded-to-32-bytes-..!!"))
	b := deriveKey([]byte("master-b-padded-to-32-bytes-..!!"))
	if bytes.Equal(a, b) {
		t.Fatal("different masters produced same key")
	}
}

func TestEncryptDecrypt_RoundTrip(t *testing.T) {
	key := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	plaintext := []byte(`{"host":"example.com","token":"abc"}`)

	ct, nonce, err := encrypt(key, plaintext)
	if err != nil {
		t.Fatalf("encrypt: %v", err)
	}
	if len(nonce) != nonceLen {
		t.Fatalf("nonce len = %d, want %d", len(nonce), nonceLen)
	}
	if bytes.Equal(ct, plaintext) {
		t.Fatal("ciphertext equals plaintext")
	}

	pt, err := decrypt(key, nonce, ct)
	if err != nil {
		t.Fatalf("decrypt: %v", err)
	}
	if !bytes.Equal(pt, plaintext) {
		t.Fatalf("round-trip mismatch: %s vs %s", pt, plaintext)
	}
}

func TestDecrypt_WrongKeyFails(t *testing.T) {
	good := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	bad := deriveKey([]byte("master-key-32-bytes-padded-OTHR!"))
	ct, nonce, err := encrypt(good, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := decrypt(bad, nonce, ct); !errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("want ErrDecryptFailed, got %v", err)
	}
}

func TestDecrypt_BadNonceLength(t *testing.T) {
	key := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	ct, _, err := encrypt(key, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	// Pass a too-short nonce — should error with non-ErrDecryptFailed so callers
	// can distinguish a data-integrity bug from a real auth failure.
	_, err = decrypt(key, []byte{1, 2, 3}, ct)
	if err == nil {
		t.Fatal("expected error for bad nonce length")
	}
	if errors.Is(err, ErrDecryptFailed) {
		t.Fatalf("nonce-length error should NOT be ErrDecryptFailed (callers need to distinguish): %v", err)
	}
}

func TestEncrypt_NoncesAreUnique(t *testing.T) {
	key := deriveKey([]byte("master-key-32-bytes-padded-test!"))
	seen := map[string]bool{}
	for i := 0; i < 100; i++ {
		_, nonce, err := encrypt(key, []byte("x"))
		if err != nil {
			t.Fatal(err)
		}
		s := string(nonce)
		if seen[s] {
			t.Fatal("duplicate nonce produced")
		}
		seen[s] = true
	}
}
