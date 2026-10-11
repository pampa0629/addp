package oceanbase

import (
	"database/sql"
	"os"
	"testing"

	"github.com/addp/common/engine/plugins/shared/writetest"
)

func TestIntegrationOceanBaseTypedJSONWrites(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("run through the Common OceanBase T2 gate")
	}
	p := &Plugin{}
	connInfo := oceanBaseIntegrationConnInfo()
	dsn, err := p.BuildDSN(connInfo)
	if err != nil {
		t.Fatal(err)
	}
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	database := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	writetest.MySQLJSONWrites(t, db, p, connInfo, database, nil, nil)
}
