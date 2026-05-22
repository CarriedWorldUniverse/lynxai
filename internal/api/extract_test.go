package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http/httptest"
	"path/filepath"
	"testing"

	bridle "github.com/CarriedWorldUniverse/bridle"

	"github.com/CarriedWorldUniverse/lynxai/internal/creds"
	"github.com/CarriedWorldUniverse/lynxai/internal/engine"
	"github.com/CarriedWorldUniverse/lynxai/internal/extract"
)

type stubTurner struct {
	emit json.RawMessage
}

func (s *stubTurner) RunTurn(ctx context.Context, req bridle.TurnRequest, runner bridle.ToolRunner, sink bridle.EventSink) (bridle.TurnResult, error) {
	return bridle.TurnResult{
		ToolCalls: []bridle.ToolInvocation{{Name: "emit_extraction", Args: s.emit}},
	}, nil
}

func TestExtract_HandlerWiresEverything(t *testing.T) {
	eng := &stubEngine{res: &engine.FetchResult{Markdown: "# Hello\nname: alice", Status: 200}}
	xtr := extract.NewExtractor(&stubTurner{emit: json.RawMessage(`{"name":"alice"}`)})

	dir := t.TempDir()
	key, _ := creds.LoadOrCreateMasterKey(filepath.Join(dir, "master.key"))
	v, _ := creds.OpenVault(filepath.Join(dir, "lynxai.db"), key)
	t.Cleanup(func() { v.Close() })
	h := NewRouter(Deps{Vault: v, Engine: eng, Extractor: xtr, Forms: engine.NewFormLoginCache()})

	body := bytes.NewBufferString(`{
		"url": "https://x",
		"schema": {"type":"object","properties":{"name":{"type":"string"}},"required":["name"]}
	}`)
	req := httptest.NewRequest("POST", "/extract", body)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("status %d body %s", w.Code, w.Body)
	}
	var got map[string]any
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got["json"] == nil {
		t.Fatalf("response missing 'json': %+v", got)
	}
	js, _ := json.Marshal(got["json"])
	if string(js) != `{"name":"alice"}` {
		t.Errorf("got %s", js)
	}
}
