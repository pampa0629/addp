// Hosted-only timing fixture. Control files live in the suite's private directory.
package main

import (
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"path/filepath"
	"time"
)

func forwardMeta(upstream *url.URL, dir string) http.Handler {
	proxy := &httputil.ReverseProxy{
		Rewrite:  func(req *httputil.ProxyRequest) { req.SetURL(upstream) },
		ErrorLog: log.New(io.Discard, "", 0),
	}
	proxy.ModifyResponse = func(res *http.Response) error {
		req := res.Request
		if req.Method != http.MethodGet || res.StatusCode != http.StatusOK || req.URL.Path != "/api/v1/meta/items/by-catalog-path" {
			return nil
		}
		q := req.URL.Query()
		target := q.Get("engine_id") + "\n" + q.Get("catalog_path")
		armed := filepath.Join(dir, "armed")
		data, err := os.ReadFile(armed)
		if err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
		if string(data) != target || err != nil {
			return nil
		}
		// Only one matching response claims the gate, even for simultaneous reads.
		if err := os.Rename(armed, filepath.Join(dir, "held")); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		deadline := time.NewTimer(45 * time.Second)
		defer deadline.Stop()
		tick := time.NewTicker(50 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-req.Context().Done():
				return req.Context().Err()
			case <-deadline.C:
				return errors.New("fixture response hold expired")
			case <-tick.C:
				if _, err := os.Stat(filepath.Join(dir, "released")); err == nil {
					return os.Rename(filepath.Join(dir, "held"), filepath.Join(dir, "forwarded"))
				} else if !errors.Is(err, os.ErrNotExist) {
					return err
				}
			}
		}
	}
	return proxy
}

func main() {
	dir := os.Args[1]
	// The destination is fixed. Clients cannot select a host or control the gate over HTTP.
	upstream, _ := url.Parse("http://127.0.0.1:8082")
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		log.Fatal("cannot bind Hosted Meta fixture")
	}
	if err := os.WriteFile(filepath.Join(dir, "url.tmp"), []byte("http://"+listener.Addr().String()), 0600); err != nil {
		log.Fatal("cannot write Hosted Meta fixture URL")
	}
	if err := os.Rename(filepath.Join(dir, "url.tmp"), filepath.Join(dir, "url")); err != nil {
		log.Fatal("cannot publish Hosted Meta fixture URL")
	}
	server := http.Server{Handler: forwardMeta(upstream, dir), ReadHeaderTimeout: 5 * time.Second, ErrorLog: log.New(io.Discard, "", 0)}
	if err := server.Serve(listener); err != nil {
		log.Fatal("Hosted Meta fixture stopped")
	}
}
