package main

import (
	"errors"
	"strings"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/iamcli"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

const httpQualityLocalSuite = "python-http-product-local"

func httpQualityLocalKeys() []string {
	keys := make([]string, len(executionTraceLocalEnvironmentKeys))
	for i, key := range executionTraceLocalEnvironmentKeys {
		keys[i] = strings.Replace(key, "ADDP_EXECUTION_TRACE_", "ADDP_HTTP_LOCAL_", 1)
	}
	return keys
}

func httpQualityLocalCluster(environment []string) (string, error) {
	values, err := localTraceEnvironment(environment)
	if err != nil {
		return "", err
	}
	id, err := uuid.Parse(values["ADDP_HTTP_LOCAL_RUN_ID"])
	if err != nil || id == uuid.Nil || id.String() != values["ADDP_HTTP_LOCAL_RUN_ID"] {
		return "", errors.New("HTTP local run identity invalid")
	}
	return "addp-http-quality-local-" + id.String(), nil
}

func validateHTTPQualityLocal(environment []string, output, suite string) error {
	values, err := localTraceEnvironment(environment)
	if err != nil {
		return err
	}
	if suite != httpQualityLocalSuite || values["ADDP_HTTP_LOCAL"] != "1" {
		return errors.New("HTTP local suite and marker required")
	}
	for _, key := range []string{"GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED", "ADDP_ONLINE_LOCAL_SGM", "ADDP_EXECUTION_TRACE_LOCAL", "ADDP_CATALOG_BROWSER_LOCAL"} {
		if values[key] != "" {
			return errors.New("HTTP local rejects other profiles")
		}
	}
	if values["POSTGRES_DB"] != "addp_test" || (values["POSTGRES_HOST"] != "127.0.0.1" && values["POSTGRES_HOST"] != "localhost") {
		return errors.New("HTTP local requires isolated loopback addp_test")
	}
	if _, err := httpQualityLocalCluster(environment); err != nil {
		return err
	}
	return validateExecutionTraceOutput(output)
}

func runHTTPQualityLocal(environment []string, output, suite string) error {
	if err := validateHTTPQualityLocal(environment, output, suite); err != nil {
		return err
	}
	cfg := config.Load()
	if cfg.PostgresDB != "addp_test" {
		return errors.New("HTTP local database changed")
	}
	db, err := gorm.Open(postgres.Open(cfg.PostgreSQLDSN()), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		return errors.New("HTTP local database unavailable")
	}
	sqlDB, err := db.DB()
	if err != nil {
		return err
	}
	defer sqlDB.Close()
	if err := validateHTTPQualityLocalDatabase(db, environment); err != nil {
		return err
	}
	if err := iamcli.RequireCurrentMigration(db); err != nil {
		return err
	}
	var users int64
	if err := db.Table("system.principals").Where("principal_type = ?", "user").Count(&users).Error; err != nil {
		return err
	}
	if users != 0 {
		return errors.New("HTTP local database already has users")
	}
	return prepareLocalIdentities(db, cfg, output, localIdentityProfile{
		id: "local-http-quality", title: "Local HTTP quality", prefix: "ADDP_HTTP_LOCAL", role: "local.http_quality",
		permissions: []string{"agent.session.read"}, keys: httpQualityLocalKeys(),
	})
}

func validateHTTPQualityLocalDatabase(db *gorm.DB, environment []string) error {
	expected, err := httpQualityLocalCluster(environment)
	if err != nil {
		return err
	}
	var identity struct{ Database, Cluster string }
	if err := db.Raw("SELECT current_database() AS database, current_setting('cluster_name') AS cluster").Scan(&identity).Error; err != nil {
		return errors.New("HTTP local cluster identity unavailable")
	}
	if identity.Database != "addp_test" || identity.Cluster != expected {
		return errors.New("HTTP local refuses shared or foreign PostgreSQL")
	}
	return nil
}

func httpQualityLocalSelected(environment []string, suite string) bool {
	if suite == httpQualityLocalSuite {
		return true
	}
	for _, entry := range environment {
		if strings.HasPrefix(entry, "ADDP_HTTP_LOCAL=") && entry != "ADDP_HTTP_LOCAL=" {
			return true
		}
	}
	return false
}
