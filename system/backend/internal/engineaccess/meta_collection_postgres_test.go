package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/addp/system/internal/models"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type collectionFactsFixture struct {
	db       *gorm.DB
	engineID uint
	digest   string
	failure  error
	reads    atomic.Int64
}

func (f *collectionFactsFixture) ReadScanPlanFacts(ctx context.Context, tenant uint, id string) (*execution.MetaScanPlanFacts, error) {
	f.reads.Add(1)
	if f.failure != nil {
		return nil, f.failure
	}
	var row struct {
		SourceTaskID    *string
		ExecutionConfig string
	}
	if err := f.db.WithContext(ctx).Table((execution.TaskExecution{}).TableName()).Select("source_task_id,execution_config").Where("tenant_id=? AND execution_id=?", tenant, id).Take(&row).Error; err != nil {
		return nil, err
	}
	digest, err := execution.ComputeMetaScanExecutionPlanDigest(json.RawMessage(row.ExecutionConfig))
	if err != nil {
		return nil, err
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(row.ExecutionConfig), &config); err != nil {
		return nil, err
	}
	frozen := config["task_definition_digest"].(string)
	facts := &execution.MetaScanPlanFacts{SchemaVersion: execution.MetaScanPlanFactsSchemaV1, ExecutionID: id, TaskID: row.SourceTaskID, TaskDefinitionDigest: &frozen, ExecutionPlanDigest: digest, EngineID: f.engineID, EngineVersion: config["engine_version"].(string), ScanDepth: config["scan_depth"].(string), BasisState: authorizationfacts.AuthorizationBasisNeedsRefresh}
	if frozen == f.digest {
		facts.BasisState = authorizationfacts.AuthorizationBasisComplete
		facts.SourceScope = &execution.MetaScanSourceScope{Mode: "full", Anchors: []plugin.EngineCatalogPath{plugin.EngineCatalogRootPath(plugin.TabularCatalogModel("schema"), f.engineID)}}
	}
	return facts, nil
}

