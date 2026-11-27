package oceanbase

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"reflect"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
)

func TestIntegrationOceanBaseGuardedQueryReadSession(t *testing.T) {
	if os.Getenv("ADDP_OCEANBASE_INTEGRATION") != "1" {
		t.Skip("set ADDP_OCEANBASE_INTEGRATION=1 to run OceanBase integration test")
	}
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	p := &Plugin{}
	adminInfo := oceanBaseIntegrationConnInfo()
	database := oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_DATABASE", "addp_oceanbase_disposable")
	dsn, err := p.BuildDSN(adminInfo)
	if err != nil {
		t.Fatal(err)
	}
	admin, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()
	const reader = "'addp_query_read_gate'@'%'"
	const password = "LocalOceanBaseReadOnly18!"
	if _, err := admin.ExecContext(ctx, "CREATE USER "+reader+" IDENTIFIED BY '"+password+"'"); err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanup, close := context.WithTimeout(context.Background(), 10*time.Second)
		defer close()
		if _, err := admin.ExecContext(cleanup, "DROP USER "+reader); err != nil {
			t.Error(err)
		}
	}()
	for _, name := range []string{"addp_query_read_first", "addp_query_read_second"} {
		statements := []string{
			"CREATE TABLE " + name + " (id BIGINT PRIMARY KEY, label VARCHAR(30))",
			"INSERT INTO " + name + " VALUES (7, 'authorized result')",
			"GRANT SELECT ON `" + database + "`." + name + " TO " + reader,
		}
		for _, statement := range statements {
			if _, err := admin.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
	}
	readerInfo := plugin.ConnectionInfo{}
	for k, v := range adminInfo {
		readerInfo[k] = v
	}
	readerInfo["user"], readerInfo["password"] = "addp_query_read_gate@"+oceanBaseIntegrationEnv("ADDP_TEST_OCEANBASE_TENANT", "test"), password
	const engineID = 95002
	first := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_query_read_first")
	second := plugin.TabularItemPath(engineID, plugin.EngineCatalogTermDatabase, database, "addp_query_read_second")
	const single = "SELECT * FROM addp_query_read_first WHERE id=:id"
	denied := errors.New("source authorization refused")
	for _, tc := range []struct {
		name, statement string
		sources         []plugin.EngineCatalogPath
		allowed         bool
	}{
		{"authorized", single, []plugin.EngineCatalogPath{first}, true},
		{"ungranted_join", "SELECT a.id FROM addp_query_read_first a JOIN addp_query_read_second b ON b.id=a.id WHERE a.id=:id", []plugin.EngineCatalogPath{first, second}, false},
		{"withdrawn_same_query", single, []plugin.EngineCatalogPath{first}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			prepared, err := p.PrepareQuery(ctx, readerInfo, plugin.QueryRequest{
				EngineID: engineID, Language: "sql", Query: tc.statement,
				Options: plugin.QueryOptions{ReadOnly: true, Parameters: map[string]any{"id": 7}},
			})
			if err != nil {
				t.Fatal(err)
			}
			want, err := plugin.NewQueryReadSet(tc.sources...)
			if err != nil {
				t.Fatal(err)
			}
			calls := 0
			session, err := p.OpenGuardedQueryReadSession(ctx, prepared, func(_ context.Context, got *plugin.QueryReadSet) error {
				calls++
				if !reflect.DeepEqual(got, want) {
					t.Fatalf("sources=%+v; want=%+v", got, want)
				}
				if tc.allowed {
					return nil
				}
				return denied
			})
			if session != nil {
				defer session.Close(ctx)
			}
			if !tc.allowed {
				if calls != 1 || session != nil || !errors.Is(err, denied) {
					t.Fatalf("expected denial before business read: calls=%d session=%t err=%v", calls, session != nil, err)
				}
				return
			}
			if err != nil || calls != 1 {
				t.Fatalf("open query calls=%d err=%v", calls, err)
			}
			batch, err := session.ReadBatch(ctx, 2)
			if err != nil || batch == nil || len(batch.Rows) != 1 || batch.Rows[0]["id"] != int64(7) || batch.Rows[0]["label"] != "authorized result" {
				t.Fatalf("rows=%+v err=%v", batch, err)
			}
		})
	}
}
