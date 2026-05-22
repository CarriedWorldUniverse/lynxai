package engine

import (
	"context"
	"fmt"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

// FormLoginConfig is the input to FormLoginCache.Login.
type FormLoginConfig struct {
	LoginURL          string
	Method            string // "POST" only in v1
	UserField         string
	PassField         string
	User              string
	Password          string
	SuccessCookieName string
}

// FormLoginCache caches the cookies obtained from form logins, keyed by
// credential name. Cache lives for the process lifetime only — persisted
// contexts come in a later spec.
type FormLoginCache struct {
	mu     sync.Mutex
	by     map[string][]CredCookie
	client *http.Client
	sf     singleflight.Group
}

func NewFormLoginCache() *FormLoginCache {
	return &FormLoginCache{
		by:     map[string][]CredCookie{},
		client: &http.Client{Timeout: 30 * time.Second},
	}
}

// Login returns the cached cookies for credName if present, otherwise performs
// the form-login POST and caches the result.
func (c *FormLoginCache) Login(ctx context.Context, credName string, cfg FormLoginConfig) ([]CredCookie, error) {
	// Fast-path: already cached.
	c.mu.Lock()
	if cookies, ok := c.by[credName]; ok {
		c.mu.Unlock()
		return cookies, nil
	}
	c.mu.Unlock()

	// Deduplicate concurrent first-logins per credName.
	v, err, _ := c.sf.Do(credName, func() (any, error) {
		// Re-check in case another caller populated while we waited.
		c.mu.Lock()
		if cookies, ok := c.by[credName]; ok {
			c.mu.Unlock()
			return cookies, nil
		}
		c.mu.Unlock()

		jar, err := cookiejar.New(nil)
		if err != nil {
			return nil, fmt.Errorf("cookiejar: %w", err)
		}
		cli := *c.client
		cli.Jar = jar

		body := url.Values{}
		body.Set(cfg.UserField, cfg.User)
		body.Set(cfg.PassField, cfg.Password)

		method := cfg.Method
		if method == "" {
			method = "POST"
		}
		req, err := http.NewRequestWithContext(ctx, method, cfg.LoginURL, strings.NewReader(body.Encode()))
		if err != nil {
			return nil, fmt.Errorf("build request: %w", err)
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

		resp, err := cli.Do(req)
		if err != nil {
			return nil, fmt.Errorf("login POST: %w", err)
		}
		defer resp.Body.Close()

		u, err := url.Parse(cfg.LoginURL)
		if err != nil {
			return nil, fmt.Errorf("parse login url: %w", err)
		}

		// Collect cookies the jar accumulated (covers Set-Cookie + any redirects).
		jarCookies := jar.Cookies(u)
		var found bool
		out := make([]CredCookie, 0, len(jarCookies))
		for _, ck := range jarCookies {
			if ck.Name == cfg.SuccessCookieName {
				found = true
			}
			out = append(out, CredCookie{
				Name: ck.Name, Value: ck.Value,
				Domain: ck.Domain, Path: ck.Path,
				Secure: ck.Secure, HTTPOnly: ck.HttpOnly,
			})
		}
		if !found {
			return nil, fmt.Errorf("form login: success cookie %q not in response (HTTP %d)", cfg.SuccessCookieName, resp.StatusCode)
		}

		c.mu.Lock()
		c.by[credName] = out
		c.mu.Unlock()
		return out, nil
	})
	if err != nil {
		return nil, err
	}
	return v.([]CredCookie), nil
}

// Invalidate drops the cached cookies for a credential (e.g., on 401 retry).
func (c *FormLoginCache) Invalidate(credName string) {
	c.mu.Lock()
	delete(c.by, credName)
	c.mu.Unlock()
}
