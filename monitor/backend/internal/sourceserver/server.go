// Package sourceserver owns the fixed, private HTTP/TLS boundary of deployment sources.
package sourceserver

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"
)

// Handler buffers a complete projection before writing; failed reads never retain data.
func Handler(read func(context.Context) ([]byte, error)) http.Handler {
	busy := make(chan struct{}, 1)
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.Method != http.MethodGet || r.URL.EscapedPath() != "/metrics" || r.URL.RawQuery != "" || r.URL.ForceQuery || r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			http.Error(w, "Metrics request rejected", http.StatusNotFound)
			return
		}
		select {
		case busy <- struct{}{}:
			defer func() { <-busy }()
		default:
			http.Error(w, "Metrics source busy", http.StatusServiceUnavailable)
			return
		}
		body, err := read(r.Context())
		if err != nil || len(body) == 0 || len(body) > 256*1024 {
			http.Error(w, "Metrics source unavailable", http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write(body)
	})
}

func New(handler http.Handler, directory string) (*http.Server, error) {
	key, err := tls.LoadX509KeyPair(filepath.Join(directory, "server.crt"), filepath.Join(directory, "server.key"))
	if err != nil {
		return nil, errors.New("source certificate unavailable")
	}
	ca, err := os.ReadFile(filepath.Join(directory, "ca.crt"))
	if err != nil {
		return nil, errors.New("source client trust unavailable")
	}
	pool := x509.NewCertPool()
	if !pool.AppendCertsFromPEM(ca) {
		return nil, errors.New("source client trust rejected")
	}
	return &http.Server{
		Addr: ":9443", Handler: handler,
		ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 4 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 5 * time.Second,
		MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0),
		TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: pool, Certificates: []tls.Certificate{key}},
	}, nil
}

func Serve(handler http.Handler, directory string) error {
	server, err := New(handler, directory)
	if err != nil {
		return err
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	go func() {
		<-ctx.Done()
		deadline, done := context.WithTimeout(context.Background(), 5*time.Second)
		defer done()
		_ = server.Shutdown(deadline)
	}()
	if err = server.ListenAndServeTLS("", ""); errors.Is(err, http.ErrServerClosed) {
		return nil
	}
	return errors.New("source listener unavailable")
}
