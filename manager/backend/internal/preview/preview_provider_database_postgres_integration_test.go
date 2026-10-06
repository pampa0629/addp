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
