package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
)

func newTestServer(t *testing.T) (http.Handler, *creds.Vault) {
	t.Helper()
	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, err := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { v.Close() })
	h := NewRouter(Deps{Vault: v}) // engine/extractor unused for these tests
	return h, v
}

func TestCredentialsPut_Roundtrip(t *testing.T) {
	h, _ := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"x","kind":"bearer","host":"api.x","bundle":{"host":"api.x","token":"abc"}}`)
	req := httptest.NewRequest("POST", "/credentials", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 201 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}

	req = httptest.NewRequest("GET", "/credentials", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("list status = %d", w.Code)
	}
	var sums []creds.CredentialSummary
	if err := json.NewDecoder(w.Body).Decode(&sums); err != nil {
		t.Fatal(err)
	}
	if len(sums) != 1 || sums[0].Name != "x" {
		t.Fatalf("sums = %+v", sums)
	}
}

func TestCredentialsPut_InvalidBundle(t *testing.T) {
	h, _ := newTestServer(t)
	body := bytes.NewBufferString(`{"name":"x","kind":"bearer","host":"y","bundle":{}}`) // missing token
	req := httptest.NewRequest("POST", "/credentials", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 400 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
}

func TestCredentialsGet_NotFound(t *testing.T) {
	h, _ := newTestServer(t)
	req := httptest.NewRequest("GET", "/credentials/missing", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d", w.Code)
	}
}

func TestCredentialsDelete(t *testing.T) {
	h, v := newTestServer(t)
	_ = v.Put(context.Background(), "to-delete", creds.KindBearer, "x", []byte(`{"host":"x","token":"t"}`))

	req := httptest.NewRequest("DELETE", "/credentials/to-delete", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 204 {
		t.Errorf("status = %d", w.Code)
	}
}
