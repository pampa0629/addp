package oracle

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOracleGuardedQueryRead(t *testing.T) {
	if os.Getenv("ADDP_ORACLE_SPATIAL_INTEGRATION") != "1" {
		t.Skip("requires the disposable Oracle Spatial gate")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	p := &OraclePlugin{}
	info := oracleIntegrationConnInfo(t)
	schema := strings.ToUpper(plugin.GetString(info, "user"))
	adminInfo := oracleIntegrationConnInfo(t)
	adminInfo["user"], adminInfo["password"] = "system", os.Getenv("ADDP_TEST_ORACLE_ADMIN_PASSWORD")
	open := func(info plugin.ConnectionInfo) *sql.DB {
		t.Helper()
		dsn, err := p.BuildDSN(info)
		if err != nil {
			t.Fatal(err)
		}
		db, err := sql.Open("oracle", dsn)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { db.Close() })
		return db
	}
	admin, owner := open(adminInfo), open(info)
	for _, stmt := range []string{
		"CREATE USER ADDP_GUARD_READER IDENTIFIED BY Only_fixture331",
		"GRANT CREATE SESSION TO ADDP_GUARD_READER",
		fmt.Sprintf("CREATE AUDIT POLICY ADDP_GUARD_SQL ACTIONS SELECT ON %s.CUSTOMER_LOCATIONS, SELECT ON %s.ORDERS", schema, schema),
		"AUDIT POLICY ADDP_GUARD_SQL BY ADDP_GUARD_READER",
	} {
		if _, err := admin.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	for _, stmt := range []string{
		"GRANT SELECT ON CUSTOMER_LOCATIONS TO ADDP_GUARD_READER",
		"GRANT SELECT ON ORDERS TO ADDP_GUARD_READER",
		"CREATE VIEW ADDP_GUARD_VIEW AS SELECT ID FROM CUSTOMER_LOCATIONS",
		"GRANT SELECT ON ADDP_GUARD_VIEW TO ADDP_GUARD_READER",
	} {
		if _, err := owner.ExecContext(ctx, stmt); err != nil {
			t.Fatal(err)
		}
	}
	info["user"], info["password"] = "ADDP_GUARD_READER", "Only_fixture331"
	const engineID = 94023
	locations := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, schema, "CUSTOMER_LOCATIONS")
	orders := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermSchema, schema, "ORDERS")
	readDenied := errors.New("source grant denied")
	source, orderSource := schema+".CUSTOMER_LOCATIONS", schema+".ORDERS"
	plain := "SELECT ID FROM " + source + " WHERE ID = :1 FETCH FIRST 1 ROWS ONLY"
	readAudit := func(t *testing.T) int {
		t.Helper()
		var count int
		err := admin.QueryRowContext(ctx, `
			SELECT COUNT(*) FROM (
				SELECT DISTINCT SESSIONID, STATEMENT_ID FROM UNIFIED_AUDIT_TRAIL
				WHERE DBUSERNAME = 'ADDP_GUARD_READER' AND ACTION_NAME = 'SELECT'
				AND OBJECT_SCHEMA = :1 AND OBJECT_NAME IN ('CUSTOMER_LOCATIONS', 'ORDERS')
			)
		`, schema).Scan(&count)
		if err != nil {
			t.Fatal(err)
		}
		return count
	}
	for _, tc := range []struct {
		name, query         string
		args                []any
		paths               []plugin.EngineCatalogPath
		deny                error
		column, valuePrefix string
	}{
		{"ordinary", plain, []any{int64(1)}, []plugin.EngineCatalogPath{locations}, nil, "ID", "1"},
		{"ungranted_join", "SELECT a.ID FROM " + source + " a JOIN " + orderSource + " b ON a.ID = b.CUSTOMER_ID", nil, []plugin.EngineCatalogPath{locations, orders}, readDenied, "", ""},
		{"revoked", plain, []any{int64(1)}, []plugin.EngineCatalogPath{locations}, readDenied, "", ""},
		{"hosted_plain", `SELECT TO_CHAR(ID) AS "optional_text" FROM ` + source + " WHERE ID = 1", nil, []plugin.EngineCatalogPath{locations}, nil, "optional_text", "1"},
		{"view_unresolved", "SELECT ID FROM " + schema + ".ADDP_GUARD_VIEW", nil, nil, plugin.ErrQueryReadSetUnresolved, "", ""},
		{"spatial", "SELECT MDSYS.SDO_UTIL.TO_WKTGEOMETRY(MDSYS.SDO_CS.TRANSFORM(a.SHAPE, 3857)) AS WKT FROM " + source + " a WHERE a.ID = 1", nil, []plugin.EngineCatalogPath{locations}, nil, "WKT", "POINT ("},
		{"spatial_sample", "SELECT * FROM " + source + " WHERE ID = 1", nil, []plugin.EngineCatalogPath{locations}, nil, "SHAPE", "{"},
		{"cte", "WITH picked AS (SELECT ID FROM " + source + " WHERE ID = 1) SELECT ID FROM picked", nil, []plugin.EngineCatalogPath{locations}, nil, "ID", "1"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			before := readAudit(t)
			prepared, err := p.PrepareQuery(ctx, info, plugin.QueryRequest{Language: "sql", EngineID: engineID, Query: tc.query, Options: plugin.QueryOptions{ReadOnly: true, Args: tc.args}})
			if err != nil {
				t.Fatal(err)
			}
			checked := false
			session, err := p.OpenGuardedQueryReadSession(ctx, prepared, func(_ context.Context, actual *plugin.QueryReadSet) error {
				checked = true
				want, err := plugin.NewQueryReadSet(tc.paths...)
				if err != nil || !reflect.DeepEqual(actual.Paths, want.Paths) {
					t.Fatalf("actual sources=%+v want=%+v err=%v", actual, want, err)
				}
				if got := readAudit(t); got != before {
					t.Fatalf("business SELECT before Grant verdict: %d", got-before)
				}
				return tc.deny
			})
			if checked != (len(tc.paths) > 0) || !errors.Is(err, tc.deny) {
				t.Fatalf("guard authorize called=%t, error=%v", checked, err)
			}
			wantCount := 0
			if tc.deny == nil {
				defer session.Close(ctx)
				batch, err := session.ReadBatch(ctx, 5)
				if err != nil || len(batch.Rows) != 1 || !strings.HasPrefix(fmt.Sprint(batch.Rows[0][tc.column]), tc.valuePrefix) {
					t.Fatalf("result=%+v err=%v", batch, err)
				}
				if err := session.Close(ctx); err != nil {
					t.Fatal(err)
				}
				wantCount = 1
			} else if session != nil {
				_ = session.Close(ctx)
				t.Fatal("denied request opened a cursor")
			}
			after := readAudit(t)
			if after-before != wantCount {
				t.Fatalf("source business SELECT=%d want=%d", after-before, wantCount)
			}
			t.Logf("business_select=%d", after-before)
		})
	}
}
