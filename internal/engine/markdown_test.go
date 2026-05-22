package engine

import (
	"strings"
	"testing"
)

func TestHTMLToMarkdown_BasicText(t *testing.T) {
	md, err := HTMLToMarkdown(`<html><body><h1>Hello</h1><p>World</p></body></html>`, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "# Hello") {
		t.Errorf("missing h1: %q", md)
	}
	if !strings.Contains(md, "World") {
		t.Errorf("missing p text: %q", md)
	}
}

func TestHTMLToMarkdown_StripsScriptAndStyle(t *testing.T) {
	html := `<html><body><script>alert('x')</script><style>p{}</style><p>visible</p></body></html>`
	md, err := HTMLToMarkdown(html, false)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(md, "alert") {
		t.Errorf("script content leaked: %q", md)
	}
	if strings.Contains(md, "p{}") {
		t.Errorf("style content leaked: %q", md)
	}
	if !strings.Contains(md, "visible") {
		t.Errorf("body content missing: %q", md)
	}
}

func TestHTMLToMarkdown_StripsChromeByDefault(t *testing.T) {
	html := `<html><body><nav>NAV</nav><header>HDR</header><main><p>BODY</p></main><footer>FOOT</footer></body></html>`
	md, err := HTMLToMarkdown(html, false)
	if err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"NAV", "HDR", "FOOT"} {
		if strings.Contains(md, s) {
			t.Errorf("chrome %s should be stripped: %q", s, md)
		}
	}
	if !strings.Contains(md, "BODY") {
		t.Errorf("main content missing: %q", md)
	}
}

func TestHTMLToMarkdown_IncludeChromeKeepsThem(t *testing.T) {
	html := `<html><body><nav>NAV</nav><main><p>BODY</p></main></body></html>`
	md, err := HTMLToMarkdown(html, true)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "NAV") {
		t.Errorf("chrome should be kept with includeChrome=true: %q", md)
	}
}

func TestHTMLToMarkdown_Links(t *testing.T) {
	md, err := HTMLToMarkdown(`<a href="https://example.com">click</a>`, false)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(md, "https://example.com") {
		t.Errorf("link URL missing: %q", md)
	}
	if !strings.Contains(md, "click") {
		t.Errorf("link text missing: %q", md)
	}
}