func exerciseMetaCollectionRuntime(t *testing.T, db *gorm.DB, tenantID, adminID int64, engine models.Engine, newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("meta collection runtime", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		now := testDatabaseClock(t, db)()
		reader := &collectionFactsFixture{db: db, engineID: engine.ID, digest: strings.Repeat("a", 64)}
		svc := NewService(NewRepository(db), nil).WithMetaScanFactsReader(reader)
		var grantCount, denyCount int64
		if err := db.Table("system.engine_access_grants").Count(&grantCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.engine_access_denies").Count(&denyCount).Error; err != nil {
			t.Fatal(err)
		}
		var machine iam.Principal
		var member iam.TenantMembership
		if err := db.Raw("SELECT p.* FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-meta'").Scan(&machine).Error; err != nil || machine.ID == 0 {
			t.Fatal("missing Meta runtime", err)
		}
		if err := db.Where("tenant_id=? AND principal_id=?", tenantID, machine.ID).Take(&member).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.oauth_clients").Where("client_id='addp-meta'").Updates(map[string]any{"status": "active", "client_secret_hash": "disposable_meta_fixture"}).Error; err != nil {
			t.Fatal(err)
		}
		defer db.Table("system.oauth_clients").Where("client_id='addp-meta'").Update("status", "disabled")
		if err := db.First(&machine, machine.ID).Error; err != nil {
			t.Fatal(err)
		}
		runtime := Actor{TenantID: tenantID, PrincipalID: machine.ID, MembershipID: member.ID, AuthorizationVersion: machine.AuthorizationVersion, TokenExpiresAt: now.Add(time.Hour)}
		newRun := func(taskID string) execution.MetaCollectionAccessRequest {
			t.Helper()
			version := strconv.FormatInt(engine.Version, 10)
			token := uuid.NewString()
			until := now.Add(20 * time.Minute)
			owner := "meta-collection-fixture"
			run := execution.TaskExecution{TenantID: int(tenantID), ExecutionID: uuid.NewString(), Module: "meta", TaskType: "scan", ExecutionBoundary: "bounded", Source: "meta", SourceTaskID: &taskID, Status: "running", TriggerType: "scheduled", Attempt: 1, LeaseToken: &token, LeaseExpiresAt: &until, LeaseOwner: &owner, ExecutionConfig: commonmodels.JSONMap{"engine_id": engine.ID, "engine_type": "postgresql", "engine_version": version, "scan_depth": "basic", "force": false, "task_definition_digest": reader.digest}}
			if err := db.Create(&run).Error; err != nil {
				t.Fatal(err)
			}
			var raw string
			if err := db.Table(run.TableName()).Select("execution_config").Where("execution_id=?", run.ExecutionID).Scan(&raw).Error; err != nil {
				t.Fatal(err)
			}
			digest, err := execution.ComputeMetaScanExecutionPlanDigest(json.RawMessage(raw))
			if err != nil {
				t.Fatal(err)
			}
			return execution.MetaCollectionAccessRequest{ExecutionID: run.ExecutionID, Attempt: 1, LeaseToken: token, ExecutionPlanDigest: digest}
		}
		request := newRun("41")
		for _, r := range []execution.MetaCollectionAccessRequest{request, newRun("41"), newRun("42")} {
			observed, err := svc.CheckMetaCollection(ctx, runtime, r)
			if err != nil || observed.ExecutionID != r.ExecutionID || observed.ExecutionPlanDigest != r.ExecutionPlanDigest || observed.ObservedAt.IsZero() {
				t.Fatalf("tenant task without approval: %+v %v", observed, err)
			}
		}
		for _, mutate := range []func(*execution.MetaCollectionAccessRequest){func(r *execution.MetaCollectionAccessRequest) { r.Attempt++ }, func(r *execution.MetaCollectionAccessRequest) { r.LeaseToken = uuid.NewString() }, func(r *execution.MetaCollectionAccessRequest) { r.ExecutionPlanDigest = strings.Repeat("b", 64) }} {
			wrong := request
			mutate(&wrong)
			if _, err := svc.CheckMetaCollection(ctx, runtime, wrong); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("wrong execution binding allowed", err)
			}
		}
		for _, change := range []map[string]any{
			{"source": "orchestrator"}, {"trigger_type": "event"}, {"status": "cancelled"},
			{"lease_expires_at": now.Add(-time.Second)},
			{"metadata": commonmodels.JSONMap{"scan_cancellation": commonmodels.JSONMap{"reason": "user_requested"}}},
		} {
			r := newRun("41")
			if err := db.Table((execution.TaskExecution{}).TableName()).Where("execution_id=?", r.ExecutionID).Updates(change).Error; err != nil {
				t.Fatal(err)
			}
			if _, err := svc.CheckMetaCollection(ctx, runtime, r); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("invalid active run allowed", change, err)
			}
		}
		reader.digest = strings.Repeat("b", 64)
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("changed plan allowed", err)
		}
		reader.digest = strings.Repeat("a", 64)
		reader.failure = errors.New("upstream unavailable")
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, ErrMetaCollectionFactsUnavailable) {
			t.Fatal("unavailable facts allowed", err)
		}
		for _, code := range []int{404, 409} {
			reader.failure = &client.TenantAPIError{StatusCode: code}
			if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("missing facts allowed", err)
			}
		}
		reader.failure = nil
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", engine.Version+1).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("changed engine allowed", err)
		}
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", engine.Version).Error; err != nil {
			t.Fatal(err)
		}
		stale := runtime
		stale.AuthorizationVersion++
		reads := reader.reads.Load()
		if _, err := svc.CheckMetaCollection(ctx, stale, request); err == nil || reads != reader.reads.Load() {
			t.Fatal("stale runtime reached Meta", err)
		}
		var current int64
		if err := db.Table("system.engine_access_grants").Count(&current).Error; err != nil || current != grantCount {
			t.Fatal("collection changed data Grants", err)
		}
		if err := db.Table("system.engine_access_denies").Count(&current).Error; err != nil || current != denyCount {
			t.Fatal("collection changed data Deny", err)
		}
	})
}
