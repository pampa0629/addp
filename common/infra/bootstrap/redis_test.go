package bootstrap

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/addp/common/config"
	"github.com/alicebob/miniredis/v2"
)

func TestRequiredRedisConfigurationAndAuthentication(t *testing.T) {
	for _, cfg := range []config.RedisConfig{{}, {Host: "127.0.0.1", Port: "bad"}, {Host: "127.0.0.1", Port: "0"}, {Host: "127.0.0.1", Port: "6379", DB: -1}} {
		if client, err := OpenRequiredRedis(t.Context(), cfg); err == nil || client != nil {
			t.Fatal("invalid config returned client")
		}
	}
	server := miniredis.RunT(t)
	server.RequireAuth("secret")
	cfg := config.RedisConfig{Host: server.Host(), Port: server.Port(), Password: "wrong"}
	if client, err := OpenRequiredRedis(t.Context(), cfg); err == nil || client != nil {
		t.Fatal("wrong authentication accepted")
	}
	cfg.Password = "secret"
	client, err := OpenRequiredRedis(t.Context(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	started := time.Now()
	if c, err := OpenRequiredRedis(ctx, cfg); err == nil || c != nil || time.Since(started) > time.Second {
		t.Fatal("cancelled startup accepted or exceeded bound")
	}
}

func TestRequiredRedisRespectsCallerDeadline(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	done := make(chan struct{})
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		<-done
	}()
	defer close(done)
	host, port, _ := net.SplitHostPort(listener.Addr().String())
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	started := time.Now()
	client, err := OpenRequiredRedis(ctx, config.RedisConfig{Host: host, Port: port, Password: "fixture"})
	if err == nil || client != nil || time.Since(started) > time.Second {
		t.Fatal("startup ignored caller deadline")
	}
}
