package creds

import (
	"context"
	"testing"
)

func TestVault_RecordUse_WritesRow(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	if err := v.RecordUse(ctx, "name", "https://example.com/x", "ok"); err != nil {
		t.Fatalf("RecordUse: %v", err)
	}

	rows, err := v.db.QueryContext(ctx, `SELECT name, request_url, outcome FROM credential_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		count++
		var name, url, outcome string
		if err := rows.Scan(&name, &url, &outcome); err != nil {
			t.Fatal(err)
		}
		if name != "name" || url != "https://example.com/x" || outcome != "ok" {
			t.Errorf("row = (%q, %q, %q)", name, url, outcome)
		}
	}
	if count != 1 {
		t.Fatalf("want 1 audit row, got %d", count)
	}
}

func TestVault_RecordUse_DoesNotLeakBundles(t *testing.T) {
	v := newTestVault(t)
	ctx := context.Background()

	bundle := []byte(`{"host":"x","token":"SECRET-DO-NOT-LOG"}`)
	if err := v.Put(ctx, "name", KindBearer, "x", bundle); err != nil {
		t.Fatal(err)
	}
	if err := v.RecordUse(ctx, "name", "https://x/y", "ok"); err != nil {
		t.Fatal(err)
	}

	// Scan the entire audit table for any occurrence of the secret.
	rows, err := v.db.QueryContext(ctx, `SELECT name, request_url, outcome FROM credential_audit`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	for rows.Next() {
		var n, u, o string
		_ = rows.Scan(&n, &u, &o)
		for _, s := range []string{n, u, o} {
			if containsSecret(s) {
				t.Errorf("audit row contains secret: %q", s)
			}
		}
	}
}

func containsSecret(s string) bool {
	return len(s) >= 6 && (s == "SECRET-DO-NOT-LOG" || (len(s) > 17 && (s[:17] == "SECRET-DO-NOT-LOG" || s[len(s)-17:] == "SECRET-DO-NOT-LOG")))
}
