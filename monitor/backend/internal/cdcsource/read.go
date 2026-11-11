// Package cdcsource reads bounded, fixed CDC component signals without exporting identities or payloads.
package cdcsource

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"sort"
	"time"

	"github.com/addp/monitor/internal/sourceserver"
)

const MaxResponseBytes = 1024 * 1024

var ErrUnavailable = errors.New("CDC observation unavailable")

// Target is supplied by the deployment owner, never by a scrape request.
type Target struct {
	Kind    string     `json:"kind"`
	Address netip.Addr `json:"address"`
}

func (t Target) Valid() bool {
	a := t.Address
	return (t.Kind == "redpanda" || t.Kind == "kafka-connect") && a.IsValid() && !a.Is4In6() && a.Zone() == "" && (a.IsPrivate() || a.IsLoopback())
}

// Read shares one deadline across every upstream read; redirects and proxies are forbidden.
func Read(ctx context.Context, target Target) ([]byte, error) {
	if !target.Valid() {
		return nil, ErrUnavailable
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: time.Second}).DialContext,
		DisableKeepAlives: true, MaxConnsPerHost: 1, ResponseHeaderTimeout: 2 * time.Second, MaxResponseHeaderBytes: 8192}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return read(ctx, target, client)
}

func Handler(target Target) http.Handler {
	return sourceserver.Handler(func(ctx context.Context) ([]byte, error) { return Read(ctx, target) })
}

func read(ctx context.Context, target Target, client *http.Client) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()
	fetch := func(port uint16, path string, body []byte) ([]byte, error) {
		method := http.MethodGet
		if body != nil {
			method = http.MethodPost
		}
		req, err := http.NewRequestWithContext(ctx, method, "http://"+netip.AddrPortFrom(target.Address, port).String()+path, bytes.NewReader(body))
		if err != nil {
			return nil, ErrUnavailable
		}
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		if err != nil {
			return nil, ErrUnavailable
		}
		defer resp.Body.Close()
		// Jolokia represents an empty wildcard match as a typed 404 response.
		if resp.StatusCode != http.StatusOK && !(port == 8778 && resp.StatusCode == http.StatusNotFound) {
			return nil, ErrUnavailable
		}
		raw, err := io.ReadAll(io.LimitReader(resp.Body, MaxResponseBytes+1))
		if err != nil || len(raw) > MaxResponseBytes {
			return nil, ErrUnavailable
		}
		return raw, nil
	}
	var values map[string]float64
	var err error
	if target.Kind == "redpanda" {
		var raw []byte
		raw, err = fetch(9644, "/public_metrics", nil)
		if err == nil {
			values, err = broker(raw)
		}
	} else {
		values, err = connect(fetch)
	}
	if err != nil || ctx.Err() != nil {
		return nil, ErrUnavailable
	}
	prefix := "addp_cdc_redpanda_"
	if target.Kind == "kafka-connect" {
		prefix = "addp_cdc_connect_"
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var out bytes.Buffer
	fmt.Fprintf(&out, "%sobserved_at_seconds %d\n", prefix, time.Now().Unix())
	for _, key := range keys {
		fmt.Fprintf(&out, "%s%s %g\n", prefix, key, values[key])
	}
	return out.Bytes(), nil
}

// JSON is decoded once at the upstream boundary; error text is never forwarded.
func decode(raw []byte, dst any) error {
	if err := json.Unmarshal(raw, dst); err != nil {
		return ErrUnavailable
	}
	return nil
}
