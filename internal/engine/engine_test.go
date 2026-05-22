//go:build integration

package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestEngine_Fetch_StaticPage(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><head><title>T</title></head><body><h1>Hello</h1></body></html>`))
	}))
	defer srv.Close()

	e, err := New(Config{PoolSize: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer e.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	res, err := e.Fetch(ctx, FetchRequest{URL: srv.URL, Timeout: 15 * time.Second})
	if err != nil {
		t.Fatalf("Fetch: %v", err)
	}
	if res.Status != 200 {
		t.Errorf("Status = %d", res.Status)
	}
	if res.Title != "T" {
		t.Errorf("Title = %q", res.Title)
	}
	if !strings.Contains(res.Markdown, "# Hello") {
		t.Errorf("Markdown missing h1: %q", res.Markdown)
	}
}
