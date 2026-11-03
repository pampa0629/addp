package service

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	"github.com/addp/common/authorization/authtest"
	"github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/service/internal/models"
	"github.com/addp/service/internal/protection"
	"github.com/addp/service/internal/repository"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestSQLRoleSelectionAgainstPostgres(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("SERVICE_POSTGRES_TEST_DSN"))
	if dsn == "" {
		t.Skip("SERVICE_POSTGRES_TEST_DSN is not set")
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	schema := fmt.Sprintf("service_sql_roles_%d", time.Now().UnixNano())
	if _, err = db.Exec(`CREATE SCHEMA "` + schema + `"`); err != nil {
		t.Fatal(err)
	}
	defer func() {
		if _, err := db.Exec(`DROP SCHEMA "` + schema + `" CASCADE`); err != nil {
			t.Error(err)
		}
	}()
	qualified := `"` + schema + `"."persons"`
	if _, err = db.Exec(`CREATE TABLE ` + qualified + ` (id bigint PRIMARY KEY,phone text); INSERT INTO ` + qualified + ` VALUES(1,'13661384499'),(2,NULL)`); err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatal(err)
	}
	port, _ := strconv.Atoi(u.Port())
	password, _ := u.User.Password()
	conn := plugin.ConnectionInfo{"host": u.Hostname(), "port": port, "database": strings.TrimPrefix(u.Path, "/"), "user": u.User.Username(), "password": password, "sslmode": "disable"}
	engineID, tenant := uint(91), uint(7)
	engine, err := plugin.Get("postgresql")
	if err != nil {
		t.Fatal(err)
	}
	baseSQL := `SELECT id,phone AS phone_alias,phone AS phone_copy FROM ` + qualified + ` WHERE id >= :minimum`
	probe, err := engine.(plugin.QueryRuntimeProvider).PrepareQuery(t.Context(), conn, plugin.QueryRequest{EngineID: engineID, Language: "sql", Query: `SELECT id,phone FROM ` + qualified, Options: plugin.QueryOptions{EngineID: engineID, EngineType: "postgresql", ReadOnly: true}})
	if err != nil {
		t.Fatal(err)
	}
	lineage, err := probe.OutputLineage(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	fields := lineage.Sources[0].Fields
	pdb, err := gorm.Open(sqlite.Open("file:"+t.Name()+"-projection?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = pdb.Exec("ATTACH DATABASE ':memory:' AS service").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(pdb, "service", "service", nil)
	if err != nil {
		t.Fatal(err)
	}
	component := dataprotection.Component{Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: "string"}
	component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	snapshotHash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	option := dataprotection.RoleMaskingOption{ID: "c5024432-a0a1-498c-b320-5cb4547515a3", Name: "Full mask", RoleKeys: []string{"custom.analytics"}, Decision: dataprotection.Decision{Effect: "mask", Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 0, "suffix_runes": 0, "mask_rune": "*"}, InvalidValueEffect: "suppress"}}
	policyID := "c4d8478d-5a76-49de-a073-0c107c34b0fd"
	projection := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: "sql-role-projection", Revision: "00000000000000000001", ConsumerOwner: "service", State: dataprotection.ProjectionStateActive, TargetEngineID: engineID, Target: dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: commonmodels.GenerateItemFingerprint(engineID, schema+".persons")}, SourceSnapshotHash: snapshotHash, ValidFrom: now.Add(-time.Minute), ExpiresAt: dataprotection.ProjectionDeadline(now.Add(time.Hour)), Rules: []dataprotection.Rule{{Action: "service_execute", Component: component, Decision: dataprotection.Decision{Effect: "mask", Algorithm: dataprotection.AlgorithmKeepPrefixSuffixV2, Parameters: map[string]any{"prefix_runes": 3, "suffix_runes": 4, "mask_rune": "*"}, InvalidValueEffect: "suppress"}, RoleOptions: &dataprotection.RoleMaskingOptions{PolicyID: policyID, PolicyRevision: 1, AssessmentRevision: 1, Options: []dataprotection.RoleMaskingOption{option}}}}}
	apply := func(previous, next string) {
		t.Helper()
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := store.ApplyBatch(t.Context(), 7, previous, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: next, Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}}, NextCursor: next}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
	}
	apply("", "sql-role-1")
	facts := authtest.NewTenantUserAuthContext("7", "9", []string{"service.definition.read", "service.definition.update", "service.data_read.execute"})
	var ineligible, expired atomic.Bool
	var observations atomic.Int64
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/system/engines/91":
			json.NewEncoder(w).Encode(commonmodels.Engine{ID: engineID, TenantID: &tenant, EngineType: "postgresql", ConnectionInfo: commonmodels.ConnectionInfo(conn)})
		case "/api/v1/system/auth/subject-roles":
			observations.Add(1)
			keys := []string{"custom.analytics"}
			if ineligible.Load() {
				keys = []string{}
			}
			until := time.Now().Add(time.Minute)
			if expired.Load() {
				until = time.Now().Add(-time.Second)
			}
			json.NewEncoder(w).Encode(authorization.CurrentSubjectRoles{TenantID: "7", ActorPrincipalID: "9", TenantMembershipID: *facts.Context.TenantMembershipID, AuthorizationVersion: facts.Authorization.AuthorizationVersion, RoleKeys: keys, ObservedAt: time.Now().UTC(), ExpiresAt: until})
		default:
			t.Errorf("unexpected System path: %s", r.URL.Path)
			w.WriteHeader(404)
		}
	}))
	defer server.Close()
	tokens := client.ServiceTokenProviderFunc(func(context.Context, uint) (string, error) { return "addp_at_fixture", nil })
	executor := NewQueryExecutorService(client.NewSystemClient(server.URL, tokens), client.NewSystemServiceClient(server.URL, nil, server.Client()), []byte("0123456789abcdef0123456789abcdef"))
	executor.SetProtectionGate(protection.NewGate(store))
	ctx := WithQueryProtectionUser(t.Context(), facts, "addp_at_current")
	q := roleTestService()
	q.ID = 0
	q.TenantID = tenant
	q.EngineID = &engineID
	q.ConfigType = "sql"
	q.SchemaName = ""
	q.TargetTable = ""
	q.SqlQuery = baseSQL
	q.Version = 1
	q.NamedParameters = []models.QueryServiceNamedParameter{{Name: "minimum", Type: datatype.FieldTypeBigInt, Required: true}}
	contract := &models.QueryServiceOutputContract{Table: &datatype.TableInfo{Fields: []datatype.FieldInfo{{Name: "id", Type: datatype.FieldTypeBigInt, Nullable: false}, {Name: "phone_alias", Type: datatype.FieldTypeString, Nullable: true}, {Name: "phone_copy", Type: datatype.FieldTypeString, Nullable: true}}, PrimaryKey: []string{"id"}}}
	q.DataConfig[models.QueryServiceSourceSnapshotKey] = queryServiceSnapshotPayload(buildSQLDependencySnapshot(baseSQL, contract, now))
	q.DataConfig["default_fields"] = []string{"id", "phone_alias", "phone_copy"}
	q.DataConfig["filterable_fields"] = []string{"id"}
	choices := []dataprotection.FieldRoleChoice{{Target: projection.Target, ComponentKey: "phone", Selection: dataprotection.RoleOptionSelection{PolicyID: policyID, PolicyRevision: 1, AssessmentRevision: 1, OptionID: option.ID}}}
	q.DataConfig[models.QueryProtectionChoicesKey] = choices
	candidates, err := executor.QueryProtectionOptions(ctx, tenant, &models.QueryProtectionOptionsRequest{ConfigType: "sql", EngineID: engineID, SQL: baseSQL, Parameters: map[string]any{"minimum": int64(1)}})
	if err != nil || len(candidates.Fields) != 1 || candidates.Fields[0].ComponentKey != "phone" || len(candidates.Fields[0].Options) != 1 {
		t.Fatalf("SQL candidates: %+v %v", candidates, err)
	}
	ownerDB := openQueryServiceCreateTestDB(t)
	if err := ownerDB.Create(q).Error; err != nil {
		t.Fatal(err)
	}
	owner := NewQueryServiceService(repository.NewQueryServiceRepository(ownerDB), nil, nil, "")
	owner.SetRoleProtection(executor)
	title := "SQL selected method"
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: 1, Title: &title}); err != nil {
		t.Fatalf("save with required parameter must only prepare metadata: %v", err)
	}
	q, err = owner.repo.GetByIDAndTenant(q.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	if q.SourceSnapshot().Source != nil {
		t.Fatal("SQL invented a Meta source snapshot")
	}
	if _, err := db.Exec(`ALTER TABLE ` + qualified + ` ADD COLUMN note text`); err != nil {
		t.Fatal(err)
	}
	changedTitle := "must not save stale structure"
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: q.Version, Title: &changedTitle}); !errors.Is(err, dataprotection.ErrSchemaMismatch) {
		t.Fatalf("save accepted changed source structure: %v", err)
	}
	unchanged, err := owner.repo.GetByIDAndTenant(q.ID, tenant)
	if err != nil || unchanged.Version != q.Version || unchanged.Title != q.Title {
		t.Fatal("rejected schema change mutated the definition", err)
	}
	if _, err := db.Exec(`ALTER TABLE ` + qualified + ` DROP COLUMN note`); err != nil {
		t.Fatal(err)
	}
	request := &models.QueryExecutionRequest{Parameters: map[string]any{"minimum": int64(1)}, Page: models.QueryPageRequest{Limit: 1}}
	first, err := executor.ExecuteQuery(ctx, q, request)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Data) != 1 || first.Data[0]["phone_alias"] != "***********" || first.Data[0]["phone_copy"] != "***********" || !first.Page.HasMore {
		t.Fatalf("SQL alias/copy output: %+v", first)
	}
	request.Page.Cursor = first.Page.NextCursor
	second, err := executor.ExecuteQuery(ctx, q, request)
	if err != nil || len(second.Data) != 1 || second.Data[0]["phone_alias"] != nil || second.Data[0]["phone_copy"] != nil {
		t.Fatalf("SQL NULL/cursor: %+v %v", second, err)
	}
	ineligible.Store(true)
	if _, err := executor.ExecuteQuery(ctx, q, request); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatalf("revoked role accepted: %v", err)
	}
	ineligible.Store(false)
	expired.Store(true)
	if _, err := executor.ExecuteQuery(ctx, q, request); !errors.Is(err, dataprotection.ErrRoleOptionIneligible) {
		t.Fatalf("expired role accepted: %v", err)
	}
	expired.Store(false)
	projection.Revision = "00000000000000000002"
	projection.Rules[0].RoleOptions.PolicyRevision = 2
	apply("sql-role-1", "sql-role-2")
	if _, err := executor.ExecuteQuery(ctx, q, request); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatalf("revised method accepted: %v", err)
	}
	inactive := "inactive"
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: q.Version, Status: &inactive}); err != nil {
		t.Fatalf("stop stale method: %v", err)
	}
	q, err = owner.repo.GetByIDAndTenant(q.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	activeWithStaleChoice := "active"
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: q.Version, Status: &activeWithStaleChoice}); !errors.Is(err, dataprotection.ErrRoleOptionUnavailable) {
		t.Fatalf("re-enable accepted a stale SQL method: %v", err)
	}
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: q.Version, DataConfig: map[string]any{models.QueryProtectionChoicesKey: []any{}}}); err != nil {
		t.Fatal(err)
	}
	q, err = owner.repo.GetByIDAndTenant(q.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	active := "active"
	if _, err := owner.UpdateService(ctx, q.ID, tenant, &models.UpdateQueryServiceRequest{Version: q.Version, Status: &active}); err != nil {
		t.Fatal(err)
	}
	q, err = owner.repo.GetByIDAndTenant(q.ID, tenant)
	if err != nil {
		t.Fatal(err)
	}
	request.Page.Cursor = ""
	before := observations.Load()
	restored, err := executor.ExecuteQuery(ctx, q, request)
	if err != nil || restored.Data[0]["phone_alias"] != "136****4499" || restored.Data[0]["phone_copy"] != "136****4499" || observations.Load() != before {
		t.Fatalf("default restore added role dependency: %+v %v", restored, err)
	}
}
