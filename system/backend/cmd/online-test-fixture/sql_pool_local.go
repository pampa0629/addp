package main

import (
	"errors"
	"regexp"

	"gorm.io/gorm"
)

const sqlPoolLocalRun = "ADDP_SQL_POOL_LOCAL_RUN_ID"
const sqlPoolLocalDatabase = "addp_test"

var sqlPoolRunPattern = regexp.MustCompile(`^addp-sql-pool-local-[0-9a-f]{32}$`)

func validateSQLPoolLocalEnvironment(values map[string]string, suite, output string) error {
	if suite != "sql-pool-consumer-flow" || !sqlPoolRunPattern.MatchString(values[sqlPoolLocalRun]) ||
		values["POSTGRES_DB"] != sqlPoolLocalDatabase || values["POSTGRES_HOST"] != "127.0.0.1" {
		return errors.New("local SQL pool requires its dedicated loopback database and run ID")
	}
	for _, key := range []string{"GITHUB_ACTIONS", "ADDP_ONLINE_HOSTED", "ADDP_ONLINE_OWNER_MANAGED", "ADDP_ONLINE_LOCAL_SGM", "ADDP_EXECUTION_TRACE_LOCAL", "ADDP_CATALOG_BROWSER_LOCAL"} {
		if values[key] != "" {
			return errors.New("local SQL pool refuses other fixture profiles")
		}
	}
	return validateExecutionTraceOutput(output)
}

func requireSQLPoolLocalDatabase(db *gorm.DB, run string) error {
	var local bool
	if err := db.Raw("SELECT current_database() = ? AND current_setting('cluster_name') = ?", sqlPoolLocalDatabase, run).Scan(&local).Error; err != nil || !local {
		return errors.New("local SQL pool refuses a shared or foreign PostgreSQL cluster")
	}
	return nil
}
