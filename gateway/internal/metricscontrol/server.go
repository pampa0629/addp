// Package metricscontrol owns the Gateway's optional private control listener.
package metricscontrol

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	commonConfig "github.com/addp/common/config"
	"github.com/addp/common/logger"
	"github.com/addp/gateway/internal/config"
)

var ErrUnavailable = errors.New("gateway_metrics_control_unavailable")

type Server struct {
	http *http.Server
	done chan struct{}
}

func certificateFile(directory, name string) ([]byte, error) {
	file, err := os.Open(filepath.Join(directory, name))
	if err != nil {
		return nil, ErrUnavailable
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() {
		return nil, ErrUnavailable
	}
	data, err := io.ReadAll(io.LimitReader(file, (256<<10)+1))
	if err != nil || len(data) == 0 || len(data) > 256<<10 {
		return nil, ErrUnavailable
	}
	return data, nil
}

func controlTLS(cfg *config.Config) (*tls.Config, error) {
	if !filepath.IsAbs(cfg.MetricsControlTLSDir) {
		return nil, ErrUnavailable
	}
	directory, err := filepath.EvalSymlinks(cfg.MetricsControlTLSDir)
	if err != nil {
		return nil, ErrUnavailable
	}
	if root := commonConfig.ProjectRoot(); root != "" {
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, ErrUnavailable
		}
		relative, err := filepath.Rel(root, directory)
		if err != nil || relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
			return nil, ErrUnavailable
		}
	}
	cert, err := certificateFile(directory, "server.crt")
	if err != nil {
		return nil, err
	}
	key, err := certificateFile(directory, "server.key")
	if err != nil {
		return nil, err
	}
	ca, err := certificateFile(directory, "client-ca.crt")
	if err != nil {
		return nil, err
	}
	pair, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, ErrUnavailable
	}
	leaf, err := x509.ParseCertificate(pair.Certificate[0])
	if err != nil || leaf.IsCA || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) || cfg.MetricsControlServerName == "" || leaf.VerifyHostname(cfg.MetricsControlServerName) != nil {
		return nil, ErrUnavailable
	}
	serverAuth := false
	for _, usage := range leaf.ExtKeyUsage {
		serverAuth = serverAuth || usage == x509.ExtKeyUsageServerAuth
	}
	roots := x509.NewCertPool()
	if !serverAuth || !roots.AppendCertsFromPEM(ca) {
		return nil, ErrUnavailable
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots}, nil
}

func handler(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method := ""
		switch r.URL.EscapedPath() {
		case "/api/v1/system/oauth/token":
			method = http.MethodPost
		case "/api/v1/monitor/platform/metrics_discovery":
			method = http.MethodGet
		default:
			http.NotFound(w, r)
			return
		}
		if r.Method != method {
			w.Header().Set("Allow", method)
			http.Error(w, http.StatusText(http.StatusMethodNotAllowed), http.StatusMethodNotAllowed)
			return
		}
		if r.URL.RawQuery != "" || r.URL.ForceQuery {
			http.Error(w, http.StatusText(http.StatusBadRequest), http.StatusBadRequest)
			return
		}
		body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 8192))
		if err != nil {
			status := http.StatusBadRequest
			var oversized *http.MaxBytesError
			if errors.As(err, &oversized) {
				status = http.StatusRequestEntityTooLarge
			}
			http.Error(w, http.StatusText(status), status)
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))
		ctx, cancel := context.WithTimeout(r.Context(), 5*time.Second)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// Start leaves the main HTTP listener independent of optional deployment failures.
func Start(cfg *config.Config, next http.Handler) (*Server, error) {
	if cfg.MetricsControlListen == "" {
		return nil, nil
	}
	host, port, err := net.SplitHostPort(cfg.MetricsControlListen)
	if err != nil || net.ParseIP(host) == nil {
		return nil, ErrUnavailable
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 1 || number > 65535 || strconv.Itoa(number) != port {
		return nil, ErrUnavailable
	}
	tlsConfig, err := controlTLS(cfg)
	if err != nil {
		return nil, err
	}
	listener, err := net.Listen("tcp", cfg.MetricsControlListen)
	if err != nil {
		return nil, ErrUnavailable
	}
	server := &Server{http: &http.Server{Handler: handler(next), TLSConfig: tlsConfig, ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192, ErrorLog: log.New(io.Discard, "", 0)}, done: make(chan struct{})}
	go func() {
		defer close(server.done)
		if err := server.http.ServeTLS(listener, "", ""); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.L().Error("gateway_metrics_control_serve_failed")
		}
	}()
	return server, nil
}

func (s *Server) Shutdown(ctx context.Context) error {
	err := s.http.Shutdown(ctx)
	if err != nil {
		_ = s.http.Close()
	}
	<-s.done
	return err
}
