package router

import (
	"context"
	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/gateway/internal/config"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"
)

func TestRedisCoreGatewayWire(t *testing.T) {
	if os.Getenv("ADDP_REDIS_WIRING_CHILD") != "gateway" {
		t.Fatal("private protocol child required")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	host, port, err := net.SplitHostPort(os.Getenv("ADDP_REDIS_WIRING_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := bootstrap.OpenRequiredRedis(ctx, commonconfig.RedisConfig{Host: host, Port: port, Password: os.Getenv("ADDP_REDIS_CORE_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	pg, err := url.Parse(os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	password, _ := pg.User.Password()
	cfg := &config.Config{DBHost: pg.Hostname(), DBPort: pg.Port(), DBUser: pg.User.Username(), DBPassword: password, DBName: strings.TrimPrefix(pg.Path, "/"), DBSchema: "gateway", SystemServiceURL: os.Getenv("ADDP_REDIS_WIRING_SYSTEM_URL"), ServiceClientSecret: os.Getenv("ADDP_REDIS_WIRING_GATEWAY_SECRET"), ModuleWatchTimeout: time.Second}
	router, _ := SetupRouter(cfg, client)
	listener, err := net.Listen("tcp", os.Getenv("ADDP_REDIS_WIRING_LISTEN"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: router, ReadHeaderTimeout: time.Second}
	defer server.Close()
	go server.Serve(listener)
	if err := os.WriteFile(os.Getenv("ADDP_REDIS_WIRING_READY"), []byte(listener.Addr().String()), 0600); err != nil {
		t.Fatal(err)
	}
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			t.Fatal("private protocol child timed out")
		case <-ticker.C:
			if _, err := os.Stat(os.Getenv("ADDP_REDIS_WIRING_STOP")); err == nil {
				return
			}
		}
	}
}
