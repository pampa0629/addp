package oceanbase_test

import (
	"context"
	"database/sql"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	oceanbaseplugin "github.com/addp/common/engine/plugins/oceanbase"
	"github.com/addp/common/models"
)

func TestIntegrationOceanBaseExecutableSampleQuery(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}

	const engineID uint = 94002
	const tableName = "addp_sample_query_gate"
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
	p := &oceanbaseplugin.Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	databaseName := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	path := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, databaseName, tableName)
	if err := p.DeleteResource(ctx, connInfo, path); err != nil {
		t.Fatalf("drop stale sample-query gate table: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cleanupCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cleanupCancel()
		if err := p.DeleteResource(cleanupContext, connInfo, path); err != nil {
			t.Errorf("cleanup sample-query gate table: %v", err)
		}
	})
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatalf("BuildDSN() error = %v", err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatalf("sql.Open() error = %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(ctx, "CREATE TABLE `"+tableName+"` (`id` BIGINT NOT NULL PRIMARY KEY, `name` VARCHAR(255) NOT NULL) ENGINE=InnoDB"); err != nil {
		t.Fatalf("create sample-query gate table: %v", err)
	}
	if _, err := db.ExecContext(ctx, "INSERT INTO `"+tableName+"` VALUES (1, 'OceanBase sample query')"); err != nil {
		t.Fatalf("insert sample-query gate row: %v", err)
	}

	engine := &models.Engine{
		ID:         engineID,
		EngineType: "oceanbase",
		ConnectionInfo: models.ConnectionInfo{
			"host":     connInfo["host"],
			"port":     connInfo["port"],
			"database": connInfo["database"],
			"user":     connInfo["user"],
			"password": connInfo["password"],
		},
	}

	query, language, err := dbbridge.GenerateExecutableSampleQuery(ctx, engine, "sql", dbbridge.ExecutableSampleQueryOptions{ValidationLimit: 10})
	if err != nil {
		t.Fatalf("GenerateExecutableSampleQuery() error = %v", err)
	}
	if language != "sql" || !strings.Contains(query, "`"+databaseName+"`") {
		t.Fatalf("GenerateExecutableSampleQuery() = (%q, %q)", query, language)
	}
}

func oceanBaseIntegrationConnInfo() plugin.ConnectionInfo {
	tenant := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_TENANT", "test")
	return plugin.ConnectionInfo{
		"host":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_HOST", "127.0.0.1"),
		"port":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_PORT", "2881"),
		"database": oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable"),
		"user":     oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_USER", "root@"+tenant),
		"password": oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_PASSWORD", ""),
	}
}

func oceanBaseIntegrationEnv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
