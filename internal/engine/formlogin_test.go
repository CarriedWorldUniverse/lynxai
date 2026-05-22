package engine

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestFormLogin_Success(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatal(err)
		}
		if r.FormValue("username") != "alice" || r.FormValue("password") != "secret" {
			w.WriteHeader(401)
			return
		}
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "xyz", Path: "/"})
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	ctx := context.Background()
	cookies, err := cache.Login(ctx, "test-cred", FormLoginConfig{
		LoginURL:          srv.URL + "/login",
		Method:            "POST",
		UserField:         "username",
		PassField:         "password",
		User:              "alice",
		Password:          "secret",
		SuccessCookieName: "sessionid",
	})
	if err != nil {
		t.Fatalf("Login: %v", err)
	}
	if len(cookies) == 0 {
		t.Fatal("no cookies returned")
	}
	var found bool
	for _, c := range cookies {
		if c.Name == "sessionid" && c.Value == "xyz" {
			found = true
		}
	}
	if !found {
		t.Fatalf("sessionid cookie missing in result: %+v", cookies)
	}
}

func TestFormLogin_Cache(t *testing.T) {
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "v", Path: "/"})
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	cfg := FormLoginConfig{
		LoginURL: srv.URL, Method: "POST",
		UserField: "u", PassField: "p", User: "a", Password: "b",
		SuccessCookieName: "sessionid",
	}
	for i := 0; i < 3; i++ {
		if _, err := cache.Login(context.Background(), "same-name", cfg); err != nil {
			t.Fatal(err)
		}
	}
	if hits != 1 {
		t.Errorf("login called %d times, want 1 (cache miss)", hits)
	}
}

func TestFormLogin_FailureNoCookie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// no Set-Cookie
		w.WriteHeader(200)
		_, _ = w.Write([]byte("bad creds"))
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	_, err := cache.Login(context.Background(), "x", FormLoginConfig{
		LoginURL: srv.URL, Method: "POST",
		UserField: "u", PassField: "p", User: "a", Password: "b",
		SuccessCookieName: "sessionid",
	})
	if err == nil {
		t.Fatal("expected error when success cookie missing")
	}
	if !strings.Contains(err.Error(), "sessionid") {
		t.Errorf("error should mention missing cookie: %v", err)
	}
}

func TestFormLogin_ConcurrentFirstLoginDedups(t *testing.T) {
	var (
		hits int32
		gate = make(chan struct{}) // ensure all goroutines pile up before server unblocks
	)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&hits, 1)
		<-gate
		http.SetCookie(w, &http.Cookie{Name: "sessionid", Value: "v", Path: "/"})
		w.WriteHeader(200)
	}))
	defer srv.Close()

	cache := NewFormLoginCache()
	cfg := FormLoginConfig{
		LoginURL: srv.URL, Method: "POST",
		UserField: "u", PassField: "p", User: "a", Password: "b",
		SuccessCookieName: "sessionid",
	}

	const N = 10
	var wg sync.WaitGroup
	wg.Add(N)
	errCh := make(chan error, N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			if _, err := cache.Login(context.Background(), "concurrent-key", cfg); err != nil {
				errCh <- err
			}
		}()
	}
	// Small delay to ensure goroutines have arrived at the singleflight gate.
	time.Sleep(50 * time.Millisecond)
	close(gate)
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Fatalf("login error: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("server saw %d login POSTs under concurrent first-login; want 1", got)
	}
}
