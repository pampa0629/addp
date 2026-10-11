package tidb

import (
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestIntegrationTiDBTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_TIDB_INTEGRATION") != "1" {
		t.Skip("run through the Common TiDB T2 gate")
	}
	p := &Plugin{}
	connInfo := tidbIntegrationConnInfo()
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	database := tidbIntegrationEnv("ADDP_TEST_TIDB_DATABASE", "addp_tidb_disposable")
	writetest.MySQLJSONWrites(t, db, p, connInfo, database, nil, nil)
}
