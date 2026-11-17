// Package redistest provides isolated Redis fault observation for infrastructure gates.
// Production code must not import it.
package redistest

import (
	"context"
	"io"
	"net"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/redis/go-redis/v9"
)

func Config(t *testing.T) config.RedisConfig {
	t.Helper()
	address := os.Getenv("ADDP_REDIS_CORE_ENDPOINT")
	if address == "" {
		t.Skip("requires make test-common-redis-core")
	}
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	return config.RedisConfig{Host: host, Port: port, Password: os.Getenv("ADDP_REDIS_CORE_PASSWORD")}
}

func Open(t *testing.T, cfg config.RedisConfig) *redis.Client {
	t.Helper()
	client, err := bootstrap.OpenRequiredRedis(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client
}

// Proxy drops active TCP connections while preserving the actual Redis server,
// its keys and its expiry clock. Recover never creates or restarts a server.
type Proxy struct {
	listener    net.Listener
	target      string
	mu          sync.Mutex
	available   bool
	closed      bool
	connections map[net.Conn]bool
}

func NewProxy(t *testing.T, target string) *Proxy {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	p := &Proxy{listener: listener, target: target, available: true, connections: make(map[net.Conn]bool)}
	t.Cleanup(func() { p.mu.Lock(); p.closed = true; p.mu.Unlock(); p.SetAvailable(false); _ = listener.Close() })
	go p.serve()
	return p
}

func (p *Proxy) Config(cfg config.RedisConfig) config.RedisConfig {
	cfg.Host, cfg.Port, _ = net.SplitHostPort(p.listener.Addr().String())
	return cfg
}

func (p *Proxy) SetAvailable(available bool) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.available = available
	if !available {
		for c := range p.connections {
			_ = c.Close()
		}
	}
}

func (p *Proxy) serve() {
	for {
		downstream, err := p.listener.Accept()
		if err != nil {
			return
		}
		p.mu.Lock()
		if !p.available || p.closed {
			p.mu.Unlock()
			_ = downstream.Close()
			continue
		}
		p.connections[downstream] = true
		p.mu.Unlock()
		go p.forward(downstream)
	}
}

func (p *Proxy) forward(downstream net.Conn) {
	upstream, err := net.DialTimeout("tcp", p.target, time.Second)
	if err != nil {
		_ = downstream.Close()
		p.mu.Lock()
		delete(p.connections, downstream)
		p.mu.Unlock()
		return
	}
	p.mu.Lock()
	if !p.available || p.closed {
		delete(p.connections, downstream)
		p.mu.Unlock()
		_ = downstream.Close()
		_ = upstream.Close()
		return
	}
	p.connections[upstream] = true
	p.mu.Unlock()
	defer func() {
		_ = downstream.Close()
		_ = upstream.Close()
		p.mu.Lock()
		delete(p.connections, downstream)
		delete(p.connections, upstream)
		p.mu.Unlock()
	}()
	done := make(chan struct{})
	go func() { _, _ = io.Copy(upstream, downstream); _ = upstream.Close(); close(done) }()
	_, _ = io.Copy(downstream, upstream)
	_ = downstream.Close()
	<-done
}
