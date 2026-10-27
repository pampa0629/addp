package protection

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/postgresql"
	execution "github.com/addp/common/execution"
	"github.com/addp/common/execution/executiontest"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/testpg"
	"github.com/google/uuid"
	postgresdriver "gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestIntegrationPostgresExportArtifactProtection(t *testing.T) {
	if os.Getenv("ADDP_POSTGRES_INTEGRATION") != "1" {
		t.Skip("run make test-transfer-postgres")
	}
	dsn, err := (&postgresql.PostgreSQLPlugin{}).BuildDSN(testpg.ConnInfoFromEnv(t))
	if err != nil {
		t.Fatal(err)
	}
	db, err := gorm.Open(postgresdriver.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	if err := execution.EnsureStore(db); err != nil {
		t.Fatal(err)
	}
	testExportProtectionLifecycle(t, db)
}

func TestExportArtifactProtectionLifecycle(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := executiontest.EnsureSQLiteStore(db); err != nil {
		t.Fatal(err)
	}
	pool, _ := db.DB()
	defer pool.Close()
	testExportProtectionLifecycle(t, db)
}

func testExportProtectionLifecycle(t *testing.T, db *gorm.DB) {
	tx := db.Begin()
	defer tx.Rollback()
	schema := "export_receipt_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	var schemaErr error
	if db.Dialector.Name() == "sqlite" {
		schemaErr = tx.Exec(`ATTACH DATABASE ':memory:' AS "` + schema + `"`).Error
	} else {
		schemaErr = tx.Exec(`CREATE SCHEMA "` + schema + `"`).Error
	}
	if schemaErr != nil {
		t.Fatal(schemaErr)
	}
	store, err := projectionstore.Migrate(tx, schema, "transfer", ExportArtifactBarrier{})
	if err != nil {
		t.Fatal(err)
	}
	tenant := int64(789126)
	cursor := ""
	apply := func(changes ...dataprotection.ProjectionChange) {
		t.Helper()
		next := uuid.NewString()
		if err := store.ApplyBatch(t.Context(), tenant, cursor, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: changes, NextCursor: next}, time.Now().UTC()); err != nil {
			t.Fatal(err)
		}
		cursor = next
	}
	apply()
	target := dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: strings.Repeat("a", 64)}
	other := target
	other.ResourceIdentity = strings.Repeat("b", 64)
	createdIDs := []string{}
	newExport := func(targets []dataprotection.ResourceReference) *execution.TaskExecution {
		t.Helper()
		owner, token := "export-worker", uuid.NewString()
		until := time.Now().UTC().Add(time.Hour)
		item := &execution.TaskExecution{TenantID: int(tenant), ExecutionID: uuid.NewString(), Module: execution.ModuleTransfer, TaskType: execution.TaskTypeSync, Source: "develop", Status: execution.ExecutionStatusRunning, ExecutionBoundary: execution.ExecutionBoundaryBounded, TriggerType: execution.TriggerTypeManual, Attempt: 1, LeaseOwner: &owner, LeaseToken: &token, LeaseExpiresAt: &until, Metadata: commonmodels.JSONMap{models.ExportProtectionMetadataKey: &models.ExportProtectionReceipt{}}}
		if err := tx.Create(item).Error; err != nil {
			t.Fatal(err)
		}
		createdIDs = append(createdIDs, item.ExecutionID)
		lease, err := execution.LeaseFromExecution(*item)
		if err != nil {
			t.Fatal(err)
		}
		ctx := WithExportExecution(execution.ContextWithLease(t.Context(), lease), item)
		if targets == nil {
			return item
		}
		if err := captureExportTargets(ctx, store, tenant, targets); err != nil {
			t.Fatal(err)
		}
		return item
	}
	gate := NewGate(store, nil)
	check := func(item *execution.TaskExecution, want bool) {
		t.Helper()
		got, err := gate.ExportArtifactCurrent(t.Context(), tenant, item.ExecutionID)
		if err != nil || got != want {
			t.Fatalf("delivery = %t, %v; want %t", got, err, want)
		}
	}
	projection := func(target dataprotection.ResourceReference, revision int) dataprotection.Projection {
		fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
		component := dataprotection.Component{Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString)}
		component.SchemaFingerprint, err = dataprotection.ComponentSchemaFingerprint(fields, component)
		if err != nil {
			t.Fatal(err)
		}
		snapshot, err := dataprotection.TableSchemaSnapshotHash(fields)
		if err != nil {
			t.Fatal(err)
		}
		p := dataprotection.Projection{SchemaVersion: dataprotection.ProjectionSchemaV5, ProjectionID: target.ResourceIdentity, Revision: fmt.Sprintf("%020d", revision), ConsumerOwner: "transfer", State: dataprotection.ProjectionStateActive, Target: target, TargetEngineID: 11, SourceSnapshotHash: snapshot, ValidFrom: time.Now().UTC().Add(-time.Minute), Rules: []dataprotection.Rule{{Action: "export", Component: component, Decision: dataprotection.Decision{Effect: dataprotection.EffectSuppress}}}}
		if err := p.Seal(); err != nil {
			t.Fatal(err)
		}
		return p
	}
	upsert := func(p dataprotection.Projection) {
		apply(dataprotection.ProjectionChange{ChangeID: uuid.NewString(), Operation: dataprotection.ChangeOperationUpsert, Projection: &p})
	}
	unmanaged := newExport([]dataprotection.ResourceReference{target})
	check(unmanaged, true)
	upsert(projection(other, 1))
	check(unmanaged, true)
	p := projection(target, 1)
	upsert(p)
	check(unmanaged, false)
	fresh := newExport([]dataprotection.ResourceReference{target})
	check(fresh, true)
	upsert(p)
	check(fresh, true) // idempotent delivery does not change protection
	// A failed installation must roll back both invalidation and checkpoint.
	before := cursor
	bad := projection(target, 2)
	rollback := errors.New("abort after barrier")
	if err := tx.Transaction(func(nested *gorm.DB) error {
		nestedStore, err := projectionstore.Open(nested, schema, "transfer", ExportArtifactBarrier{})
		if err != nil {
			return err
		}
		if err := nestedStore.ApplyBatch(t.Context(), tenant, before, &dataprotection.ProjectionChangesResponse{SchemaVersion: dataprotection.ProjectionChangesSchemaV1, Changes: []dataprotection.ProjectionChange{{ChangeID: uuid.NewString(), Operation: dataprotection.ChangeOperationUpsert, Projection: &bad}}, NextCursor: "rolled-back"}, time.Now().UTC()); err != nil {
			return err
		}
		return rollback
	}); !errors.Is(err, rollback) {
		t.Fatal(err)
	}
	if got, err := store.CurrentCursor(t.Context(), tenant); err != nil || got != before {
		t.Fatal("checkpoint escaped rollback", got, err)
	}
	check(fresh, true)
	upsert(bad)
	check(fresh, false)
	apply(dataprotection.ProjectionChange{ChangeID: uuid.NewString(), Operation: dataprotection.ChangeOperationRelease, Release: &dataprotection.ProjectionRelease{ProjectionID: p.ProjectionID, Revision: fmt.Sprintf("%020d", 3), Target: target}})
	check(unmanaged, false)
	check(fresh, false)
	newUnmanaged := newExport([]dataprotection.ResourceReference{target})
	check(newUnmanaged, true)
	p = projection(target, 4)
	p.ExpiresAt = dataprotection.ProjectionDeadline(time.Now().UTC().Add(time.Second))
	if err := p.Seal(); err != nil {
		t.Fatal(err)
	}
	upsert(p)
	check(newUnmanaged, false)
	expiring := newExport([]dataprotection.ResourceReference{target})
	check(expiring, true)
	time.Sleep(time.Until(*p.ExpiresAt) + 10*time.Millisecond)
	check(expiring, false)
	reloaded, err := projectionstore.Open(tx, schema, "transfer", ExportArtifactBarrier{})
	if err != nil {
		t.Fatal(err)
	}
	restarted := NewGate(reloaded, nil)
	if ok, err := restarted.ExportArtifactCurrent(context.Background(), tenant, fresh.ExecutionID); err != nil || ok {
		t.Fatal("restart resurrected artifact", err)
	}
	if ok, err := gate.ExportArtifactCurrent(t.Context(), tenant+1, fresh.ExecutionID); err == nil || ok {
		t.Fatal("cross-tenant delivery accepted")
	}

	// Exercise both real Transfer preparation hooks, not just receipt persistence.
	model := plugin.TabularCatalogModel(plugin.EngineCatalogTermSchema)
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermSchema, "business", plugin.EngineCatalogTermTable, plugin.EngineCatalogKindTable, "persons")
	nativeTarget, err := dataprotection.DataItemTargetFromCatalogPath(model, path)
	if err != nil {
		t.Fatal(err)
	}
	upsert(projection(nativeTarget, 1))
	sourceGate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "postgresql"}}.descriptor)
	placements := []*execution.TaskExecution{}
	for _, kind := range []string{"native", "query"} {
		pending := newExport(nil)
		placements = append(placements, pending)
		lease, err := execution.LeaseFromExecution(*pending)
		if err != nil {
			t.Fatal(err)
		}
		ctx := WithExportExecution(execution.ContextWithLease(t.Context(), lease), pending)
		bound, err := sourceGate.PrepareBoundedTableProtection(ctx, uint(tenant), map[string]interface{}{"source": map[string]interface{}{"locator": "addp://engine/11/path/business/persons?type=table"}}, nil)
		if err != nil {
			t.Fatal(err)
		}
		fields := []datatype.FieldInfo{{Name: "phone", Path: []string{"phone"}, Type: datatype.FieldTypeString, Nullable: true}}
		var protect *dataprotection.PreparedTableProtection
		if kind == "native" {
			protect, err = bound.PrepareCatalogTableProtection(ctx, path, fields)
		} else {
			prepared, e := plugin.NewPreparedQuery(&plugin.QueryAnalysis{Language: "sql", SchemaCoverage: plugin.QuerySchemaCoverageComplete}, nil, func(context.Context) (*plugin.QueryReadSet, error) { return plugin.NewQueryReadSet(path) }, func(context.Context, *plugin.QueryReadSet) (*plugin.QueryOutputLineage, error) {
				return &plugin.QueryOutputLineage{Sources: []plugin.QueryOutputSource{{Path: path, Fields: fields, IdentityOutput: true}}}, nil
			}, func(context.Context) (*plugin.QueryResult, error) { return nil, nil })
			if e != nil {
				t.Fatal(e)
			}
			protect, err = bound.PrepareQueryProtection(ctx, prepared)
		}
		if err != nil {
			t.Fatal(kind, err)
		}
		output := &plugin.QueryResult{Rows: []map[string]interface{}{{"phone": "synthetic-sensitive-value"}}}
		if err := protect.Apply(output); err != nil {
			t.Fatal(kind, err)
		}
		if _, exists := output.Rows[0]["phone"]; exists {
			t.Fatal("unprotected source output", kind)
		}
		check(pending, true)
	}
	upsert(projection(nativeTarget, 2))
	for _, item := range placements {
		check(item, false)
	}

	// A receipt cannot be installed without the current worker lease.
	unleased := newExport(nil)
	if err := captureExportTargets(WithExportExecution(t.Context(), unleased), store, tenant, []dataprotection.ResourceReference{nativeTarget}); !errors.Is(err, dataprotection.ErrNotReady) {
		t.Fatal("receipt installed without lease", err)
	}
	check(unleased, false)
	// Cross a bounded invalidation page: none of the later artifacts may survive.
	many := make([]*execution.TaskExecution, 101)
	for i := range many {
		many[i] = newExport([]dataprotection.ResourceReference{nativeTarget})
	}
	upsert(projection(nativeTarget, 3))
	for _, item := range many {
		check(item, false)
	}
	if err := tx.Rollback().Error; err != nil {
		t.Fatal(err)
	}
	var remaining int64
	if err := db.Model(&execution.TaskExecution{}).Where("execution_id IN ?", createdIDs).Count(&remaining).Error; err != nil || remaining != 0 {
		t.Fatal("fixture rollback not proven", remaining, err)
	}
	if db.Dialector.Name() == "postgres" {
		if err := db.Raw("SELECT COUNT(*) FROM pg_namespace WHERE nspname=?", schema).Scan(&remaining).Error; err != nil || remaining != 0 {
			t.Fatal("fixture schema rollback not proven", remaining, err)
		}
	}
}
