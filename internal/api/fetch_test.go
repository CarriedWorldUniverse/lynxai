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
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
)

type stubEngine struct {
	got engine.FetchRequest
	res *engine.FetchResult
	err error
}

func (s *stubEngine) Fetch(ctx context.Context, req engine.FetchRequest) (*engine.FetchResult, error) {
	s.got = req
	return s.res, s.err
}

func newTestServerWithEngine(t *testing.T, eng FetcherEngine) (http.Handler, *creds.Vault) {
	t.Helper()
	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, _ := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	t.Cleanup(func() { v.Close() })
	return NewRouter(Deps{Vault: v, Engine: eng, Forms: engine.NewFormLoginCache()}), v
}

func TestFetch_NoCredential(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "# hello", Status: 200, FinalURL: "https://x"}}
	h, _ := newTestServerWithEngine(t, eng)

	body := bytes.NewBufferString(`{"url":"https://example.com"}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status = %d body=%s", w.Code, w.Body)
	}
	var got engine.FetchResult
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got.Markdown != "# hello" {
		t.Errorf("md = %q", got.Markdown)
	}
	if eng.got.URL != "https://example.com" {
		t.Errorf("url = %q", eng.got.URL)
	}
	if eng.got.Credential != nil {
		t.Errorf("credential should be nil")
	}
}

func TestFetch_WithBearerCredential(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "ok", Status: 200}}
	h, v := newTestServerWithEngine(t, eng)
	_ = v.Put(context.Background(), "ex", creds.KindBearer, "example.com", []byte(`{"host":"example.com","token":"abc"}`))

	body := bytes.NewBufferString(`{"url":"https://example.com/x","credential":{"name":"ex"}}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	if eng.got.Credential == nil || eng.got.Credential.Headers["Authorization"] != "Bearer abc" {
		t.Errorf("credential not applied: %+v", eng.got.Credential)
	}
}

func TestFetch_CredentialNotFound(t *testing.T) {
	eng := &stubEngine{}
	h, _ := newTestServerWithEngine(t, eng)
	body := bytes.NewBufferString(`{"url":"https://x","credential":{"name":"missing"}}`)
	req := httptest.NewRequest("POST", "/fetch", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 404 {
		t.Errorf("status = %d", w.Code)
	}
}
