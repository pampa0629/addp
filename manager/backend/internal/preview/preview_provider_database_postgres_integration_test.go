package preview

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
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
		path := plugin.TabularItemPath(12, "schema", schema, "protected_spatial")
		rules := []dataprotection.Rule{{Action: "preview", Component: dataprotection.Component{Key: "phone", ValueType: "string", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}}, Decision: dataprotection.Decision{Effect: dataprotection.EffectMask, Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]interface{}{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: dataprotection.EffectSuppress}}}
		for index, want := range []struct{ phone, point string }{{"138****7055", "POINT(1 2)"}, {"158****2486", "POINT(3 4)"}} {
			req := &PreviewRequest{Engine: &models.Engine{ID: 12, EngineType: "postgresql", ConnectionInfo: map[string]interface{}(conn)}, EnginePlugin: pg, ProviderPath: path, Schema: schema, Table: "protected_spatial", Page: index + 1, PageSize: 1}
			executions := 0
			req.executePrepared = func(ctx context.Context, _ plugin.EnginePlugin, plan plugin.PreparedQuery) (*plugin.QueryResult, *dataprotection.PreparedTableProtection, error) {
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
				if len(result.Rows) != 1 || result.Rows[0]["phone"] != want.phone || result.Rows[0]["location"] != want.point {
					t.Fatal("native display/page/masking result changed")
				}
				if _, err := plan.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
					t.Fatal("preview query executed twice")
				}
				return result, &dataprotection.PreparedTableProtection{Apply: func(result *plugin.QueryResult) error {
					return dataprotection.ProtectQueryResultSource(result, lineage.Sources[0], "preview", rules, dataprotection.SubjectReference{})
				}}, nil
			}
			result, err := (&DatabaseTablePreviewProvider{}).Preview(ctx, req)
			if err != nil || result == nil || result.PreparedProtection == nil || executions != 1 {
				t.Fatalf("protected spatial page failed: %v", err)
			}
			protected := &plugin.QueryResult{Rows: result.Rows, Columns: result.Columns}
			if err := result.PreparedProtection.Apply(protected); err != nil || protected.Rows[0]["phone"] != want.phone || protected.Rows[0]["location"] != want.point {
				t.Fatalf("final spatial preview changed: %v", err)
			}
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
			if name == "base" && fmt.Sprint(result.Rows[0]["id"]) != "2" {
				t.Fatalf("bound page offset lost: %#v", result.Rows)
			}
		})
	}
}
