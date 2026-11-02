// Package miniosource projects only fixed MinIO v3 operational statistics.
package miniosource

import (
	"context"
	"crypto/hmac"
	"crypto/sha512"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/netip"
	"regexp"
	"time"
)

var ErrUnavailable = errors.New("MinIO observation unavailable")
var paths = []string{"system/process", "cluster/health", "cluster/usage/objects", "api/requests"}

const MaxResponseBytes = 256 * 1024

type Target struct {
	Address            netip.AddrPort
	Scope              string
	AccessKey          string
	SecretKey          string
	ContainerID        string
	ContainerStartedAt time.Time
}

func (t Target) Valid() bool {
	a := t.Address.Addr()
	return t.Address.IsValid() && t.Address.Port() == 9000 && !a.Is4In6() && a.Zone() == "" && !a.IsUnspecified() &&
		(a.IsLoopback() || a.IsPrivate()) && (t.Scope == "infra" || t.Scope == "business") &&
		regexp.MustCompile(`^ADDP[A-F0-9]{16}$`).MatchString(t.AccessKey) && regexp.MustCompile(`^[!-~]{8,128}$`).MatchString(t.SecretKey) &&
		regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(t.ContainerID) && !t.ContainerStartedAt.IsZero() && !t.ContainerStartedAt.After(time.Now())
}

func token(t Target, now time.Time) string {
	enc := base64.RawURLEncoding.EncodeToString
	claims, _ := json.Marshal(map[string]any{"iss": "prometheus", "sub": t.AccessKey, "exp": now.Unix() + 300})
	body := enc([]byte(`{"alg":"HS512","typ":"JWT"}`)) + "." + enc(claims)
	mac := hmac.New(sha512.New, []byte(t.SecretKey))
	_, _ = mac.Write([]byte(body))
	return body + "." + enc(mac.Sum(nil))
}

// Read has one total deadline and no proxy, DNS, redirect, retry or stale cache.
func Read(ctx context.Context, target Target) (Snapshot, error) {
	if !target.Valid() {
		return Snapshot{}, ErrUnavailable
	}
	transport := &http.Transport{Proxy: nil, DialContext: (&net.Dialer{Timeout: 3 * time.Second}).DialContext,
		DisableKeepAlives: true, MaxConnsPerHost: 1, ResponseHeaderTimeout: 3 * time.Second, MaxResponseHeaderBytes: 8192}
	defer transport.CloseIdleConnections()
	client := &http.Client{Transport: transport, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	return read(ctx, target, client)
}

func read(ctx context.Context, target Target, client *http.Client) (Snapshot, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	authorization := "Bearer " + token(target, time.Now())
	fetch := func(path string) ([]byte, error) {
		if ctx.Err() != nil {
			return nil, ErrUnavailable
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+target.Address.String()+"/minio/metrics/v3/"+path, nil)
		if err != nil {
			return nil, ErrUnavailable
		}
		req.Header.Set("Authorization", authorization)
		req.Header.Set("Accept", "text/plain; version=0.0.4")
		response, err := client.Do(req)
		if err != nil {
			return nil, ErrUnavailable
		}
		defer response.Body.Close()
		if response.StatusCode != 200 {
			return nil, ErrUnavailable
		}
		body, err := io.ReadAll(io.LimitReader(response.Body, MaxResponseBytes+1))
		if err != nil || len(body) > MaxResponseBytes {
			return nil, ErrUnavailable
		}
		return body, nil
	}
	input := make(map[string][]byte)
	var scanAt time.Time
	for _, path := range paths {
		body, err := fetch(path)
		if err != nil {
			return Snapshot{}, ErrUnavailable
		}
		input[path] = body
		if path == "cluster/usage/objects" {
			scanAt = time.Now()
		}
	}
	boundary, err := fetch(paths[0])
	if err != nil {
		return Snapshot{}, ErrUnavailable
	}
	snapshot, err := project(input, boundary, target, time.Now(), scanAt)
	if ctx.Err() != nil {
		return Snapshot{}, ErrUnavailable
	}
	return snapshot, err
}
