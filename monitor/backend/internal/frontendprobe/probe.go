// Package frontendprobe checks the public entry and application script of the
// module frontends, using a deployment-owned origin and a closed path set.
package frontendprobe

import (
	"context"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"golang.org/x/net/html"
)

// Modules is the closed set of public ADDP frontend identities.
var Modules = [...]string{"console", "agent", "asset", "catalog", "develop", "graph", "inference",
	"manager", "meta", "model", "monitor", "ontology", "orchestrator", "portal", "quality",
	"security", "service", "standard", "system", "transfer", "workbench"}

type Result struct {
	Entry bool
	Asset *bool
}

type Prober struct {
	origin *url.URL
	client *http.Client
}

func New(origin *url.URL) *Prober {
	return &Prober{origin: origin, client: &http.Client{
		Transport: &http.Transport{
			DialContext: (&net.Dialer{Timeout: time.Second}).DialContext, ResponseHeaderTimeout: time.Second, MaxResponseHeaderBytes: 8192,
		},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}}
}

var entryScript = regexp.MustCompile(`^(assets/[A-Za-z0-9_.-]+\.js|src/main\.(js|ts)(\?t=[0-9]+)?)$`)

func (p *Prober) Check(ctx context.Context, entryPath string) Result {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	entry, contentType := p.read(ctx, entryPath, 128<<10)
	if contentType != "text/html" || entry == "" {
		return Result{}
	}
	app, asset := entryParts(entry, entryPath)
	if !app {
		return Result{}
	}
	js := false
	if asset != "" {
		body, contentType := p.read(ctx, asset, 4096)
		js = body != "" && (contentType == "text/javascript" || contentType == "application/javascript") &&
			!strings.HasPrefix(strings.TrimSpace(body), "<")
	}
	return Result{Entry: true, Asset: &js}
}

func (p *Prober) read(ctx context.Context, path string, limit int64) (string, string) {
	target := *p.origin
	target.Path, target.RawQuery, _ = strings.Cut(path, "?")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, target.String(), nil)
	if err != nil {
		return "", ""
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return "", ""
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", ""
	}
	contentType, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type"))
	body, err := io.ReadAll(io.LimitReader(resp.Body, limit))
	if err != nil {
		return "", ""
	}
	return string(body), contentType
}

func entryParts(body, entryPath string) (app bool, asset string) {
	t := html.NewTokenizer(strings.NewReader(body))
	for tt := t.Next(); tt != html.ErrorToken; tt = t.Next() {
		if tt != html.StartTagToken {
			continue
		}
		token := t.Token()
		switch token.Data {
		case "div":
			for _, attr := range token.Attr {
				app = app || attr.Key == "id" && attr.Val == "app"
			}
		case "script":
			var src string
			var module bool
			for _, attr := range token.Attr {
				if attr.Key == "src" {
					src = attr.Val
				}
				module = module || attr.Key == "type" && attr.Val == "module"
			}
			if asset == "" && module && strings.HasPrefix(src, entryPath) && entryScript.MatchString(strings.TrimPrefix(src, entryPath)) {
				asset = src
			}
		}
	}
	return app, asset
}
