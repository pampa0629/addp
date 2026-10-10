package processmetrics

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/config"
	"github.com/addp/common/models"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var ErrSourceUnavailable = errors.New("process_metrics_source_unavailable")

type Identity struct {
	ModuleName, Role, InstanceID string
	StartedAt                    time.Time
}

// Server owns one private listener and one registry for the current process.
type Server struct {
	http        *http.Server
	done        chan struct{}
	declaration models.ProcessMetricsDeclaration
}

func (s *Server) Declaration() *models.ProcessMetricsDeclaration { copy := s.declaration; return &copy }
func (s *Server) Close() error                                   { err := s.http.Close(); <-s.done; return err }

func readCertificateFile(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.Mode().IsRegular() || info.Size() < 1 || info.Size() > 256<<10 {
		return nil, ErrSourceUnavailable
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
	if err != nil || len(data) > 256<<10 {
		return nil, ErrSourceUnavailable
	}
	return data, nil
}

func sourceTLS(d config.ProcessMetricsDeployment) (*tls.Config, error) {
	if root := config.ProjectRoot(); root != "" {
		resolved, err := filepath.EvalSymlinks(d.TLSDir)
		if err != nil {
			return nil, ErrSourceUnavailable
		}
		root, err = filepath.EvalSymlinks(root)
		if err != nil {
			return nil, ErrSourceUnavailable
		}
		rel, err := filepath.Rel(root, resolved)
		if err != nil || rel == "." || (rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))) {
			return nil, ErrSourceUnavailable
		}
	}
	info, err := os.Lstat(d.TLSDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrSourceUnavailable
	}
	files, err := os.ReadDir(d.TLSDir)
	if err != nil || len(files) != 3 {
		return nil, ErrSourceUnavailable
	}
	for _, f := range files {
		switch f.Name() {
		case "ca.crt", "server.crt", "server.key":
		default:
			return nil, ErrSourceUnavailable
		}
	}
	ca, err := readCertificateFile(filepath.Join(d.TLSDir, "ca.crt"))
	if err != nil {
		return nil, err
	}
	cert, err := readCertificateFile(filepath.Join(d.TLSDir, "server.crt"))
	if err != nil {
		return nil, err
	}
	key, err := readCertificateFile(filepath.Join(d.TLSDir, "server.key"))
	if err != nil {
		return nil, err
	}
	certificate, err := tls.X509KeyPair(cert, key)
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	leaf, err := x509.ParseCertificate(certificate.Certificate[0])
	if err != nil || leaf.IsCA || time.Now().Before(leaf.NotBefore) || !time.Now().Before(leaf.NotAfter) {
		return nil, ErrSourceUnavailable
	}
	serverAuth := false
	for _, use := range leaf.ExtKeyUsage {
		serverAuth = serverAuth || use == x509.ExtKeyUsageServerAuth
	}
	endpoint, _ := url.Parse(d.Endpoint)
	if !serverAuth || leaf.VerifyHostname(endpoint.Hostname()) != nil {
		return nil, ErrSourceUnavailable
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, ErrSourceUnavailable
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, ClientAuth: tls.RequireAndVerifyClientCert, ClientCAs: roots, Certificates: []tls.Certificate{certificate}}, nil
}

func handler(registry *prometheus.Registry) http.Handler {
	metrics := promhttp.HandlerFor(registry, promhttp.HandlerOpts{ErrorHandling: promhttp.HTTPErrorOnError, MaxRequestsInFlight: 1, EnableOpenMetrics: false})
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		if r.ContentLength != 0 || len(r.TransferEncoding) != 0 {
			http.Error(w, "request body not allowed", http.StatusBadRequest)
			return
		}
		if r.URL.Path != "/metrics" || r.URL.RawPath != "" || r.URL.RawQuery != "" || r.URL.ForceQuery {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodGet {
			w.Header().Set("Allow", "GET")
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		metrics.ServeHTTP(w, r)
	})
}

// Start validates deployment inputs before binding. Failures are optional-source
// failures; the caller must continue its business registration without a declaration.
func Start(d config.ProcessMetricsDeployment, identity Identity) (*Server, error) {
	return start(d, identity, sampleSelf)
}

// StartOptional is the sole deployment adapter shared by the ordinary lease
// client and System's local bootstrap. It never changes business readiness.
func StartOptional(identity Identity) *Server {
	d, err := config.ProcessMetricsDeploymentFor(os.Getenv(config.ProcessMetricsDeploymentsEnv), identity.ModuleName, identity.Role)
	if err != nil {
		log.Print("process_metrics_configuration_invalid")
		return nil
	}
	if d == nil {
		return nil
	}
	source, err := Start(*d, identity)
	if err != nil {
		log.Print("process_metrics_source_unavailable")
		return nil
	}
	return source
}

func start(d config.ProcessMetricsDeployment, identity Identity, sample Sampler) (*Server, error) {
	if d.Validate() != nil || identity.ModuleName != d.ModuleName || identity.Role != d.Role || identity.InstanceID == "" || !utf8.ValidString(identity.InstanceID) || utf8.RuneCountInString(identity.InstanceID) > 100 || strings.TrimSpace(identity.InstanceID) != identity.InstanceID || strings.IndexFunc(identity.InstanceID, unicode.IsControl) >= 0 ||
		identity.StartedAt.IsZero() || identity.StartedAt.After(time.Now()) || sample == nil {
		return nil, ErrSourceUnavailable
	}
	tlsConfig, err := sourceTLS(d)
	if err != nil {
		return nil, err
	}
	registry := prometheus.NewRegistry()
	if registry.Register(newCollector(identity.ModuleName, identity.Role, identity.InstanceID, identity.StartedAt, sample)) != nil {
		return nil, ErrSourceUnavailable
	}
	if _, err := registry.Gather(); err != nil {
		return nil, ErrSourceUnavailable
	}
	listener, err := net.Listen("tcp", d.Listen)
	if err != nil {
		return nil, ErrSourceUnavailable
	}
	server := &http.Server{Handler: handler(registry), ReadHeaderTimeout: 2 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second, IdleTimeout: 10 * time.Second, MaxHeaderBytes: 8192,
		ErrorLog: log.New(io.Discard, "", 0)}
	s := &Server{http: server, done: make(chan struct{}), declaration: models.ProcessMetricsDeclaration{SchemaVersion: models.ProcessMetricsSchema, Endpoint: d.Endpoint}}
	go func() { defer close(s.done); _ = server.Serve(tls.NewListener(listener, tlsConfig)) }()
	return s, nil
}
