package api

import (
	"context"
	"encoding/json"
	commonconfig "github.com/addp/common/config"
	"github.com/addp/common/infra/bootstrap"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/repository"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"
)

func TestRedisCoreSystemWire(t *testing.T) {
	if os.Getenv("ADDP_REDIS_WIRING_CHILD") != "system" {
		t.Fatal("private protocol child required")
	}
	address, err := url.Parse(os.Getenv("ADDP_SYSTEM_POSTGRES_TEST_DSN"))
	if err != nil {
		t.Fatal(err)
	}
	query := address.Query()
	query.Set("search_path", "system")
	address.RawQuery = query.Encode()
	db, err := gorm.Open(postgres.Open(address.String()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	connection, _ := db.DB()
	defer connection.Close()
	if err := repository.AutoMigrateNonIAM(db); err != nil {
		t.Fatal("production non-IAM bootstrap:", err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	secrets := testBuiltinServiceClientSecrets("redis-wire")
	provisioner, err := iam.NewServiceCredentialProvisioner(iam.NewRepository(db), nil)
	if err != nil {
		t.Fatal(err)
	}
	secrets["addp-gateway"] = os.Getenv("ADDP_REDIS_WIRING_GATEWAY_SECRET")
	if err := provisioner.Apply(ctx, secrets); err != nil {
		t.Fatal(err)
	}
	host, port, err := net.SplitHostPort(os.Getenv("ADDP_REDIS_WIRING_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	client, err := bootstrap.OpenRequiredRedis(ctx, commonconfig.RedisConfig{Host: host, Port: port, Password: os.Getenv("ADDP_REDIS_CORE_PASSWORD")})
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	listener, err := net.Listen("tcp", os.Getenv("ADDP_REDIS_WIRING_LISTEN"))
	if err != nil {
		t.Fatal(err)
	}
	server := &http.Server{Handler: SetupRouter(db, testIAMRuntimeConfig(), client), ReadHeaderTimeout: time.Second}
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
				raw, err := os.ReadFile(os.Getenv("ADDP_REDIS_WIRING_IDENTITY"))
				if err != nil {
					t.Fatal(err)
				}
				var identity map[string]string
				if err := json.Unmarshal(raw, &identity); err != nil {
					t.Fatal(err)
				}
				principal, err := strconv.ParseInt(identity["ADDP_REDIS_HTTP_ACCOUNT_ID"], 10, 64)
				if err != nil {
					t.Fatal(err)
				}
				var tokens, roles, audits int64
				if err := db.Raw(`SELECT count(*) FROM system.access_tokens t JOIN system.refresh_token_families f ON f.id=t.family_id WHERE f.principal_id=?`, principal).Scan(&tokens).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Raw(`SELECT count(*) FROM system.role_assignments WHERE principal_id=?`, principal).Scan(&roles).Error; err != nil {
					t.Fatal(err)
				}
				if err := db.Raw(`SELECT count(*) FROM system.audit_logs WHERE principal_id=? AND event_name='oauth.token.issued' AND result='succeeded'`, principal).Scan(&audits).Error; err != nil {
					t.Fatal(err)
				}
				if tokens != 1 || roles != 0 || audits != 1 {
					t.Fatalf("protocol side effects tokens=%d roles=%d audits=%d", tokens, roles, audits)
				}
				return
			}
		}
	}
}
