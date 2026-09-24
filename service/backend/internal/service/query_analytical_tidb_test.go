package service

import (
	"os"
	"testing"

	commonmodels "github.com/addp/common/models"
)

func TestIntegrationTiDBAnalyticalServiceExecution(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("requires the standard TiDB gate")
	}
	testAnalyticalQueryExecution(t, "tidb", commonmodels.ConnectionInfo{
		"host": os.Getenv("ADDP_TEST_TIDB_HOST"), "port": os.Getenv("ADDP_TEST_TIDB_PORT"),
		"user": os.Getenv("ADDP_TEST_TIDB_USER"), "password": os.Getenv("ADDP_TEST_TIDB_PASSWORD"), "database": "mysql",
	})
}
