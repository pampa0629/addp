package redis

import (
	"context"
	"crypto/tls"
	"encoding/pem"
	"net"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/alicebob/miniredis/v2"
	"github.com/alicebob/miniredis/v2/server"
)

func redisTestConnection(t *testing.T, address string) plugin.ConnectionInfo {
	t.Helper()
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		t.Fatal(err)
	}
	n, _ := strconv.Atoi(port)
	return plugin.ConnectionInfo{"host": host, "port": n, "user": "reader", "password": "secret", "database": 0}
}

func TestConnectionValidationAndIdentity(t *testing.T) {
	p := &RedisPlugin{}
	base := plugin.ConnectionInfo{"host": "localhost", "password": "secret"}
	if err := p.ValidateConnectionInfo(base); err != nil {
		t.Fatal(err)
	}
	for key, values := range map[string][]interface{}{
		"host":     {"redis://localhost", "host:6379", " bad/host ", "", 123},
		"port":     {0, 65536, 6379.5, "6379", nil},
		"database": {-1, 0.5, "0", 2147483648.0, nil},
		"password": {"", 123, nil}, "user": {"", " reader ", 123, nil},
		"use_ssl": {"true", 1, nil}, "tls_ca_cert": {"bad PEM", 123},
	} {
		for _, value := range values {
			c := plugin.ConnectionInfo{}
			for k, v := range base {
				c[k] = v
			}
			c[key] = value
			if err := p.ValidateConnectionInfo(c); err == nil {
				t.Fatalf("accepted %s=%v", key, value)
			}
		}
	}
	identity, err := plugin.BuildConnectionIdentityKey("redis", base)
	if err != nil {
		t.Fatal(err)
	}
	explicit := plugin.ConnectionInfo{"host": "127.0.0.1", "port": float64(6379), "database": float64(0), "user": "default", "password": "changed"}
	normalized, _ := plugin.BuildConnectionIdentityKey("redis", explicit)
	if identity != normalized {
		t.Fatalf("default identity %s differs from explicit %s", identity, normalized)
	}
	explicit["database"] = 1
	different, _ := plugin.BuildConnectionIdentityKey("redis", explicit)
	if identity == different {
		t.Fatal("database is missing from identity")
	}
	if err := plugin.ValidatePluginCapabilities(p); err != nil {
		t.Fatal(err)
	}
	descriptor, err := plugin.DescribeEngineType(p)
	if err != nil {
		t.Fatal(err)
	}
	if descriptor.CatalogModel != nil || descriptor.Capabilities.Compute != nil || descriptor.Capabilities.Storage.Catalog != nil || descriptor.Capabilities.Storage.Store != nil {
		t.Fatal("connection-only plugin declared data access")
	}
}

func TestAuthenticatedConnectionAndCancellation(t *testing.T) {
	m := miniredis.RunT(t)
	m.RequireUserAuth("reader", "secret")
	m.DB(3).Set("sentinel", "unchanged")
	c := redisTestConnection(t, m.Addr())
	c["database"] = 3
	p := &RedisPlugin{}
	if err := p.TestConnection(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	if value, _ := m.DB(3).Get("sentinel"); value != "unchanged" || len(m.DB(3).Keys()) != 1 {
		t.Fatal("probe mutated data")
	}
	c["password"] = "wrong"
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("accepted incorrect password")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	c["password"] = "secret"
	if err := p.TestConnection(ctx, c); err == nil {
		t.Fatal("ignored canceled context")
	}
	// A TCP peer that never replies must obey the caller's short deadline.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	defer close(done)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			defer conn.Close()
			<-done
		}
	}()
	ctx, cancel = context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	start := time.Now()
	if err := p.TestConnection(ctx, redisTestConnection(t, listener.Addr().String())); err == nil {
		t.Fatal("accepted silent peer")
	}
	if time.Since(start) > time.Second {
		t.Fatal("caller deadline ignored")
	}
}

func TestTLSVerifiesTrustAndHostname(t *testing.T) {
	httpServer := httptest.NewTLSServer(nil)
	certificate := httpServer.TLS.Certificates[0]
	httpServer.Close()
	m := miniredis.NewMiniRedis()
	if err := m.StartTLS(&tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12}); err != nil {
		t.Fatal(err)
	}
	defer m.Close()
	m.RequireUserAuth("reader", "secret")
	c := redisTestConnection(t, m.Addr())
	c["use_ssl"] = true
	p := &RedisPlugin{}
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("accepted untrusted certificate")
	}
	c["tls_ca_cert"] = string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificate.Certificate[0]}))
	if err := p.TestConnection(context.Background(), c); err != nil {
		t.Fatal(err)
	}
	c["host"] = "localhost"
	if err := p.TestConnection(context.Background(), c); err == nil {
		t.Fatal("accepted certificate for another hostname")
	}
}

func TestConnectionRejectsOtherServerModes(t *testing.T) {
	for _, mode := range []string{"cluster", "sentinel"} {
		t.Run(mode, func(t *testing.T) {
			m := miniredis.RunT(t)
			m.Server().SetPreHook(func(peer *server.Peer, command string, args ...string) bool {
				if command != "HELLO" {
					return false
				}
				peer.WriteStrings([]string{"server", "redis", "mode", mode})
				return true
			})
			if err := (&RedisPlugin{}).TestConnection(context.Background(), redisTestConnection(t, m.Addr())); err == nil || !strings.Contains(err.Error(), "standalone server mode") {
				t.Fatalf("unexpected result for %s server: %v", mode, err)
			}
		})
	}
}
