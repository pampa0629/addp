package infraconfig

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/migration"
	"github.com/jackc/pgx/v5"
)

func TestUnboundAndUnsupportedProviderDoNotOpenManagementConnection(t *testing.T) {
	settings := New(&config.Config{InfraDatabaseProvider: "other", InfraPGConfigPassword: "must-not-use"})
	values := settings.Read(context.Background())
	if len(values) != 2 || values[0].Status != "unsupported" || values[1].Status != "unbound" {
		t.Fatalf("settings = %+v", values)
	}
	_, err := settings.Apply(context.Background(), "database", "work_mem", "40", nil)
	if !errors.Is(err, ErrUnavailable) {
		t.Fatalf("apply error = %v", err)
	}
}

func TestInfraConfigurationNative(t *testing.T) {
	if os.Getenv("ADDP_INFRA_CONFIGURATION_INTEGRATION") != "1" {
		t.Skip("requires owned PostgreSQL and Redis from make test-system-infra-configuration")
	}
	password := os.Getenv("ADDP_INFRA_GATE_MANAGER_PASSWORD")
	cfg := &config.Config{
		InfraDatabaseProvider: "postgres", InfraPGConfigPassword: password, InfraRedisConfigPassword: password,
		PostgresHost: "127.0.0.1", PostgresPort: os.Getenv("ADDP_INFRA_GATE_PG_PORT"), PostgresDB: "addp_test",
		RedisHost: "127.0.0.1", RedisPort: os.Getenv("ADDP_INFRA_GATE_REDIS_PORT"),
	}
	s := New(cfg)
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	settings := s.Read(ctx)
	if settings[0].Status != "available" || settings[1].Status != "available" {
		t.Fatalf("cannot read native settings: %+v", settings)
	}
	lookup := func(index int, name string) Parameter {
		t.Helper()
		for _, parameter := range settings[index].Parameters {
			if parameter.Name == name {
				return parameter
			}
		}
		t.Fatalf("missing setting %q", name)
		return Parameter{}
	}
	apply := func(component, name, value string) Outcome {
		t.Helper()
		out, err := s.Apply(ctx, component, name, value, func(endpoint, previous string) error {
			if endpoint == "" || previous == "" {
				t.Fatal("operation lacks target and prior value")
			}
			return nil
		})
		if err != nil || out.Application != "confirmed" || out.Current == nil || out.Current.Value != value {
			t.Fatalf("apply %s: %+v, %v", name, out, err)
		}
		return out
	}
	switch phase := os.Getenv("ADDP_INFRA_GATE_PHASE"); phase {
	case "apply":
		cfg.PostgresUser, cfg.PostgresPassword = "infra_test", os.Getenv("ADDP_INFRA_GATE_PASSWORD")
		verifyPermissionMigration(t, ctx, cfg.PostgreSQLDSN())
		work := lookup(0, "work_mem")
		if work.Value != "32768" || !work.Editable || work.Source != "configuration file" {
			t.Fatalf("deployment baseline: %+v", work)
		}
		readOnly := lookup(0, "shared_buffers")
		if readOnly.Editable || readOnly.Source != "command line" || readOnly.Activation != "restart" {
			t.Fatalf("command-line setting: %+v", readOnly)
		}
		auditFailure := errors.New("audit unavailable")
		if _, err := s.Apply(ctx, "database", "work_mem", "40960", func(string, string) error { return auditFailure }); !errors.Is(err, auditFailure) {
			t.Fatalf("write did not stop before audit failure: %v", err)
		}
		settings = s.Read(ctx)
		if lookup(0, "work_mem").Value != work.Value {
			t.Fatal("write occurred despite audit rejection")
		}
		for _, sample := range [][3]string{
			{"database", "work_mem", "40960"}, {"database", "maintenance_work_mem", "196608"},
			{"database", "log_min_duration_statement", "850"}, {"database", "log_lock_waits", "on"},
			{"redis", "slowlog-log-slower-than", "24000"}, {"redis", "slowlog-max-len", "192"}, {"redis", "latency-monitor-threshold", "250"},
		} {
			if result := apply(sample[0], sample[1], sample[2]); result.Persistence != "confirmed" {
				t.Fatalf("%s not persisted: %+v", sample[1], result)
			}
		}
		if _, err := s.Apply(ctx, "redis", "maxmemory-policy", "allkeys-lru", nil); !errors.Is(err, ErrReadOnly) {
			t.Fatalf("read-only Redis setting was accepted: %v", err)
		}
	case "persistence-failure":
		if result := apply("redis", "slowlog-log-slower-than", "25300"); result.Persistence != "unconfirmed" || result.FailureStage != "persist" {
			t.Fatalf("persistence failure mislabeled: %+v", result)
		}
	case "restart":
		for name, value := range map[string]string{
			"work_mem": "40960", "maintenance_work_mem": "196608", "log_min_duration_statement": "850", "log_lock_waits": "on",
		} {
			if p := lookup(0, name); p.Value != value {
				t.Fatalf("PG parameter missing after recreation: %+v", p)
			}
		}
		for name, value := range map[string]string{
			"slowlog-log-slower-than": "24000", "slowlog-max-len": "192", "latency-monitor-threshold": "250",
		} {
			if p := lookup(1, name); p.Value != value {
				t.Fatalf("Redis parameter missing after recreation: %+v", p)
			}
		}
	default:
		t.Fatalf("invalid fixture phase %q", phase)
	}
}

func verifyPermissionMigration(t *testing.T, ctx context.Context, dsn string) {
	t.Helper()
	if err := migration.NewRunner(dsn).Run(ctx); err != nil {
		t.Fatalf("apply System migrations: %v", err)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close(context.Background())
	var count int
	var platformOnly bool
	err = conn.QueryRow(ctx, `SELECT count(*), coalesce(bool_and(
		r.role_key IS NOT DISTINCT FROM 'platform.system_administrator' AND r.tenant_id IS NULL AND
		p.allowed_scope_types = ARRAY['platform']::text[] AND NOT p.delegable), false)
		FROM system.permissions p
		LEFT JOIN system.role_permissions rp ON rp.permission_id = p.id
		LEFT JOIN system.roles r ON r.id = rp.role_id
		WHERE p.permission_key IN ('system.infra_configuration.read','system.infra_configuration.update')`).Scan(&count, &platformOnly)
	if err != nil || count != 2 || !platformOnly {
		t.Fatalf("Infra permission assignments: count=%d platformOnly=%v err=%v", count, platformOnly, err)
	}
}
