package service

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonModels "github.com/addp/common/models"
	commonquery "github.com/addp/common/query"
	"github.com/addp/develop/backend/internal/protection"
	"github.com/addp/develop/backend/internal/repository"
	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// Exercise actual parser, catalog, protection gate and PostgreSQL driver errors,
// then persist through the same lease completion and safe observation as Worker.
func TestQueryFailureDiagnosticsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("DEVELOP_POSTGRES_TEST_DSN")
	if dsn == "" {
		t.Skip("DEVELOP_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	ctx := t.Context()
	schema := fmt.Sprintf("develop_diagnostic_%d", time.Now().UnixNano())
	if _, err := db.ExecContext(ctx, `CREATE SCHEMA "`+schema+`"`); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Error(err)
		}
	})
	prefix := `"` + schema + `".`
	for _, statement := range []string{"CREATE TABLE " + prefix + "a(id integer)", "INSERT INTO " + prefix + "a VALUES(1)"} {
		if _, err := db.ExecContext(ctx, statement); err != nil {
			t.Fatal(err)
		}
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	password, _ := u.User.Password()
	conn := plugin.ConnectionInfo{"host": u.Hostname(), "port": u.Port(), "user": u.User.Username(), "password": password, "database": strings.TrimPrefix(u.Path, "/"), "sslmode": "disable"}
	provider := &postgresql.PostgreSQLPlugin{}
	local, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	localSQL, _ := local.DB()
	localSQL.SetMaxOpenConns(1)
	defer localSQL.Close()
	if err := executiontest.EnsureSQLiteStore(local); err != nil {
		t.Fatal(err)
	}
	if err := local.Exec("ATTACH DATABASE ':memory:' AS develop").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(local, "develop", "develop", nil)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	// A tenant with another enrolled resource must still resolve this query's
	// exact read set; no unmanaged-tenant shortcut may hide the reported defect.
	projection := dataprotection.Projection{
		TargetEngineID: 1, SchemaVersion: dataprotection.ProjectionSchemaV5,
		ProjectionID: "diagnostic-other-resource", Revision: "00000000000000000001", ConsumerOwner: "develop", State: dataprotection.ProjectionStateEnrolling,
		Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonModels.GenerateItemFingerprint(91, schema+".other")},
		Rules:  []dataprotection.Rule{}, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour))}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	if err := store.ApplyBatch(ctx, 7, "", &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1,
		Changes: []dataprotection.ProjectionChange{{ChangeID: "diagnostic-change", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: "diagnostic-cursor"}, now); err != nil {
		t.Fatal(err)
	}
	gate := protection.NewGate(store)
	service := &QueryExecutionService{queries: repository.NewExecutionRepository(local)}
	for _, scenario := range []struct {
		name, query, code, stage, reason, object string
		line, column                             int
	}{
		{"wrong table", "SELECT * FROM " + prefix + "farmla1nd", "table_not_found", "preparation", "table_not_found", "farmla1nd", 1, 15},
		{"syntax", "SELECT id, FROM " + prefix + "a", "query_syntax_invalid", "preparation", "query_syntax_invalid", "", 1, 12},
		{"wrong field", "SELECT missing_field FROM " + prefix + "a", "sqlstate_42703", "execution", "column_not_found", "missing_field", 1, 8},
		{"ambiguous field", "SELECT id FROM " + prefix + "a x JOIN " + prefix + "a y ON x.id=y.id", "sqlstate_42702", "execution", "ambiguous_column", "id", 1, 8},
		{"wrong function", "SELECT missing_function(id) FROM " + prefix + "a", "function_not_found", "preparation", "function_or_operator_not_found", "missing_function", 1, 8},
		{"wrong operator", "SELECT id + true FROM " + prefix + "a", "sqlstate_42883", "execution", "function_or_operator_not_found", "+", 1, 11},
		{"wrong type", "SELECT CAST('not-number' AS integer) FROM " + prefix + "a", "sqlstate_22P02", "execution", "type_mismatch", "", 0, 0},
		{"division by zero", "SELECT 1/(id-id) FROM " + prefix + "a", "sqlstate_22012", "execution", "division_by_zero", "", 0, 0},
		{"unicode multiline field", " \n-- 中文😀\n  SELECT missing_field FROM " + prefix + "a;", "sqlstate_42703", "execution", "column_not_found", "missing_field", 3, 10},
		{"parameter shifted field", "SELECT :value, missing_field FROM " + prefix + "a", "sqlstate_42703", "execution", "column_not_found", "missing_field", -1, 0},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			options := plugin.QueryOptions{ReadOnly: true, Limit: 11}
			if scenario.name == "parameter shifted field" {
				options.Parameters = map[string]interface{}{"value": 1}
			}
			prepared, queryErr := provider.PrepareQuery(ctx, conn, plugin.QueryRequest{EngineID: 91, Language: "sql", Query: scenario.query, Options: options})
			if queryErr == nil {
				_, end, gateErr := gate.BeginPreparedQuery(ctx, 7, provider, prepared)
				queryErr = gateErr
				if queryErr == nil {
					defer end()
					_, queryErr = prepared.Execute(ctx)
				} else if gate.HasActiveExecutionsForTenant(7) {
					t.Fatal("failed preparation left an active read")
				}
			}
			if queryErr == nil {
				t.Fatal("incorrect SQL succeeded")
			}
			code := queryErrorCode(queryErr)
			if code != scenario.code {
				t.Fatalf("got code %q from actual failure %v, want %q", code, queryErr, scenario.code)
			}
			token, owner, expires := uuid.NewString(), "diagnostic-worker", now.Add(time.Minute)
			item := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: uuid.NewString(), Module: "develop", TaskType: "query", Source: "develop", Status: "running", TriggerType: "manual", Attempt: 1, LeaseToken: &token, LeaseOwner: &owner, LeaseExpiresAt: &expires, CreatedAt: now, UpdatedAt: now}
			item.ExecutionConfig = commonModels.JSONMap{"inputs": commonModels.JSONMap{"query_source_digest": commonquery.SourceDigest(scenario.query)}}
			if err := local.Create(item).Error; err != nil {
				t.Fatal(err)
			}
			lease := commonExecution.Lease{ExecutionID: item.ExecutionID, TenantID: 7, Token: token, Owner: owner, Attempt: 1}
			if err := service.completeQueryError(ctx, item, lease, now, nil, nil, queryErr); err != nil {
				t.Fatal(err)
			}
			var stored commonExecution.TaskExecution
			if err := local.Where("execution_id = ?", item.ExecutionID).First(&stored).Error; err != nil {
				t.Fatal(err)
			}
			observation := commonExecution.Observe(&stored)
			diagnostic, ok := observation.ErrorDetails["diagnostic"].(*commonExecution.FailureDiagnostic)
			if !ok || stored.Status != "failed" || diagnostic.Stage != scenario.stage || diagnostic.Reason != scenario.reason {
				t.Fatalf("diagnostic=%+v, expected %s/%s", diagnostic, scenario.stage, scenario.reason)
			}
			if scenario.object != "" && (diagnostic.SQL == nil || diagnostic.SQL.Object == nil || !strings.Contains(diagnostic.SQL.Object.Name, scenario.object)) {
				t.Fatalf("missing confirmed object: %+v", diagnostic.SQL)
			}
			if scenario.line > 0 && (diagnostic.SQL == nil || diagnostic.SQL.Position == nil || diagnostic.SQL.Position.Line != scenario.line || diagnostic.SQL.Position.Column != scenario.column) {
				t.Fatalf("position=%+v, want %d:%d", diagnostic.SQL, scenario.line, scenario.column)
			}
			if scenario.line == -1 && diagnostic.SQL.Position != nil {
				t.Fatalf("parameter rewrite leaked unproven position: %+v", diagnostic.SQL)
			}
			encoded, _ := json.Marshal(observation.ErrorDetails)
			for _, private := range []string{"not-number", "source_digest", "SourceDigest", "Offset", "message", "pq:", "SELECT "} {
				if strings.Contains(string(encoded), private) {
					t.Fatalf("raw runtime text in observation: %s", encoded)
				}
			}
		})
	}
}
