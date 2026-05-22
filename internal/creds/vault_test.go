package creds

import (
	"context"
	"path/filepath"
	"testing"
)

func newTestVault(t *testing.T) *Vault {
	t.Helper()
	dir := t.TempDir()
	key, err := LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	if err != nil {
		t.Fatal(err)
	}
	v, err := OpenVault(filepath.Join(dir, "lynxai.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	return v
}

func TestVault_PutGet_RoundTrip(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	bundle := []byte(`{"host":"api.example.com","token":"abc"}`)
	if err := v.Put(ctx, "ex-prod", KindBearer, "api.example.com", bundle); err != nil {
		t.Fatalf("Put: %v", err)
	}

	got, err := v.Get(ctx, "ex-prod")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name != "ex-prod" {
		t.Errorf("Name = %q", got.Name)
	}
	if got.Kind != KindBearer {
		t.Errorf("Kind = %q", got.Kind)
	}
	if got.Host != "api.example.com" {
		t.Errorf("Host = %q", got.Host)
	}
	if string(got.Bundle) != string(bundle) {
		t.Errorf("Bundle = %s, want %s", got.Bundle, bundle)
	}
}

func TestVault_Get_NotFound(t *testing.T) {
	v := newTestVault(t)
	_, err := v.Get(context.Background(), "missing")
	if err != ErrCredentialNotFound {
		t.Fatalf("want ErrCredentialNotFound, got %v", err)
	}
}

func TestVault_Put_RejectsInvalidBundle(t *testing.T) {
	v := newTestVault(t)
	err := v.Put(context.Background(), "bad", KindBearer, "x", []byte(`{}`))
	if err == nil {
		t.Fatal("expected validation error for empty bearer bundle")
	}
}

func TestVault_Delete(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	bundle := []byte(`{"host":"x","token":"t"}`)
	if err := v.Put(ctx, "to-delete", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.Delete(ctx, "to-delete"); err != nil {
		t.Fatal(err)
	}
	if _, err := v.Get(ctx, "to-delete"); err != ErrCredentialNotFound {
		t.Fatalf("want ErrCredentialNotFound after Delete, got %v", err)
	}
}

func TestVault_List_OmitsBundle(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	bundle := []byte(`{"host":"x","token":"t"}`)
	if err := v.Put(ctx, "a", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "b", KindBearer, "y", bundle); err != nil {
		t.Fatal(err)
	}
	sums, err := v.List(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if len(sums) != 2 {
		t.Fatalf("want 2 summaries, got %d", len(sums))
	}
	// Summary type must not expose bundles
	for _, s := range sums {
		if s.Name == "" || s.Kind == "" || s.Host == "" {
			t.Errorf("summary missing fields: %+v", s)
		}
	}
}

func TestVault_PutSameNameUpdates(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()
	if err := v.Put(ctx, "same", KindBearer, "x", []byte(`{"host":"x","token":"old"}`)); err != nil {
		t.Fatal(err)
	}
	if err := v.Put(ctx, "same", KindBearer, "x", []byte(`{"host":"x","token":"new"}`)); err != nil {
		t.Fatal(err)
	}
	got, _ := v.Get(ctx, "same")
	if string(got.Bundle) != `{"host":"x","token":"new"}` {
		t.Fatalf("update didn't replace bundle: %s", got.Bundle)
	}
}
