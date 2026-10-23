package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"sort"
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

func (f *collectionFactsFixture) ReadScanTaskFacts(_ context.Context, _ uint, id string) (*execution.MetaScanTaskFacts, error) {
	f.reads.Add(1)
	if f.failure != nil {
		return nil, f.failure
	}
	return &execution.MetaScanTaskFacts{SchemaVersion: execution.MetaScanTaskFactsSchemaV1, TaskID: id, TaskDefinitionDigest: f.digest, EngineID: f.engineID, ScopeKind: "engine", ScanDepth: "basic"}, nil
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

func exerciseMetaCollectionQualifications(t *testing.T, db *gorm.DB, tenantID, adminID int64, engine models.Engine, newUser func(*testing.T, time.Duration) (userProvenance, time.Time), seedDelegation func(*testing.T, int64, time.Time) *Delegation) {
	t.Run("meta collection qualification", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(t.Context(), 60*time.Second)
		defer cancel()
		identity := iam.NewRepository(db)
		roles := iam.NewTenantRoleService(identity, testDatabaseClock(t, db))
		role, err := roles.CreateRole(ctx, iam.CreateTenantRoleInput{TenantID: tenantID, RoleKey: "custom.collection_approval", Name: "Collection approval fixture", PermissionKeys: []string{"system.meta_collection_qualification.approve", "system.meta_collection_qualification.read", "system.meta_collection_qualification.revoke"}, ActorPrincipalID: adminID})
		if err != nil {
			t.Fatal(err)
		}
		user, expires := newUser(t, time.Hour)
		seedDelegation(t, user.MembershipID, expires.Add(-time.Second))
		if _, err := roles.CreateAssignments(ctx, iam.CreateTenantRoleAssignmentsInput{TenantID: tenantID, MembershipID: user.MembershipID, RoleIDs: []int64{role.ID}, ActorPrincipalID: adminID, Reason: "Collection fixture only"}); err != nil {
			t.Fatal(err)
		}
		p, err := identity.GetPrincipal(ctx, user.PrincipalID)
		if err != nil {
			t.Fatal(err)
		}
		now := testDatabaseClock(t, db)()
		actor := Actor{TenantID: tenantID, PrincipalID: p.ID, MembershipID: user.MembershipID, AuthorizationVersion: p.AuthorizationVersion, TokenExpiresAt: now.Add(time.Hour)}
		reader := &collectionFactsFixture{db: db, engineID: engine.ID, digest: strings.Repeat("a", 64)}
		svc := NewService(NewRepository(db), nil).WithMetaScanFactsReader(reader)
		var grantCount, denyCount int64
		if err := db.Table("system.engine_access_grants").Count(&grantCount).Error; err != nil {
			t.Fatal(err)
		}
		if err := db.Table("system.engine_access_denies").Count(&denyCount).Error; err != nil {
			t.Fatal(err)
		}
		input := ApproveMetaCollectionInput{Actor: actor, EngineID: int64(engine.ID), TaskID: "41", CommandID: uuid.New(), ExpectedTaskDefinitionDigest: reader.digest, Purpose: "Identity and boundary recognition only", ExpiresAt: now.Add(15 * time.Minute).Truncate(time.Microsecond)}
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
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("unapproved task allowed", err)
		}
		approved, err := svc.ApproveMetaCollection(ctx, input)
		if err != nil || approved.ID != input.CommandID || approved.Version != 1 || approved.ScanDepth != "basic" {
			t.Fatalf("approve=%+v err=%v", approved, err)
		}
		readback, _, err := svc.ListMetaCollections(ctx, actor, input.EngineID, 1, 100)
		if err != nil {
			t.Fatal(err)
		}
		var stored *MetaCollectionQualification
		for i := range readback {
			if readback[i].ID == approved.ID {
				stored = &readback[i]
			}
		}
		if stored == nil || !stored.GrantedAt.Equal(approved.GrantedAt) || !stored.ExpiresAt.Equal(approved.ExpiresAt) {
			t.Fatal("approval and persisted history disagree on exact timestamps")
		}
		wire := func(row *MetaCollectionQualification) map[string]any {
			t.Helper()
			raw, err := json.Marshal(row)
			var result map[string]any
			if err != nil || json.Unmarshal(raw, &result) != nil {
				t.Fatal("qualification JSON encoding failed")
			}
			return result
		}
		issuedWire, storedWire := wire(approved), wire(stored)
		if len(issuedWire) != len(storedWire) {
			t.Fatal("approval/history JSON field sets differ")
		}
		different := []string{}
		for key, value := range issuedWire {
			if !reflect.DeepEqual(value, storedWire[key]) {
				if key != "granted_at" && key != "expires_at" {
					t.Fatalf("approval/history changed non-temporal field %s", key)
				}
				different = append(different, key)
			}
		}
		sort.Strings(different)
		t.Logf("approval/history JSON differing fields=%v; granted_at and expires_at instants identical", different)
		for _, run := range []execution.MetaCollectionAccessRequest{request, newRun("41")} {
			observation, err := svc.CheckMetaCollection(ctx, runtime, run)
			if err != nil || observation.QualificationID != approved.ID.String() {
				t.Fatalf("repeat observation=%+v err=%v", observation, err)
			}
		}
		if _, err := svc.CheckMetaCollection(ctx, runtime, newRun("42")); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("another task inherited approval", err)
		}
		for _, mutate := range []func(*execution.MetaCollectionAccessRequest){func(r *execution.MetaCollectionAccessRequest) { r.Attempt++ }, func(r *execution.MetaCollectionAccessRequest) { r.LeaseToken = uuid.NewString() }, func(r *execution.MetaCollectionAccessRequest) { r.ExecutionPlanDigest = strings.Repeat("b", 64) }} {
			wrong := request
			mutate(&wrong)
			if _, err := svc.CheckMetaCollection(ctx, runtime, wrong); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("wrong execution binding allowed", err)
			}
		}
		reader.digest = strings.Repeat("b", 64)
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("changed task allowed", err)
		}
		reader.digest = input.ExpectedTaskDefinitionDigest
		changed := input
		changed.CommandID = uuid.New()
		changed.ExpectedTaskDefinitionDigest = strings.Repeat("b", 64)
		if _, err := svc.ApproveMetaCollection(ctx, changed); !errors.Is(err, ErrMetaCollectionChanged) {
			t.Fatal("stale confirmation accepted", err)
		}
		reader.failure = errors.New("upstream unavailable")
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, ErrMetaCollectionFactsUnavailable) {
			t.Fatal("unavailable facts allowed", err)
		}
		reader.failure = nil
		for _, code := range []int{404, 409} {
			reader.failure = &client.TenantAPIError{StatusCode: code}
			if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
				t.Fatal("missing/changed Meta plan treated as allow", err)
			}
		}
		reader.failure = nil
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", engine.Version+1).Error; err != nil {
			t.Fatal(err)
		}
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("changed Engine inherited qualification", err)
		}
		if err := db.Table("system.engines").Where("id=?", engine.ID).Update("version", engine.Version).Error; err != nil {
			t.Fatal(err)
		}
		wrongActor := actor
		wrongActor.AuthorizationVersion++
		beforeReads := reader.reads.Load()
		wrongApproval := input
		wrongApproval.Actor = wrongActor
		if _, err := svc.ApproveMetaCollection(ctx, wrongApproval); err == nil || reader.reads.Load() != beforeReads {
			t.Fatal("stale human reached Meta", err)
		}
		revoked, err := svc.RevokeMetaCollection(ctx, RevokeMetaCollectionInput{Actor: actor, EngineID: input.EngineID, ID: approved.ID, Version: 1, Reason: "Finish fixture"})
		if err != nil || revoked.Status != "revoked" || revoked.Version != 2 {
			t.Fatalf("revoke=%+v err=%v", revoked, err)
		}
		if _, err := svc.CheckMetaCollection(ctx, runtime, request); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("revoked task allowed", err)
		}
		reader.failure = errors.New("history must not need Meta")
		replayed, err := svc.ApproveMetaCollection(ctx, input)
		if err != nil || replayed.Status != "revoked" || replayed.Version != 2 || !replayed.ExpiresAt.Equal(input.ExpiresAt) {
			t.Fatalf("replay=%+v err=%v", replayed, err)
		}
		reader.failure = nil
		// Two different approval commands for one task cannot create overlapping
		// qualifications, even when both first observe an empty history.
		concurrent := input
		concurrent.TaskID = "43"
		concurrent.CommandID = uuid.New()
		competitor := concurrent
		competitor.CommandID = uuid.New()
		start := make(chan struct{})
		type outcome struct {
			row *MetaCollectionQualification
			err error
		}
		results := make(chan outcome, 2)
		for _, candidate := range []ApproveMetaCollectionInput{concurrent, competitor} {
			go func() { <-start; row, err := svc.ApproveMetaCollection(ctx, candidate); results <- outcome{row, err} }()
		}
		close(start)
		var winner *MetaCollectionQualification
		for range 2 {
			result := <-results
			if result.err == nil {
				if winner != nil {
					t.Fatal("concurrent overlapping qualifications")
				}
				winner = result.row
			} else if !errors.Is(result.err, ErrMetaCollectionChanged) {
				t.Fatal("unexpected arbitration error", result.err)
			}
		}
		if winner == nil {
			t.Fatal("no approval won arbitration")
		}
		if _, err := svc.RevokeMetaCollection(ctx, RevokeMetaCollectionInput{Actor: actor, EngineID: input.EngineID, ID: winner.ID, Version: 1, Reason: "Finish arbitration"}); err != nil {
			t.Fatal(err)
		}
		// Expiration is observed from database time, not a cached active flag.
		expiring := input
		expiring.TaskID = "44"
		expiring.CommandID = uuid.New()
		expiring.ExpiresAt = testDatabaseClock(t, db)().Add(time.Second).Truncate(time.Microsecond)
		short, err := svc.ApproveMetaCollection(ctx, expiring)
		if err != nil {
			t.Fatal(err)
		}
		shortRequest := newRun("44")
		if _, err := svc.CheckMetaCollection(ctx, runtime, shortRequest); err != nil {
			t.Fatal("current short qualification refused", err)
		}
		if until := time.Until(short.ExpiresAt.Add(10 * time.Millisecond)); until > 0 {
			time.Sleep(until)
		}
		if _, err := svc.CheckMetaCollection(ctx, runtime, shortRequest); !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatal("expired qualification allowed", err)
		}
		if _, err := svc.RevokeMetaCollection(ctx, RevokeMetaCollectionInput{Actor: actor, EngineID: input.EngineID, ID: short.ID, Version: 1, Reason: "Expired history is read-only"}); !errors.Is(err, ErrVersionConflict) {
			t.Fatal("expired history changed", err)
		}
		reader.failure = errors.New("expired replay must not renew")
		if restored, err := svc.ApproveMetaCollection(ctx, expiring); err != nil || !restored.ExpiresAt.Equal(short.ExpiresAt) {
			t.Fatal("expired command was reissued", err)
		}
		// History immutability is enforced by storage, not just this service.
		for _, statement := range []string{
			"UPDATE system.meta_collection_qualifications SET expires_at=expires_at+interval '1 day' WHERE id=?",
			"UPDATE system.meta_collection_qualifications SET revoked_reason=NULL WHERE id=?",
			"DELETE FROM system.meta_collection_qualifications WHERE id=?",
		} {
			if err := db.Exec(statement, approved.ID).Error; err == nil {
				t.Fatal("immutable qualification history was changed")
			}
		}
		rows, total, err := svc.ListMetaCollections(ctx, actor, input.EngineID, 1, 10)
		if err != nil || total != 3 || len(rows) != 3 {
			t.Fatalf("history=%+v total=%d err=%v", rows, total, err)
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
