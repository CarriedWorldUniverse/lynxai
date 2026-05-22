// Package engine wraps a headless Chromium (via chromedp) for lynxai's fetch
// operations. Pages are fetched, optionally with credentials applied, and the
// resulting HTML is converted to cleaned markdown for agent consumption.
package engine

import (
	"fmt"
	"strings"

	htmltomd "github.com/JohannesKaufmann/html-to-markdown/v2/converter"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/base"
	"github.com/JohannesKaufmann/html-to-markdown/v2/plugin/commonmark"
	"golang.org/x/net/html"
)

// chromeTags are elements removed by default when includeChrome=false.
var chromeTags = []string{"nav", "header", "footer", "aside"}

// stripTags are elements always removed.
var stripTags = []string{"script", "style", "noscript", "svg", "template"}

// HTMLToMarkdown converts HTML to cleaned markdown. When includeChrome is false,
// nav/header/footer/aside elements are also stripped.
func HTMLToMarkdown(htmlSrc string, includeChrome bool) (string, error) {
	cleaned, err := stripElements(htmlSrc, includeChrome)
	if err != nil {
		return "", fmt.Errorf("strip: %w", err)
	}
	conv := htmltomd.NewConverter(
		htmltomd.WithPlugins(base.NewBasePlugin(), commonmark.NewCommonmarkPlugin()),
	)
	md, err := conv.ConvertString(cleaned)
	if err != nil {
		return "", fmt.Errorf("convert: %w", err)
	}
	return md, nil
}

func stripElements(htmlSrc string, includeChrome bool) (string, error) {
	doc, err := html.Parse(strings.NewReader(htmlSrc))
	if err != nil {
		return "", err
	}
	toStrip := append([]string{}, stripTags...)
	if !includeChrome {
		toStrip = append(toStrip, chromeTags...)
	}
	stripSet := map[string]bool{}
	for _, t := range toStrip {
		stripSet[t] = true
	}
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		var next *html.Node
		for c := n.FirstChild; c != nil; c = next {
			next = c.NextSibling
			if c.Type == html.ElementNode && stripSet[c.Data] {
				n.RemoveChild(c)
				continue
			}
			walk(c)
		}
	}
	walk(doc)
	var sb strings.Builder
	if err := html.Render(&sb, doc); err != nil {
		return "", err
	}
	return sb.String(), nil
}
