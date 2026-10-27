package main

import (
	"database/sql"
	"github.com/google/uuid"
	"github.com/mattn/go-sqlite3"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func httpLocalEnvironment() []string {
	return []string{"ADDP_HTTP_LOCAL=1", "ADDP_HTTP_LOCAL_RUN_ID=11111111-1111-4111-8111-111111111111", "POSTGRES_DB=addp_test", "POSTGRES_HOST=127.0.0.1"}
}

func TestHTTPQualityLocalAdmission(t *testing.T) {
	output := filepath.Join(privateTraceTestDir(t), "identities.env")
	base := httpLocalEnvironment()
	if err := validateHTTPQualityLocal(base, output, httpQualityLocalSuite); err != nil {
		t.Fatal(err)
	}
	for _, entry := range []string{"GITHUB_ACTIONS=true", "ADDP_ONLINE_HOSTED=1", "ADDP_ONLINE_OWNER_MANAGED=1", "ADDP_EXECUTION_TRACE_LOCAL=1", "ADDP_CATALOG_BROWSER_LOCAL=1", "POSTGRES_DB=addp", "POSTGRES_HOST=postgres", "ADDP_HTTP_LOCAL_RUN_ID=00000000-0000-0000-0000-000000000000", "ADDP_HTTP_LOCAL=0"} {
		copyEnv := append([]string{}, base...)
		key := strings.SplitN(entry, "=", 2)[0] + "="
		for i, item := range copyEnv {
			if strings.HasPrefix(item, key) {
				copyEnv = append(copyEnv[:i], copyEnv[i+1:]...)
				break
			}
		}
		copyEnv = append(copyEnv, entry)
		if err := validateHTTPQualityLocal(copyEnv, output, httpQualityLocalSuite); err == nil {
			t.Fatalf("accepted %s", key)
		}
	}
	for _, suite := range []string{"platform-node-metrics", executionTraceLocalSuite, ""} {
		if err := validateHTTPQualityLocal(base, output, suite); err == nil {
			t.Fatal("accepted foreign suite")
		}
	}
	if err := validateHTTPQualityLocal(append(base, "ADDP_HTTP_LOCAL=1"), output, httpQualityLocalSuite); err == nil {
		t.Fatal("accepted duplicate marker")
	}
	if !httpQualityLocalSelected(base, "") || httpQualityLocalSelected(nil, "platform-node-metrics") {
		t.Fatal("profile dispatch bypass")
	}
	if err := os.WriteFile(output, []byte("existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := validateHTTPQualityLocal(base, output, httpQualityLocalSuite); err == nil {
		t.Fatal("overwrote private output")
	}
}

func TestHTTPQualityLocalClusterAndCredentialScope(t *testing.T) {
	cluster, err := httpQualityLocalCluster(httpLocalEnvironment())
	if err != nil || cluster != "addp-http-quality-local-11111111-1111-4111-8111-111111111111" {
		t.Fatal(cluster, err)
	}
	keys := httpQualityLocalKeys()
	if len(keys) != 11 {
		t.Fatal("credential contract drift")
	}
	values := map[string]string{}
	for _, key := range keys {
		if !strings.HasPrefix(key, "ADDP_HTTP_LOCAL_") || strings.Contains(key, "TOKEN") {
			t.Fatal("session or foreign identity exported")
		}
		values[key] = "local-test-value"
	}
	output := filepath.Join(privateTraceTestDir(t), "credentials.env")
	if err := writeLocalIdentityEnvironment(output, values, keys); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(output)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("private credential mode", err)
	}
}

func TestHTTPQualityLocalRejectsSharedOrForeignActualDatabase(t *testing.T) {
	expected, err := httpQualityLocalCluster(httpLocalEnvironment())
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []struct {
		database, cluster string
		accepted          bool
	}{
		{"addp_test", expected, true}, {"addp_test", "", false}, {"addp_test", "foreign-run", false}, {"addp_online", expected, false},
	} {
		t.Run(item.database+item.cluster, func(t *testing.T) {
			driver := "http_quality_local_" + uuid.NewString()
			sql.Register(driver, &sqlite3.SQLiteDriver{ConnectHook: func(conn *sqlite3.SQLiteConn) error {
				if err := conn.RegisterFunc("current_database", func() string { return item.database }, true); err != nil {
					return err
				}
				return conn.RegisterFunc("current_setting", func(name string) string {
					if name == "cluster_name" {
						return item.cluster
					}
					return ""
				}, true)
			}})
			db, err := gorm.Open(sqlite.Dialector{DriverName: driver, DSN: ":memory:"}, &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
			if err != nil {
				t.Fatal(err)
			}
			raw, _ := db.DB()
			defer raw.Close()
			if err := validateHTTPQualityLocalDatabase(db, httpLocalEnvironment()); (err == nil) != item.accepted {
				t.Fatal("actual database admission", err)
			}
		})
	}
}
