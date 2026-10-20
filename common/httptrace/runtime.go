// Package httptrace owns ADDP's single, optional HTTP trace path. It never
// changes application readiness, records payloads, or exports logs/metrics.
package httptrace

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/addp/common/buildinfo"
	"github.com/addp/common/config"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/sdk/resource"
	sdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
	"go.opentelemetry.io/otel/trace/noop"
)

var ErrDeployment = errors.New("trace_deployment_invalid")
var name = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,63}$`)

type Deployment struct {
	Module   string `json:"module_name"`
	Role     string `json:"role"`
	Endpoint string `json:"endpoint"`
	TLSDir   string `json:"tls_dir"`
}
type Runtime struct {
	provider *sdk.TracerProvider
	role     string
	queue    *processor
}

var current atomic.Pointer[Runtime]
var processRole atomic.Value
var startup sync.Mutex
var noTracer = noop.NewTracerProvider().Tracer("addp.http")

// ParseDeployment has one explicit input; standard OTEL_* variables are not a
// second source of endpoint, identity, headers, sampling or credentials.
func ParseDeployment(raw, module, role string) (*Deployment, error) {
	if raw == "" {
		return nil, nil
	}
	if len(raw) > 256<<10 {
		return nil, ErrDeployment
	}
	var values []Deployment
	decoder := json.NewDecoder(strings.NewReader(raw))
	if token, e := decoder.Token(); e != nil || token != json.Delim('[') {
		return nil, ErrDeployment
	}
	for decoder.More() {
		if len(values) >= 1000 {
			return nil, ErrDeployment
		}
		if token, e := decoder.Token(); e != nil || token != json.Delim('{') {
			return nil, ErrDeployment
		}
		fields := map[string]string{}
		for decoder.More() {
			token, e := decoder.Token()
			key, ok := token.(string)
			if e != nil || !ok {
				return nil, ErrDeployment
			}
			if _, exists := fields[key]; exists {
				return nil, ErrDeployment
			}
			switch key {
			case "module_name", "role", "endpoint", "tls_dir":
			default:
				return nil, ErrDeployment
			}
			var value string
			if decoder.Decode(&value) != nil {
				return nil, ErrDeployment
			}
			fields[key] = value
		}
		if token, e := decoder.Token(); e != nil || token != json.Delim('}') || len(fields) != 4 {
			return nil, ErrDeployment
		}
		values = append(values, Deployment{fields["module_name"], fields["role"], fields["endpoint"], fields["tls_dir"]})
	}
	if token, e := decoder.Token(); e != nil || token != json.Delim(']') {
		return nil, ErrDeployment
	}
	if _, e := decoder.Token(); e != io.EOF {
		return nil, ErrDeployment
	}
	seen := map[string]bool{}
	var selected *Deployment
	for _, v := range values {
		key := v.Module + "/" + v.Role
		u, e := url.Parse(v.Endpoint)
		if !name.MatchString(v.Module) || (v.Role != "backend" && v.Role != "ingress") || seen[key] || e != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Path != "/v1/traces" || u.RawPath != "" || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || !filepath.IsAbs(v.TLSDir) {
			return nil, ErrDeployment
		}
		seen[key] = true
		if v.Module == module && v.Role == role {
			copy := v
			selected = &copy
		}
	}
	return selected, nil
}

func clientTLS(dir string) (*tls.Config, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, ErrDeployment
	}
	resolved, e := filepath.EvalSymlinks(dir)
	if e != nil {
		return nil, ErrDeployment
	}
	if root := config.ProjectRoot(); root != "" {
		root, _ = filepath.EvalSymlinks(root)
		relative, e := filepath.Rel(root, resolved)
		if e != nil || relative == "." || relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return nil, ErrDeployment
		}
	}
	info, e := os.Lstat(dir)
	if e != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0077 != 0 {
		return nil, ErrDeployment
	}
	files, e := os.ReadDir(dir)
	if e != nil || len(files) != 3 {
		return nil, ErrDeployment
	}
	read := func(n string) ([]byte, error) {
		p := filepath.Join(dir, n)
		i, e := os.Lstat(p)
		if e != nil || !i.Mode().IsRegular() || i.Size() < 1 || i.Size() > 256<<10 || (n == "client.key" && i.Mode().Perm()&0077 != 0) {
			return nil, ErrDeployment
		}
		stat, ok := i.Sys().(*syscall.Stat_t)
		if !ok || stat.Nlink != 1 {
			return nil, ErrDeployment
		}
		f, err := os.Open(p)
		if err != nil {
			return nil, ErrDeployment
		}
		defer f.Close()
		data, err := io.ReadAll(io.LimitReader(f, (256<<10)+1))
		if err != nil || len(data) > 256<<10 {
			return nil, ErrDeployment
		}
		return data, nil
	}
	ca, e := read("ca.crt")
	if e != nil {
		return nil, e
	}
	crt, e := read("client.crt")
	if e != nil {
		return nil, e
	}
	key, e := read("client.key")
	if e != nil {
		return nil, e
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(ca) {
		return nil, ErrDeployment
	}
	pair, e := tls.X509KeyPair(crt, key)
	if e != nil {
		return nil, ErrDeployment
	}
	return &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, Certificates: []tls.Certificate{pair}}, nil
}

// TLSHTTPClient is shared transport construction, not shared credentials: each
// caller supplies its own owner-only deployment directory and client identity.
func TLSHTTPClient(dir string, timeout time.Duration) (*http.Client, error) {
	config, e := clientTLS(dir)
	if e != nil {
		return nil, e
	}
	base := http.DefaultTransport.(*http.Transport).Clone()
	base.Proxy = nil
	base.TLSClientConfig = config
	return &http.Client{Transport: base, Timeout: timeout, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}, nil
}

// StartOptional is called once by the actual registration lifecycle. The same
// startup ID is used by System, logs, metrics and traces. No collector handshake
// is required for business startup; all exports happen on the bounded queue.
func StartOptional(module, role string) func() {
	processRole.Store(role)
	if os.Getenv("ADDP_OBSERVABILITY_TRACES_ENABLED") != "true" {
		return func() {}
	}
	startup.Lock()
	defer startup.Unlock()
	d, e := ParseDeployment(os.Getenv("ADDP_TRACE_DEPLOYMENTS"), module, role)
	if e != nil {
		log.Print("trace_deployment_invalid")
		return func() {}
	}
	if d == nil {
		return func() {}
	}
	if current.Load() != nil {
		log.Print("trace_already_started")
		return func() {}
	}
	config, e := clientTLS(d.TLSDir)
	if e != nil {
		log.Print("trace_deployment_invalid")
		return func() {}
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = config
	client := &http.Client{Transport: transport, Timeout: 2 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	exporter, e := otlptracehttp.New(context.Background(), otlptracehttp.WithEndpointURL(d.Endpoint), otlptracehttp.WithHTTPClient(client), otlptracehttp.WithHeaders(map[string]string{}), otlptracehttp.WithTimeout(2*time.Second), otlptracehttp.WithRetry(otlptracehttp.RetryConfig{Enabled: false}), otlptracehttp.WithCompression(otlptracehttp.NoCompression))
	if e != nil {
		log.Print("trace_deployment_invalid")
		return func() {}
	}
	r := newRuntime(module, role, buildinfo.ProcessInstanceID(), exporter)
	current.Store(r)
	var once sync.Once
	return func() {
		once.Do(func() {
			current.CompareAndSwap(r, nil)
			ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = r.provider.Shutdown(ctx)
			transport.CloseIdleConnections()
		})
	}
}

func newRuntime(module, role, id string, exporter sdk.SpanExporter) *Runtime {
	q := newProcessor(exporter)
	tp := sdk.NewTracerProvider(sdk.WithSampler(sdk.AlwaysSample()), sdk.WithResource(resource.NewSchemaless(attribute.String("service.name", module), attribute.String("service.instance.id", id), attribute.String("addp.role", role))), sdk.WithSpanLimits(sdk.SpanLimits{AttributeCountLimit: 32, AttributeValueLengthLimit: 256, EventCountLimit: 0, LinkCountLimit: 8}), sdk.WithSpanProcessor(q))
	return &Runtime{provider: tp, role: role, queue: q}
}
func tracer() trace.Tracer {
	if r := current.Load(); r != nil {
		return r.provider.Tracer("addp.http")
	}
	return noTracer
}

type Counters struct {
	Queued       int    `json:"queued"`
	Dropped      uint64 `json:"dropped"`
	ExportFailed uint64 `json:"export_failed"`
	Exported     uint64 `json:"exported"`
}

func Snapshot() Counters {
	if r := current.Load(); r != nil {
		return Counters{len(r.queue.spans), r.queue.dropped.Load(), r.queue.failed.Load(), r.queue.exported.Load()}
	}
	return Counters{}
}

// A single bounded processor provides countable non-blocking drops without
// unbounded retry buffers. Only the official OTLP exporter encodes the wire.
type processor struct {
	exporter                  sdk.SpanExporter
	spans                     chan sdk.ReadOnlySpan
	stop                      chan struct{}
	flush                     chan flushRequest
	shutdownContext           context.Context
	runContext                context.Context
	cancel                    context.CancelFunc
	done                      chan struct{}
	mu                        sync.RWMutex
	closed                    bool
	dropped, failed, exported atomic.Uint64
}

type flushRequest struct {
	ctx  context.Context
	done chan struct{}
}

func newProcessor(e sdk.SpanExporter) *processor {
	p := &processor{exporter: e, spans: make(chan sdk.ReadOnlySpan, 2048), stop: make(chan struct{}), done: make(chan struct{})}
	p.flush = make(chan flushRequest)
	p.runContext, p.cancel = context.WithCancel(context.Background())
	go p.run()
	return p
}
func (p *processor) OnStart(context.Context, sdk.ReadWriteSpan) {}
func (p *processor) OnEnd(s sdk.ReadOnlySpan) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	if p.closed {
		p.dropped.Add(1)
		return
	}
	select {
	case p.spans <- s:
	default:
		p.dropped.Add(1)
	}
}
func (p *processor) ForceFlush(ctx context.Context) error {
	request := flushRequest{ctx, make(chan struct{})}
	select {
	case p.flush <- request:
	case <-p.done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-request.done:
		return ctx.Err()
	case <-ctx.Done():
		return ctx.Err()
	}
}
func (p *processor) Shutdown(ctx context.Context) error {
	p.mu.Lock()
	if !p.closed {
		p.closed = true
		p.shutdownContext = ctx
		close(p.stop)
	}
	p.mu.Unlock()
	select {
	case <-p.done:
		return p.exporter.Shutdown(ctx)
	case <-ctx.Done():
		p.cancel()
		return ctx.Err()
	}
}
func (p *processor) run() {
	defer close(p.done)
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	batch := make([]sdk.ReadOnlySpan, 0, 256)
	send := func(parent context.Context) {
		if len(batch) == 0 {
			return
		}
		ctx, cancel := context.WithTimeout(parent, 2*time.Second)
		e := p.exporter.ExportSpans(ctx, batch)
		cancel()
		if e != nil {
			p.failed.Add(uint64(len(batch)))
		} else {
			p.exported.Add(uint64(len(batch)))
		}
		clear(batch)
		batch = batch[:0]
	}
	for {
		select {
		case s := <-p.spans:
			batch = append(batch, s)
			if len(batch) == 256 {
				send(p.runContext)
			}
		case <-tick.C:
			send(p.runContext)
		case request := <-p.flush:
			count := len(p.spans)
			for i := 0; i < count; i++ {
				batch = append(batch, <-p.spans)
				if len(batch) == 256 {
					send(request.ctx)
				}
			}
			send(request.ctx)
			close(request.done)
		case <-p.stop:
			for {
				select {
				case s := <-p.spans:
					batch = append(batch, s)
					if len(batch) == 256 {
						send(p.shutdownContext)
					}
				default:
					send(p.shutdownContext)
					return
				}
			}
		}
	}
}
