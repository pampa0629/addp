package engineaccess

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	commonapi "github.com/addp/common/api"
	shared "github.com/addp/common/authorization"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/execution"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/system/internal/iam"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

type frozenRefreshFacts map[string]*execution.MetaScanPlanFacts

func (f frozenRefreshFacts) ReadScanPlanFacts(_ context.Context, _ uint, id string) (*execution.MetaScanPlanFacts, error) {
	return f[id].Clone(), nil
}

// Called by the formal PostgreSQL Grant fixture, after an ordinary User scope
// was queried through a real current browser credential. No source is opened.
func exerciseFrozenRefreshRuntime(t *testing.T, db *gorm.DB, svc *Service, scope *shared.MetadataRefreshScope) func() {
	t.Helper()
	ctx := t.Context()
	tenant := scope.TenantID
	var machine iam.Principal
	var member iam.TenantMembership
	if err := db.Raw("SELECT p.* FROM system.principals p JOIN system.service_principals s ON s.id=p.id WHERE s.name='addp-meta'").Scan(&machine).Error; err != nil || machine.ID == 0 {
		t.Fatal("missing Meta Runtime", err)
	}
	if err := db.Where("tenant_id=? AND principal_id=?", tenant, machine.ID).Take(&member).Error; err != nil {
		t.Fatal(err)
	}
	if err := db.Table("system.oauth_clients").Where("client_id='addp-meta'").Updates(map[string]any{"status": "active", "client_secret_hash": "disposable_refresh_fixture"}).Error; err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Table("system.oauth_clients").Where("client_id='addp-meta'").Update("status", "disabled") })
	if err := db.First(&machine, machine.ID).Error; err != nil {
		t.Fatal(err)
	}
	runtime := Actor{TenantID: tenant, PrincipalID: machine.ID, MembershipID: member.ID, AuthorizationVersion: machine.AuthorizationVersion, TokenExpiresAt: time.Now().Add(time.Hour)}
	facts := frozenRefreshFacts{}
	svc.WithMetaScanFactsReader(facts)
	create := func() *execution.MetaCollectionAccessRequest {
		t.Helper()
		token := uuid.NewString()
		owner := "refresh-fixture"
		until := testDatabaseClock(t, db)().Add(5 * time.Minute)
		run := execution.TaskExecution{TenantID: int(tenant), ExecutionID: uuid.NewString(), Module: "meta", TaskType: "scan", ExecutionBoundary: "bounded", Source: "manager", Status: "running", TriggerType: "manual", Attempt: 1, LeaseToken: &token, LeaseExpiresAt: &until, LeaseOwner: &owner,
			ActorPrincipalID: &scope.Operator.PrincipalID, ActorTenantMembershipID: &scope.Operator.MembershipID, IssuedAuthorizationVersion: &scope.Operator.AuthorizationVersion,
			ExecutionConfig: commonmodels.JSONMap{"engine_id": scope.Query.Target.EngineID, "engine_type": "postgresql", "engine_version": scope.EngineVersion, "scan_depth": "deep", "refresh_scope": scope}}
		if err := db.Create(&run).Error; err != nil {
			t.Fatal(err)
		}
		var raw string
		if err := db.Table(run.TableName()).Select("execution_config").Where("tenant_id=? AND execution_id=?", tenant, run.ExecutionID).Scan(&raw).Error; err != nil {
			t.Fatal(err)
		}
		digest, err := execution.ComputeMetaScanExecutionPlanDigest(json.RawMessage(raw))
		if err != nil {
			t.Fatal(err)
		}
		f := &execution.MetaScanPlanFacts{SchemaVersion: execution.MetaScanPlanFactsSchemaV1, ExecutionID: run.ExecutionID, EngineID: scope.Query.Target.EngineID, EngineVersion: scope.EngineVersion, ExecutionPlanDigest: digest, ScanDepth: "deep", BasisState: authorizationfacts.AuthorizationBasisComplete, RefreshScope: scope}
		if err := f.Validate(); err != nil {
			t.Fatal(err)
		}
		facts[run.ExecutionID] = f
		return &execution.MetaCollectionAccessRequest{ExecutionID: run.ExecutionID, Attempt: 1, LeaseToken: token, ExecutionPlanDigest: digest}
	}
	request := create()
	check := func(allowed bool) {
		t.Helper()
		result, err := svc.CheckMetaCollection(ctx, runtime, *request)
		if allowed {
			if err != nil || result == nil || result.ExecutionID != request.ExecutionID || result.ObservedAt.IsZero() {
				t.Fatalf("refresh result=%+v err=%v", result, err)
			}
		} else if result != nil || !errors.Is(err, commonapi.ErrForbidden) {
			t.Fatalf("invalid refresh accepted=%+v err=%v", result, err)
		}
	}
	check(true)
	original := *request
	for _, change := range []func(){func() { request.Attempt++ }, func() { request.LeaseToken = uuid.NewString() }, func() { request.ExecutionPlanDigest = strings.Repeat("b", 64) }} {
		change()
		check(false)
		*request = original
	}
	for _, change := range []map[string]any{
		{"source": "orchestrator"}, {"trigger_type": "scheduled"}, {"status": "cancelled"}, {"actor_principal_id": scope.Operator.PrincipalID + 9999},
		{"lease_expires_at": testDatabaseClock(t, db)().Add(-time.Second)},
		{"metadata": commonmodels.JSONMap{"scan_cancellation": commonmodels.JSONMap{"reason": "user_requested"}}},
	} {
		request = create()
		// A different FK-valid principal cannot impersonate the frozen actor.
		if _, ok := change["actor_principal_id"]; ok {
			change["actor_principal_id"] = machine.ID
		}
		if err := db.Table((execution.TaskExecution{}).TableName()).Where("tenant_id=? AND execution_id=?", tenant, request.ExecutionID).Updates(change).Error; err != nil {
			t.Fatal(err)
		}
		check(false)
	}
	request = create()
	check(true)
	return func() { t.Helper(); check(false) }
}
