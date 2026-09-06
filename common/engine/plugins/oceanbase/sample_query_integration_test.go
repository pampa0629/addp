package oceanbase_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

func TestIntegrationOceanBaseExecutableSampleQuery(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94002
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })

	engine := &models.Engine{
		ID:         engineID,
		EngineType: "oceanbase",
		ConnectionInfo: models.ConnectionInfo{
			"host":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_HOST", "127.0.0.1"),
			"port":     oceanBaseIntegrationEnv("OCEANBASE_PORT", "2881"),
			"database": oceanBaseIntegrationEnv("OCEANBASE_DATABASE", "business"),
			"user":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_USER", "root@"+oceanBaseIntegrationEnv("OCEANBASE_TENANT_NAME", "test")),
			"password": oceanBaseIntegrationEnv("OCEANBASE_PASSWORD", "business_oceanbase_password"),
		},
	}

	query, language, err := dbbridge.GenerateExecutableSampleQuery(ctx, engine, "sql", dbbridge.ExecutableSampleQueryOptions{ValidationLimit: 10})
	if err != nil {
		t.Fatalf("GenerateExecutableSampleQuery() error = %v", err)
	}
	if language != "sql" || !strings.Contains(query, "`business`") {
		t.Fatalf("GenerateExecutableSampleQuery() = (%q, %q)", query, language)
	}
}

func oceanBaseIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
