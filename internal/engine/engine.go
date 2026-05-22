package engine

import (
	"context"
	"fmt"
	"time"

	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

// Config tunes engine behavior.
type Config struct {
	PoolSize    int           // currently unused (single allocator); reserved for v1.1
	DefaultWait time.Duration // default per-request timeout if FetchRequest.Timeout == 0
}

// FetchRequest is the input to Engine.Fetch.
type FetchRequest struct {
	URL           string
	Credential    *AppliedCredential // pre-resolved by the api layer; nil = unauth
	IncludeChrome bool
	Timeout       time.Duration // 0 = use Config.DefaultWait
}

// AppliedCredential is the engine-level view of a credential — the api layer
// resolves a name+kind into one of these and hands it in.
type AppliedCredential struct {
	Kind    CredKind
	Headers map[string]string // for basic/bearer
	Cookies []CredCookie      // for cookies / form (after login)
}

// CredKind is a flat enum so engine doesn't depend on internal/creds.
type CredKind string

const (
	CredBasic   CredKind = "basic"
	CredBearer  CredKind = "bearer"
	CredCookies CredKind = "cookies"
	CredForm    CredKind = "form"
)

type CredCookie struct {
	Name, Value, Domain, Path string
	Secure, HTTPOnly          bool
}

// FetchResult is the response.
type FetchResult struct {
	Markdown string `json:"markdown"`
	Status   int    `json:"status"`
	FinalURL string `json:"final_url"`
	Title    string `json:"title"`
}

// Engine owns the chromedp allocator and runs fetches.
type Engine struct {
	cfg       Config
	allocCtx  context.Context
	allocDone context.CancelFunc
}

// New constructs an Engine, starting the browser allocator. Caller must Close.
func New(cfg Config) (*Engine, error) {
	if cfg.DefaultWait == 0 {
		cfg.DefaultWait = 30 * time.Second
	}
	opts := append(chromedp.DefaultExecAllocatorOptions[:],
		chromedp.Flag("headless", true),
		chromedp.Flag("disable-gpu", true),
		chromedp.Flag("no-sandbox", true),
	)
	ctx, cancel := chromedp.NewExecAllocator(context.Background(), opts...)
	return &Engine{cfg: cfg, allocCtx: ctx, allocDone: cancel}, nil
}

// Close shuts the allocator down.
func (e *Engine) Close() error {
	e.allocDone()
	return nil
}

// Fetch navigates to req.URL and returns cleaned markdown.
// Status is currently hard-coded to 200; chromedp's Navigate doesn't readily
// expose the HTTP status. A future task can wire a network listener to capture it.
func (e *Engine) Fetch(ctx context.Context, req FetchRequest) (*FetchResult, error) {
	if req.URL == "" {
		return nil, fmt.Errorf("engine.Fetch: URL required")
	}
	timeout := req.Timeout
	if timeout == 0 {
		timeout = e.cfg.DefaultWait
	}

	browserCtx, cancel := chromedp.NewContext(e.allocCtx)
	defer cancel()
	fetchCtx, cancel2 := context.WithTimeout(browserCtx, timeout)
	defer cancel2()

	var (
		status   int64 = 200
		finalURL string
		title    string
		htmlSrc  string
	)

	actions := []chromedp.Action{}
	if req.Credential != nil {
		actions = append(actions, applyCredentialActions(req.Credential)...)
	}
	actions = append(actions,
		chromedp.Navigate(req.URL),
		chromedp.WaitReady("body"),
		chromedp.Location(&finalURL),
		chromedp.Title(&title),
		chromedp.OuterHTML("html", &htmlSrc, chromedp.ByQuery),
	)

	if err := chromedp.Run(fetchCtx, actions...); err != nil {
		return nil, fmt.Errorf("navigate: %w", err)
	}

	md, err := HTMLToMarkdown(htmlSrc, req.IncludeChrome)
	if err != nil {
		return nil, fmt.Errorf("markdown: %w", err)
	}

	return &FetchResult{
		Markdown: md,
		Status:   int(status),
		FinalURL: finalURL,
		Title:    title,
	}, nil
}

// applyCredentialActions returns chromedp actions to apply a pre-resolved credential.
func applyCredentialActions(c *AppliedCredential) []chromedp.Action {
	var acts []chromedp.Action
	if len(c.Headers) > 0 {
		hdrs := network.Headers{}
		for k, v := range c.Headers {
			hdrs[k] = v
		}
		acts = append(acts, network.SetExtraHTTPHeaders(hdrs))
	}
	if len(c.Cookies) > 0 {
		params := make([]*network.CookieParam, 0, len(c.Cookies))
		for _, ck := range c.Cookies {
			params = append(params, &network.CookieParam{
				Name:     ck.Name,
				Value:    ck.Value,
				Domain:   ck.Domain,
				Path:     ck.Path,
				Secure:   ck.Secure,
				HTTPOnly: ck.HTTPOnly,
			})
		}
		acts = append(acts, network.SetCookies(params))
	}
	return acts
}
