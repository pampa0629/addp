package preview

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	commonquery "github.com/addp/common/query"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	"github.com/addp/common/opaquetoken"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
)

func TestIntegrationPostgresManagerPreviewPreparedPageReadSet(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	parsed, err := url.Parse(os.Getenv("MANAGER_POSTGRES_TEST_DSN"))
	if err != nil || parsed.User == nil || parsed.Port() == "" || len(parsed.Path) < 2 {
		t.Fatal("explicit test DSN is required")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal("invalid test port")
	}
	password, _ := parsed.User.Password()
	conn := plugin.ConnectionInfo{"host": parsed.Hostname(), "port": port, "user": parsed.User.Username(), "password": password, "database": parsed.Path[1:], "sslmode": parsed.Query().Get("sslmode")}
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal("open test database failed")
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()
	schema := fmt.Sprintf("manager_preview_it_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal("create isolated preview schema failed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		// All objects are owned by this randomly named, gate-only fixture.
		if _, err := db.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Error("preview fixture cleanup failed")
		}
		var remains bool
		if err := db.QueryRowContext(cleanup, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&remains); err != nil || remains {
			t.Error("preview fixture cleanup not confirmed")
		}
	})
	for _, statement := range []string{
		`CREATE TABLE "` + schema + `"."base" (id bigint PRIMARY KEY, status text NOT NULL)`,
		`INSERT INTO "` + schema + `"."base" VALUES (1,'active'),(2,'active'),(3,'inactive')`,
		`CREATE VIEW "` + schema + `"."view" AS SELECT id,status FROM "` + schema + `"."base"`,
		`CREATE TABLE "` + schema + `"."protected_spatial" (id bigint PRIMARY KEY, phone text, location geometry(Point,4326))`,
		`INSERT INTO "` + schema + `"."protected_spatial" VALUES (1,'13801017055',ST_SetSRID(ST_Point(1,2),4326)),(2,'15810162486',ST_SetSRID(ST_Point(3,4),4326))`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal("create controlled page fixture failed")
		}
	}
	pg := &postgresql.PostgreSQLPlugin{}
	t.Run("two replicas seek composite constraint in index order", func(t *testing.T) {
		const engineID = 14815
		t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
		if _, err := db.ExecContext(ctx, `CREATE TABLE "`+schema+`".seek_rows(id bigint NOT NULL, grp text NOT NULL, PRIMARY KEY(grp,id))`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.ExecContext(ctx, `INSERT INTO "`+schema+`".seek_rows VALUES(20,'b'),(10,'b'),(20,'a'),(10,'a')`); err != nil {
			t.Fatal(err)
		}
		path := plugin.TabularItemPath(engineID, "schema", schema, "seek_rows")
		makeReq := func(page int, cursor string) *PreviewRequest {
			return &PreviewRequest{Engine: &models.Engine{ID: engineID, EngineType: "postgresql", ConnectionInfo: map[string]interface{}(conn)}, EnginePlugin: pg, ProviderPath: path, Schema: schema, Table: "seek_rows", Page: page, PageSize: 1, TablePage: &TablePageRequest{Codec: opaquetoken.New([]byte("only this fixture"), "manager.table-preview.v1"), Subject: dataprotection.SubjectReference{Type: "user", ID: "1"}, Cursor: cursor}, executePrepared: func(ctx context.Context, provider plugin.EnginePlugin, plan plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
				set, err := plan.ReadSet(ctx)
				if err != nil || set == nil || !reflect.DeepEqual(set.Paths, []plugin.EngineCatalogPath{path}) {
					t.Fatalf("page source identity: %v %v", set, err)
				}
				return executeDatabasePreviewTestPlan(ctx, provider, plan)
			}}
		}
		var token string
		for i, want := range []string{"a10", "a20", "b10", "b20"} {
			page, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, makeReq(i+1, token))
			if err != nil || !page.Sequential || len(page.Rows) != 1 || fmt.Sprint(page.Rows[0]["grp"], page.Rows[0]["id"]) != want || page.HasMore != (i < 3) {
				t.Fatalf("page %d: %+v %v", i+1, page, err)
			}
			if page.HasMore {
				token, err = page.FinishPage(page.Rows[0])
				if err != nil {
					t.Fatal(err)
				}
			}
		}
		req := makeReq(4, token)
		req.TablePage.Subject.ID = "2"
		if _, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, req); !errors.Is(err, ErrTablePageCursor) {
			t.Fatalf("wrong subject: %v", err)
		}
		req = makeReq(4, token)
		req.TablePage.Rules = []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{Path: []dataprotection.PathSegment{{Name: "grp"}}}, Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress}}}
		if _, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, req); !errors.Is(err, ErrTablePageCursor) {
			t.Fatalf("key no longer readable: %v", err)
		}
		req.TablePage.Cursor = ""
		page, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, req)
		if err != nil || page.Sequential {
			t.Fatalf("protected ordinary pagination: %+v %v", page, err)
		}
	})
	t.Run("deep page seeks into source index", func(t *testing.T) {
		relation := `"` + schema + `".deep_page`
		for _, statement := range []string{"CREATE TABLE " + relation + " (id bigint PRIMARY KEY)", "INSERT INTO " + relation + " SELECT generate_series(1,60000)", "ANALYZE " + relation} {
			if _, err := db.ExecContext(ctx, statement); err != nil {
				t.Fatal(err)
			}
		}
		dialect := commonquery.ForDialect("postgresql")
		seek := tableSeek{keys: []string{"id"}, after: []string{"50000"}}
		predicate, args := seek.predicate(dialect, "", nil)
		planRows := func(where string, args []interface{}, offset int) int {
			query := databasePreviewPostgreSQLPrimaryKeyPageQuery(dialect, `"__addp_src"."id"`, schema, "deep_page", seek.keys, where, 51, offset)
			var raw string
			if err := db.QueryRowContext(ctx, "EXPLAIN (ANALYZE, FORMAT JSON) "+query, args...).Scan(&raw); err != nil {
				t.Fatal(err)
			}
			type plan struct {
				Node      string `json:"Node Type"`
				Rows      int    `json:"Actual Rows"`
				Condition string `json:"Index Cond"`
				Children  []plan `json:"Plans"`
			}
			var out []struct {
				Root plan `json:"Plan"`
			}
			if err := json.Unmarshal([]byte(raw), &out); err != nil {
				t.Fatal(err)
			}
			var maxRows int
			found := false
			var walk func(plan)
			walk = func(p plan) {
				if strings.Contains(p.Node, "Index") {
					maxRows = max(maxRows, p.Rows)
					found = found || strings.Contains(p.Condition, "50000")
				}
				for _, child := range p.Children {
					walk(child)
				}
			}
			walk(out[0].Root)
			if where != "" && !found {
				t.Fatalf("no index range for deep cursor: %s", raw)
			}
			return maxRows
		}
		indexed := planRows(predicate, args, 0)
		offsetRows := planRows("", nil, 50000)
		if indexed != 51 || offsetRows < 50051 {
			t.Fatalf("index rows read: seek=%d offset=%d", indexed, offsetRows)
		}
		t.Logf("position 50000, page+lookahead 51, maximum rows from index node: seek=%d offset=%d", indexed, offsetRows)
	})
	t.Run("prepared profile pages", func(t *testing.T) {
		// This isolated Provider test proves preparation and exact plan execution,
		// not IAM approval. Production profiling remains closed until owner gates connect.
		path := plugin.TabularItemPath(12, "schema", schema, "view")
		path.Segments[len(path.Segments)-1].Kind = "view"
		req := &PreviewRequest{
			Engine:       &models.Engine{ID: 12, EngineType: "postgresql", ConnectionInfo: map[string]interface{}(conn)},
			EnginePlugin: pg, ProviderPath: path,
			DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd, Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}}},
		}
		fields := []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: true}, {Name: "status", Type: datatype.FieldTypeString}}
		pages, err := prepareProfilePages(ctx, req, fields, []TablePage{{Offset: 0, Limit: 1}, {Offset: 1, Limit: 1}})
		if err != nil {
			t.Fatalf("prepare bounded pages: %v", err)
		}
		want, _ := plugin.NewQueryReadSet(path, plugin.TabularItemPath(12, "schema", schema, "base"))
		if !reflect.DeepEqual(pages.ReadSet(), want) {
			t.Fatal("complete view dependencies were not prepared")
		}
		for index := range pages.Positions() {
			plan, err := pages.Query(index)
			if err != nil {
				t.Fatal(err)
			}
			set, err := plan.ReadSet(ctx)
			if err != nil || !reflect.DeepEqual(set, want) {
				t.Fatal("page source binding changed")
			}
			result, err := plan.Execute(ctx)
			if err != nil || result == nil || len(result.Rows) != 1 || result.Rows[0]["status"] != "active" || fmt.Sprint(result.Rows[0]["id"]) != strconv.Itoa(index+1) {
				t.Fatalf("bounded page %d result: %#v %v", index, result, err)
			}
			if _, err := plan.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
				t.Fatal("page was executed twice")
			}
		}
	})
	t.Run("protected spatial display and key page", func(t *testing.T) {
		const engineID = 8412
		t.Cleanup(func() { _ = plugin.ClosePool(engineID) })
		locator := &resourcetree.ResourceLocator{EngineID: engineID, Path: []string{schema, "protected_spatial"}, Type: resourcetree.TypeTable}
		resolver := NewPreviewResolver(NewPreviewRegistry(), nil, nil)
		provider := &DatabaseTablePreviewProvider{}
		var previousPool *sql.DB
		var oldRequest *PreviewRequest
		rules := []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{Key: "phone", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}}}
		for index, want := range []struct{ phone, point string }{{"138****7055", "POINT(1 2)"}, {"158****2486", "POINT(3 4)"}} {
			info := make(plugin.ConnectionInfo, len(conn)+2)
			for key, value := range conn {
				info[key] = value
			}
			info["pool_max_open_conns"], info["pool_max_idle_conns"] = index+1, 0
			engine := &models.Engine{ID: engineID, EngineType: "postgresql", Version: int64(index + 1), ConnectionInfo: map[string]interface{}(info)}
			req, err := resolver.buildProviderRequest(ctx, &PreviewResolverRequest{
				Locator: locator, Engine: engine, ItemType: "table",
				Pagination: &Pagination{Page: index + 1, PageSize: 1},
			})
			if err != nil {
				t.Fatal(err)
			}
			executions := 0
			req.executePrepared = func(ctx context.Context, _ plugin.EnginePlugin, plan plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
				if plugin.PoolEngineVersion(ctx, engineID) != engine.Version {
					t.Fatal("preview did not carry the resolved Engine version")
				}
				lineage, err := plan.OutputLineage(ctx)
				if err != nil || len(lineage.Sources) != 1 {
					t.Fatalf("spatial preview lineage: %v", err)
				}
				operations, err := dataprotection.QueryOutputTransformsAt(lineage.Sources[0], "preview", rules, dataprotection.SubjectReference{}, time.Now().UTC())
				if err != nil {
					t.Fatal(err)
				}
				if err := plan.(plugin.OutputTransformPreparedQuery).BindOutputTransforms(ctx, operations); err != nil {
					t.Fatalf("generated protected preview could not bind: %v", err)
				}
				executions++
				result, err := plan.Execute(ctx)
				if err != nil {
					return nil, nil, err
				}
				if len(result.Rows) != 2-index || result.Rows[0]["phone"] != want.phone || result.Rows[0]["location"] != want.point {
					t.Fatal("native display/page/masking result changed")
				}
				if _, err := plan.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
					t.Fatal("preview query executed twice")
				}
				return result, &dataprotection.PreparedTableProtection{Apply: func(result *plugin.QueryResult) error {
					return dataprotection.ProtectQueryResultSource(result, lineage.Sources[0], "preview", rules, dataprotection.SubjectReference{})
				}}, nil
			}
			result, err := provider.Preview(ctx, req)
			if err != nil || result == nil || result.PreparedProtection == nil || executions != 1 {
				t.Fatalf("protected spatial page failed: %v", err)
			}
			if result.Total != nil || result.HasMore != (index == 0) || len(result.Rows) != 1 {
				t.Fatalf("protected spatial page exposed incorrect pagination: total=%v more=%v rows=%d", result.Total, result.HasMore, len(result.Rows))
			}
			protected := &plugin.QueryResult{Rows: result.Rows, Columns: result.Columns}
			if err := result.PreparedProtection.Apply(protected); err != nil || protected.Rows[0]["phone"] != want.phone || protected.Rows[0]["location"] != want.point {
				t.Fatalf("final spatial preview changed: %v", err)
			}
			if previousPool != nil && previousPool.PingContext(ctx) == nil {
				t.Fatal("preview retained the previous pool after loading newer settings")
			}
			pool, err := plugin.GetOrCreatePoolFromFactory(&plugin.Engine{
				ID: engineID, EngineType: engine.EngineType, Version: engine.Version, ConnectionInfo: info,
			}, nil)
			if err != nil {
				t.Fatal(err)
			}
			previousPool, err = pool.DB()
			if err != nil || previousPool.Stats().MaxOpenConnections != index+1 {
				t.Fatalf("preview did not use the configured pool: %v", err)
			}
			if index == 0 {
				oldRequest = req
			}
		}
		if _, err := provider.Preview(ctx, oldRequest); !errors.Is(err, plugin.ErrStalePoolConfiguration) {
			t.Fatalf("late preview with the old Engine version was not rejected: %v", err)
		}
		if err := previousPool.PingContext(ctx); err != nil {
			t.Fatalf("late preview closed the current pool: %v", err)
		}
		var original string
		if err := db.QueryRowContext(ctx, `SELECT phone FROM "`+schema+`"."protected_spatial" WHERE id=1`).Scan(&original); err != nil || original != "13801017055" {
			t.Fatal("preview changed source data")
		}
	})
	for _, name := range []string{"base", "view"} {
		t.Run(name, func(t *testing.T) {
			path := plugin.TabularItemPath(12, "schema", schema, name)
			if name == "view" {
				path.Segments[len(path.Segments)-1].Kind = "view"
			}
			req := &PreviewRequest{
				Engine:       &models.Engine{ID: 12, EngineType: "postgresql", ConnectionInfo: map[string]interface{}(conn)},
				EnginePlugin: pg, ProviderPath: path, Schema: schema, Table: name, Page: 2, PageSize: 1,
				DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd, Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}}},
			}
			executions := 0
			req.executePrepared = func(ctx context.Context, _ plugin.EnginePlugin, plan plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
				readSet, err := plan.ReadSet(ctx)
				if err != nil {
					t.Fatalf("complete pagination read set: %v", err)
				}
				paths := []plugin.EngineCatalogPath{plugin.TabularItemPath(12, "schema", schema, "base")}
				if name == "view" {
					paths = append(paths, path)
				}
				want, _ := plugin.NewQueryReadSet(paths...)
				if !reflect.DeepEqual(readSet, want) {
					t.Fatalf("incomplete preview sources: %#v", readSet)
				}
				lineage, err := plan.OutputLineage(ctx)
				if err != nil || lineage == nil || len(lineage.Sources) != len(paths) {
					t.Fatalf("pagination output lineage: %#v %v", lineage, err)
				}
				executions++
				result, err := plan.Execute(ctx)
				if err != nil {
					return nil, nil, err
				}
				if _, err := plan.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
					t.Fatal("plan executed twice")
				}
				return result, &dataprotection.PreparedTableProtection{Apply: func(*plugin.QueryResult) error { return nil }}, nil
			}
			result, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, req)
			if err != nil || executions != 1 || len(result.Rows) != 1 || result.Rows[0]["status"] != "active" || result.PreparedProtection == nil {
				t.Fatalf("controlled page preview: %#v %v reads=%d", result, err, executions)
			}
			if result.Total != nil || result.HasMore {
				t.Fatalf("last filtered page should have unknown total and no next page: total=%v more=%v", result.Total, result.HasMore)
			}
			if name == "base" && fmt.Sprint(result.Rows[0]["id"]) != "2" {
				t.Fatalf("bound page offset lost: %#v", result.Rows)
			}
		})
	}
}
