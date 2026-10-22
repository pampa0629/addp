package auditarchive

import (
	"context"
	"crypto/tls"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"time"
)

// ServeReceiver serves only TLS. It is deliberately outside ADDP's shared
// lifecycle and never loads root .env or registers with Gateway.
func ServeReceiver(ctx context.Context, c Config, s Store) error {
	if c.Validate() != nil || c.Mode != "receiver" || !c.RootValid() {
		return ErrUnknown
	}
	keyStat, e := os.Lstat(c.TLSKey)
	if e != nil || !keyStat.Mode().IsRegular() || keyStat.Mode().Perm()&0077 != 0 {
		return ErrUnknown
	}
	cert, e := tls.LoadX509KeyPair(c.TLSCert, c.TLSKey)
	if e != nil {
		return ErrUnknown
	}
	receiver, e := NewReceiver(s, c.SourceID, c.DeploymentID, c.ReceiverToken, c.Root, c.Checkpoint, c.RetentionDays)
	if e != nil {
		return e
	}
	// Do not accept traffic until the configured, externally trusted anchor works.
	verifyCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	_, e = VerifyReceiver(verifyCtx, s, c.SourceID, c.DeploymentID, c.Root, c.Checkpoint)
	cancel()
	if e != nil {
		return e
	}
	server := &http.Server{Addr: c.Listen, Handler: receiver, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 20 * time.Second, WriteTimeout: 25 * time.Second, IdleTimeout: 30 * time.Second, MaxHeaderBytes: 16384, TLSConfig: &tls.Config{MinVersion: tls.VersionTLS13, Certificates: []tls.Certificate{cert}}, ErrorLog: log.New(io.Discard, "", 0)}
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			shutdown, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			_ = server.Shutdown(shutdown)
		case <-done:
		}
	}()
	e = server.ListenAndServeTLS("", "")
	close(done)
	if errors.Is(e, http.ErrServerClosed) && ctx.Err() != nil {
		return nil
	}
	return ErrUnknown
}
