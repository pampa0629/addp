package service

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/postgresql"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/preview"
)

// Standard Manager T2 only: all writes belong to a random schema in addp_test.
// This proves native plan execution, not the User/Gateway/System/Worker T4 flow.
func TestIntegrationPostgresManagerProfilePreparedSourceBoundary(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("requires standard Manager PostgreSQL gate")
	}
	parsed, err := url.Parse(os.Getenv("MANAGER_POSTGRES_TEST_DSN"))
	if err != nil || parsed.User == nil || parsed.Port() == "" || len(parsed.Path) < 2 {
		t.Fatal("explicit test DSN required")
	}
	port, err := strconv.Atoi(parsed.Port())
	if err != nil {
		t.Fatal("invalid test port")
	}
	password, _ := parsed.User.Password()
	conn := commonModels.ConnectionInfo{"host": parsed.Hostname(), "port": port, "user": parsed.User.Username(), "password": password, "database": parsed.Path[1:], "sslmode": parsed.Query().Get("sslmode")}
	db, err := sql.Open("postgres", parsed.String())
	if err != nil {
		t.Fatal("open test database failed")
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx, cancel := context.WithTimeout(t.Context(), time.Minute)
	defer cancel()
	schema := fmt.Sprintf("manager_profile_sample_it_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal("create isolated schema failed")
	}
	t.Cleanup(func() {
		cleanup, stop := context.WithTimeout(context.Background(), 10*time.Second)
		defer stop()
		if _, err := db.ExecContext(cleanup, `DROP SCHEMA "`+schema+`" CASCADE`); err != nil {
			t.Error("fixture cleanup failed")
		}
		var remains bool
		if err := db.QueryRowContext(cleanup, `SELECT EXISTS(SELECT 1 FROM pg_namespace WHERE nspname=$1)`, schema).Scan(&remains); err != nil || remains {
			t.Error("fixture cleanup unconfirmed")
		}
	})
	for _, statement := range []string{
		`CREATE TABLE "` + schema + `".base (id bigint PRIMARY KEY, status text NOT NULL)`,
		`INSERT INTO "` + schema + `".base SELECT i, 'active' FROM generate_series(1,6) i`,
		`CREATE VIEW "` + schema + `".v AS SELECT id,status FROM "` + schema + `".base`,
	} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal("fixture creation failed")
		}
	}
	sampler := NewPreviewDataProfileSampleProvider(&preview.PreviewResolver{}, nil)
	budget := DataProfileBudget{SampleSize: 2, MaxRowsScanned: 6, PageSize: 2, Timeout: 30 * time.Second}
	scope := dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}
	prepare := func(t *testing.T, name string, dataScope dataprofile.DataScope) (*DataProfileTarget, *DataProfileSamplePlan, error) {
		t.Helper()
		locator, err := resourcetree.ParseURI("addp://engine/12/path/" + schema + "/" + name + "?type=table")
		if err != nil {
			t.Fatal(err)
		}
		tenant, item, count := uint(7), uint(1), int64(6)
		target := &DataProfileTarget{EngineID: 12, Locator: locator.ToURI(), RowCount: &count, RowCountExact: true,
			Fields:   []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, PrimaryKey: name == "base"}, {Name: "status", Type: datatype.FieldTypeString}},
			resolved: &preview.PreviewResolverRequest{Locator: locator, Engine: &commonModels.Engine{ID: 12, EngineType: "postgresql", ConnectionInfo: conn}, Metadata: &commonModels.MetaNode{}, TenantID: &tenant, MetaItemID: &item},
		}
		plan, err := sampler.Prepare(ctx, target, dataScope, budget)
		return target, plan, err
	}
	t.Run("ordinary table samples same plan", func(t *testing.T) {
		target, plan, err := prepare(t, "base", scope)
		if err != nil {
			t.Fatal(err)
		}
		checks := 0
		result, err := sampler.Sample(ctx, target, scope, budget, plan, func(context.Context) error {
			checks++
			return nil
		})
		if err != nil || result == nil || result.RowsScanned != 6 || len(result.Rows) != 2 || checks != 3 ||
			len(result.ReadSet.Paths) != 1 || !result.RowCountExact || result.Partial || result.Truncated {
			t.Fatalf("ordinary table sampling failed: result=%+v checks=%d err=%v", result, checks, err)
		}
		query, _ := plan.pages.Query(0)
		if _, err := query.Execute(ctx); !errors.Is(err, plugin.ErrPreparedQueryConsumed) {
			t.Fatal("production did not consume the original one-shot page")
		}
	})
	t.Run("denied second page is not read", func(t *testing.T) {
		target, plan, err := prepare(t, "base", scope)
		if err != nil {
			t.Fatal(err)
		}
		checks := 0
		result, err := sampler.Sample(ctx, target, scope, budget, plan, func(context.Context) error {
			checks++
			if checks == 2 {
				return ErrDataProfileSourceAuthorizationRequired
			}
			return nil
		})
		if result != nil || !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) || checks != 2 {
			t.Fatal("revocation returned partial data")
		}
		query, _ := plan.pages.Query(1)
		// Test-only fixture read proves the denied production page is unconsumed.
		if _, err := query.Execute(ctx); err != nil {
			t.Fatal("denied production page was consumed")
		}
	})
	t.Run("condition limits sampled rows without a count query", func(t *testing.T) {
		condition := dataprofile.DataScope{Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "id", Operator: "gt", Value: int64(3)}}}
		target, plan, err := prepare(t, "base", condition)
		if err != nil {
			t.Fatal(err)
		}
		checks := 0
		result, err := sampler.Sample(ctx, target, condition, budget, plan, func(context.Context) error { checks++; return nil })
		if err != nil || result == nil || result.RowsScanned != 3 || len(result.Rows) != 2 || checks != 3 ||
			result.RowCount != nil || result.RowCountExact || !result.Partial {
			t.Fatalf("conditional sampling changed range or cardinality: result=%+v err=%v", result, err)
		}
		for _, row := range result.Rows {
			id, ok := row["id"].(int64)
			if !ok || id <= 3 {
				t.Fatalf("out-of-scope sampled row: %+v", row)
			}
		}
	})
	t.Run("view is explicitly unsupported in first release", func(t *testing.T) {
		_, plan, err := prepare(t, "v", scope)
		if plan != nil || !errors.Is(err, ErrDataProfileUnsupported) {
			t.Fatal("view preparation accepted unsupported sampling")
		}
	})
}
