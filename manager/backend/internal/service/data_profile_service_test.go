package service

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestDataProfileServiceGetCurrentMarksStoredResultStale(t *testing.T) {
	profile := &dataprofile.Profile{SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}}
	profiles := &dataProfileServiceTestProfileStore{
		state:   profileResultStateForTest(t),
		profile: profile,
	}
	completedAt := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	executions := &dataProfileServiceTestExecutionStore{byID: &commonExecution.TaskExecution{
		ExecutionID: "execution-12", Status: commonExecution.ExecutionStatusSuccess, CompletedAt: &completedAt,
	}}
	profiles.state.LastExecutionID = "execution-12"
	profiles.state.SourceVersion = "old-version"
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator: "addp://engine/1/item/a", ItemFingerprint: "fingerprint", SourceVersion: "new-version",
	}}
	profileService := newAuthorizedProfileServiceForTest(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{})
	profileService.SetResultReadChecker(&profileResultCheckerForTest{})

	response, err := profileService.GetCurrent(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileCurrentRequest{Locator: sampler.target.Locator})
	if err != nil {
		t.Fatalf("GetCurrent() error = %v", err)
	}
	if response.Profile != profile || !response.Stale || response.StaleReason != "source_changed" {
		t.Fatalf("response = %#v", response)
	}
	if response.StoredSourceVersion != "old-version" || response.SourceVersion != "new-version" {
		t.Fatalf("source versions = stored:%q current:%q", response.StoredSourceVersion, response.SourceVersion)
	}
	if response.ProfileExecution == nil || response.ProfileExecution.ExecutionID != "execution-12" {
		t.Fatalf("profile execution = %#v", response.ProfileExecution)
	}
}

func TestDataProfileServiceRejectsUnsupportedMode(t *testing.T) {
	profileService := newAuthorizedProfileServiceForTest(
		&dataProfileServiceTestProfileStore{},
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{},
		&dataProfileServiceTestProtectionGate{},
	)
	_, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{
		Locator: "addp://engine/1/item/a",
		Mode:    "full",
	})
	if !errors.Is(err, ErrDataProfileInvalidRequest) {
		t.Fatalf("CreateExecution() error = %v, want ErrDataProfileInvalidRequest", err)
	}
}

func TestDataProfileServiceRejectsConditionalScopeWithoutProviderSupport(t *testing.T) {
	profileService := newAuthorizedProfileServiceForTest(
		&dataProfileServiceTestProfileStore{},
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{target: &DataProfileTarget{
			Locator:         "addp://engine/1/path/public/orders?type=table",
			ItemFingerprint: "sha256:orders",
			Fields:          []datatype.FieldInfo{{Name: "status", Type: datatype.FieldTypeString}},
		}},
		&dataProfileServiceTestProtectionGate{},
	)
	_, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{
		Locator: "addp://engine/1/path/public/orders?type=table",
		DataScope: dataprofile.DataScope{
			Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}},
		},
	})
	if !errors.Is(err, ErrDataProfileUnsupported) {
		t.Fatalf("CreateExecution() error = %v, want ErrDataProfileUnsupported", err)
	}
}

func TestDataProfileConfigHashSeparatesAllAndConditionalScopes(t *testing.T) {
	all := dataProfileConfigHash(DataProfileSelection{}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, DefaultDataProfileBudget)
	condition := dataProfileConfigHash(DataProfileSelection{}, dataprofile.DataScope{
		Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
		Conditions: []dataprofile.DataScopeCondition{{Field: "status", Operator: "eq", Value: "active"}},
	}, DefaultDataProfileBudget)
	if all == condition {
		t.Fatalf("all and conditional scopes share config hash %q", all)
	}
}

func TestDataProfileServiceFailedRefreshDoesNotReplaceSuccessfulResult(t *testing.T) {
	profiles := &dataProfileServiceTestProfileStore{}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{sampleErr: errors.New("source read failed")}
	profileService := newAuthorizedProfileServiceForTest(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{})
	target := &DataProfileTarget{ItemFingerprint: "fingerprint", SourceVersion: "version"}
	execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-1"}

	profileService.runProfileExecutionForTest(t, context.Background(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", execution, DefaultDataProfileBudget)

	if profiles.replaceCalls != 0 {
		t.Fatalf("ReplaceCurrent calls = %d, want 0", profiles.replaceCalls)
	}
	if executions.failedCode != "sample_failed" {
		t.Fatalf("failed code = %q, want sample_failed", executions.failedCode)
	}
}

func TestDataProfileServiceMissingSourceAuthorizationDoesNotPublishProfile(t *testing.T) {
	previous := &dataprofile.Profile{SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample}
	profiles := &dataProfileServiceTestProfileStore{profile: previous}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{sampleErr: ErrDataProfileSourceAuthorizationRequired}
	newAuthorizedProfileServiceForTest(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{}).runProfileExecutionForTest(t,
		context.Background(), &DataProfileTarget{ItemFingerprint: "item"}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-1"}, DefaultDataProfileBudget,
	)
	if profiles.replaceCalls != 0 || profiles.profile != previous || executions.completed || executions.failedCode != "source_authorization_required" {
		t.Fatalf("unauthorized sample published: writes=%d completed=%v code=%s", profiles.replaceCalls, executions.completed, executions.failedCode)
	}
}

func TestDataProfileServiceRejectsResultWhenProtectionChangesDuringSampling(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = sqlDB.Close() })
	if err := db.Exec("ATTACH DATABASE ':memory:' AS manager").Error; err != nil {
		t.Fatal(err)
	}
	store, err := projectionstore.Migrate(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	other, err := projectionstore.Open(db, "manager", "manager", nil)
	if err != nil {
		t.Fatal(err)
	}
	previous := &dataprofile.Profile{Mode: dataprofile.ModeSample}
	profiles := &dataProfileServiceTestProfileStore{profile: previous}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{sample: &DataProfileSample{ReadSet: profileReadSetForTest(t)}, onSample: func() {
		projection := dataprotection.Projection{
			SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "install", Revision: "00000000000000000001", ConsumerOwner: "manager", State: dataprotection.ProjectionStateEnrolling,
			Target:    dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: "item"},
			ValidFrom: time.Now().Add(-time.Minute), ExpiresAt: time.Now().Add(time.Hour),
		}
		if err := projection.Seal(); err != nil {
			t.Fatal(err)
		}
		if err := other.ApplyBatch(context.Background(), 7, "", &dataprotection.ProjectionChangesResponse{
			SchemaVersion: dataprotection.ProjectionChangesSchemaV1, NextCursor: "new", Changes: []dataprotection.ProjectionChange{{ChangeID: "install", Operation: dataprotection.ChangeOperationUpsert, Projection: &projection}},
		}, time.Now()); err != nil {
			t.Fatal(err)
		}
	}}
	newAuthorizedProfileServiceForTest(profiles, executions, sampler, store).runProfileExecutionForTest(t, context.Background(), &DataProfileTarget{EngineID: 1, ItemFingerprint: "item"}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "old"}, DefaultDataProfileBudget)
	if profiles.replaceCalls != 0 || profiles.profile != previous || executions.completed || executions.failedCode != "protection_version_changed" {
		t.Fatalf("late profile was accepted: writes=%d complete=%v code=%s", profiles.replaceCalls, executions.completed, executions.failedCode)
	}
}

func TestDataProfileServiceRevalidatesExpiredRulesAtCommit(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}
	gate := managedDataProfileServiceTestGate(t, "item", fields, dataprotection.EffectSuppress)
	profiles := &dataProfileServiceTestProfileStore{}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{sample: &DataProfileSample{Fields: fields, ReadSet: profileReadSetForTest(t)}, onSample: func() {
		// Same checkpoint; the rule itself expires while work is in flight.
		gate.result.Projections[0].ExpiresAt = time.Now().Add(-time.Second)
		if err := gate.result.Projections[0].Seal(); err != nil {
			t.Fatal(err)
		}
	}}
	newAuthorizedProfileServiceForTest(profiles, executions, sampler, gate).runProfileExecutionForTest(t, context.Background(), &DataProfileTarget{EngineID: 1, ItemFingerprint: "item", Fields: fields}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "expired"}, DefaultDataProfileBudget)
	if profiles.replaceCalls != 0 || executions.completed || executions.failedCode != "security_protection_required" {
		t.Fatalf("expired profile was committed: writes=%d code=%s", profiles.replaceCalls, executions.failedCode)
	}
}

func TestDataProfileServiceRejectsManagedCurrentResultBeforeStoreRead(t *testing.T) {
	profiles := &dataProfileServiceTestProfileStore{profile: &dataprofile.Profile{}}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator:         "addp://engine/1/path/Outdoor/Persons?type=collection",
		ItemFingerprint: "sha256:outdoor-persons",
	}}
	profileService := newAuthorizedProfileServiceForTest(
		profiles,
		&dataProfileServiceTestExecutionStore{},
		sampler,
		&dataProfileServiceTestProtectionGate{managed: true},
	)

	_, err := profileService.GetCurrent(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileCurrentRequest{Locator: sampler.target.Locator})
	if !errors.Is(err, ErrDataProfileProtectionRequired) {
		t.Fatalf("GetCurrent() error = %v, want ErrDataProfileProtectionRequired", err)
	}
	if profiles.getCalls != 0 {
		t.Fatalf("profile store reads = %d, want 0", profiles.getCalls)
	}
}

func TestDataProfileServiceRejectsManagedExecutionBeforeCreation(t *testing.T) {
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator:         "addp://engine/1/path/Outdoor/Persons?type=collection",
		ItemFingerprint: "sha256:outdoor-persons",
	}}
	profileService := newAuthorizedProfileServiceForTest(
		&dataProfileServiceTestProfileStore{},
		executions,
		sampler,
		&dataProfileServiceTestProtectionGate{managed: true},
	)

	_, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{Locator: sampler.target.Locator})
	if !errors.Is(err, ErrDataProfileProtectionRequired) {
		t.Fatalf("CreateExecution() error = %v, want ErrDataProfileProtectionRequired", err)
	}
	if executions.createCalls != 0 {
		t.Fatalf("execution creates = %d, want 0", executions.createCalls)
	}
}

func TestDataProfileServiceReturnsProtectedManagedCurrentResult(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "phone", Type: datatype.FieldTypeString},
	}
	storedProfile := &dataprofile.Profile{
		DataScope:  dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
		FieldCount: 2,
		Fields: []dataprofile.FieldProfile{
			{Name: "name", Type: datatype.FieldTypeString},
			{Name: "phone", Type: datatype.FieldTypeString, TopValues: []dataprofile.ValueCount{{Value: "13661384499", Count: 1}}},
		},
	}
	profiles := &dataProfileServiceTestProfileStore{profile: storedProfile, state: profileResultStateForTest(t)}
	target := &DataProfileTarget{
		Locator: "addp://engine/1/path/Outdoor/Persons?type=collection", ItemFingerprint: "sha256:outdoor-persons", Fields: fields, ConditionSupported: true,
	}
	profileService := newAuthorizedProfileServiceForTest(
		profiles,
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)

	profileService.SetResultReadChecker(&profileResultCheckerForTest{})
	response, err := profileService.GetCurrent(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileCurrentRequest{Locator: target.Locator})
	if err != nil {
		t.Fatal(err)
	}
	if response.Profile == nil || response.Profile.FieldCount != 1 || len(response.Profile.Fields) != 1 || response.Profile.Fields[0].Name != "name" || response.ConditionSupported {
		t.Fatalf("protected profile = %#v", response.Profile)
	}
	if storedProfile.FieldCount != 2 || len(storedProfile.Fields) != 2 {
		t.Fatal("stored profile was mutated")
	}
}

func TestDataProfileServiceAllowsManagedAllScopeAndRejectsConditionScope(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}
	target := &DataProfileTarget{
		Locator: "addp://engine/1/path/Outdoor/Persons?type=collection", ItemFingerprint: "sha256:outdoor-persons",
		EngineID: 1, Fields: fields, ConditionSupported: true,
	}
	executions := &dataProfileServiceTestExecutionStore{}
	profileService := newAuthorizedProfileServiceForTest(
		&dataProfileServiceTestProfileStore{}, executions, &dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)
	if _, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{Locator: target.Locator}); err != nil {
		t.Fatalf("all-scope CreateExecution() error = %v", err)
	}
	if executions.createCalls != 1 {
		t.Fatalf("all-scope execution creates = %d", executions.createCalls)
	}
	_, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{
		Locator: target.Locator,
		DataScope: dataprofile.DataScope{
			Kind: dataprofile.DataScopeKindCondition, Logic: dataprofile.DataScopeLogicAnd,
			Conditions: []dataprofile.DataScopeCondition{{Field: "phone", Operator: "eq", Value: "13661384499"}},
		},
	})
	if !errors.Is(err, ErrDataProfileProtectionRequired) {
		t.Fatalf("conditional CreateExecution() error = %v", err)
	}
	if executions.createCalls != 1 {
		t.Fatalf("conditional execution must not be created, calls = %d", executions.createCalls)
	}
}

func TestDataProfileServiceRejectsManagedDenyBeforeExecutionCreation(t *testing.T) {
	fields := []datatype.FieldInfo{{Name: "phone", Type: datatype.FieldTypeString}}
	target := &DataProfileTarget{Locator: "addp://engine/1/item/persons", ItemFingerprint: "sha256:persons", Fields: fields}
	executions := &dataProfileServiceTestExecutionStore{}
	profileService := newAuthorizedProfileServiceForTest(
		&dataProfileServiceTestProfileStore{}, executions, &dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectDeny),
	)
	if _, err := profileService.CreateExecution(context.Background(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{Locator: target.Locator}); !errors.Is(err, ErrDataProfileProtectionRequired) {
		t.Fatalf("CreateExecution() error = %v", err)
	}
	if executions.createCalls != 0 {
		t.Fatalf("deny execution creates = %d", executions.createCalls)
	}
}

func TestDataProfileServiceProtectsManagedProfileBeforePersistence(t *testing.T) {
	fields := []datatype.FieldInfo{
		{Name: "name", Type: datatype.FieldTypeString},
		{Name: "phone", Type: datatype.FieldTypeString},
	}
	target := &DataProfileTarget{EngineID: 1, ItemFingerprint: "sha256:outdoor-persons", SourceVersion: "version-1", Fields: fields}
	profiles := &dataProfileServiceTestProfileStore{}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{
		target: target,
		sample: &DataProfileSample{
			ReadSet:     profileReadSetForTest(t),
			Fields:      fields,
			Rows:        []map[string]interface{}{{"name": "daydayup", "phone": "13661384499"}},
			RowsScanned: 1,
		},
	}
	profileService := newAuthorizedProfileServiceForTest(
		profiles, executions, sampler,
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)
	profileService.runProfileExecutionForTest(t,
		context.Background(),
		target,
		dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
		"config",
		&commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-protected"},
		DefaultDataProfileBudget,
	)
	if profiles.replaceCalls != 1 || profiles.replaced == nil {
		t.Fatalf("persisted profile = %#v, calls = %d", profiles.replaced, profiles.replaceCalls)
	}
	if profiles.replaced.FieldCount != 1 || len(profiles.replaced.Fields) != 1 || profiles.replaced.Fields[0].Name != "name" {
		t.Fatalf("persisted protected profile = %#v", profiles.replaced)
	}
	if executions.failedCode != "" || !executions.completed {
		t.Fatalf("execution failed = %q, completed = %v", executions.failedCode, executions.completed)
	}
	facts, ok := executions.completedMetadata["lineage_facts"].(commonExecution.LineageFacts)
	if !ok || facts.SchemaVersion != commonExecution.LineageFactsSchemaVersion || len(facts.Inputs) != 1 {
		t.Fatalf("lineage_facts = %#v", executions.completedMetadata["lineage_facts"])
	}
	if facts.Inputs[0].Locator != target.Locator || facts.Inputs[0].ItemFingerprint != target.ItemFingerprint {
		t.Fatalf("lineage input = %#v", facts.Inputs[0])
	}
}

func TestDataProfileServiceFreezesActorAndIsolatesReuse(t *testing.T) {
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{EngineID: 1, Locator: "addp://engine/1/item/a", ItemFingerprint: "item-a", SourceVersion: "v1"}}
	svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, executions, sampler, &dataProfileServiceTestProtectionGate{})
	req := DataProfileExecutionRequest{Locator: sampler.target.Locator}
	base := profileAuthContextForTest()
	first, err := svc.CreateExecution(context.Background(), base, "addp_at_user", req)
	if err != nil || first.Execution == nil || first.Reused {
		t.Fatalf("first execution = %#v, %v", first, err)
	}
	stored := executions.createdExecution
	if stored.ActorPrincipalID == nil || *stored.ActorPrincipalID != 9 ||
		stored.ActorTenantMembershipID == nil || *stored.ActorTenantMembershipID != 12 ||
		stored.IssuedAuthorizationVersion == nil || *stored.IssuedAuthorizationVersion != 3 ||
		stored.TriggeredBy == nil || *stored.TriggeredBy != 9 || stored.TenantID != 7 {
		t.Fatalf("actor provenance was not frozen: %#v", stored)
	}
	if stored.ExecutionAuthorizationID == nil || stored.AuthorizationExpiresAt == nil {
		t.Fatal("formal issuance was not bound before acceptance")
	}
	// Refreshing a user token without changing authorization facts can reuse work.
	refreshed := profileAuthContextForTest()
	reused, err := svc.CreateExecution(context.Background(), refreshed, "addp_at_user", req)
	if err != nil || !reused.Reused || reused.Execution.ExecutionID != first.Execution.ExecutionID {
		t.Fatalf("same actor reuse = %#v, %v", reused, err)
	}
	firstKey := executions.createdKey
	for _, tc := range []struct {
		name   string
		change func(*authorization.AuthContext)
	}{
		{"principal", func(v *authorization.AuthContext) { v.Principal.ID = "10" }},
		{"membership", func(v *authorization.AuthContext) { id := "13"; v.Context.TenantMembershipID = &id }},
		{"authorization version", func(v *authorization.AuthContext) { v.Authorization.AuthorizationVersion = "4" }},
		{"tenant", func(v *authorization.AuthContext) { id := "8"; v.Context.TenantID = &id }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			actor := profileAuthContextForTest()
			tc.change(&actor)
			result, err := svc.CreateExecution(context.Background(), actor, "addp_at_user", req)
			if err != nil || result.Reused || result.Execution.ExecutionID == first.Execution.ExecutionID || executions.createdKey == firstKey {
				t.Fatalf("cross-actor reuse = %#v, %v", result, err)
			}
			key := executions.createdKey
			if _, err := svc.GetCurrent(context.Background(), actor, "addp_at_user", DataProfileCurrentRequest{Locator: req.Locator}); err != nil {
				t.Fatal(err)
			}
			if executions.activeKey != key || executions.latestKey != key {
				t.Fatal("current execution queries were not scoped to the same actor")
			}
		})
	}
	// Mutating the detached input after enqueue must not rewrite execution facts.
	base.Principal.ID = "20"
	*base.Context.TenantMembershipID = "21"
	base.Authorization.AuthorizationVersion = "22"
	if *stored.ActorPrincipalID != 9 || *stored.ActorTenantMembershipID != 12 || *stored.IssuedAuthorizationVersion != 3 {
		t.Fatal("queued provenance changed with request context")
	}
}

func TestDataProfileServiceRejectsUntrustedActorsBeforeResolving(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*authorization.AuthContext)
	}{
		{"missing context", func(v *authorization.AuthContext) { *v = authorization.AuthContext{} }},
		{"missing membership", func(v *authorization.AuthContext) { v.Context.TenantMembershipID = nil }},
		{"noncanonical principal", func(v *authorization.AuthContext) { v.Principal.ID = "09" }},
		{"overflow version", func(v *authorization.AuthContext) { v.Authorization.AuthorizationVersion = "9223372036854775808" }},
		{"expired", func(v *authorization.AuthContext) {
			v.Token.IssuedAt = time.Now().Add(-time.Hour)
			v.Token.ExpiresAt = time.Now().Add(-time.Minute)
		}},
		{"platform", func(v *authorization.AuthContext) {
			v.Context = authorization.AuthSessionContext{Type: "platform"}
			v.Authentication.AssuranceLevel = "aal2"
		}},
		{"service", func(v *authorization.AuthContext) {
			v.Principal.Type = "service_principal"
			v.Token.Type = "service_access_token"
		}},
		{"resource ticket", func(v *authorization.AuthContext) {
			v.Token.Type = "resource_access_ticket"
			v.Client.ScopeMode = "restricted"
			v.Client.Scopes = []string{"resource:read"}
			v.Client.Audiences = []string{"manager"}
		}},
		{"delegated", func(v *authorization.AuthContext) {
			v.Token.Type = "delegated_access_token"
			v.Client.ScopeMode = "restricted"
			v.Client.Scopes = []string{"data.preview"}
			v.Client.Audiences = []string{"manager"}
			v.Delegation = &authorization.DelegationFacts{DelegatedByClientID: "addp-web", AgentRunID: "run", ToolCallID: "call"}
		}},
		{"wrong audience", func(v *authorization.AuthContext) { v.Client.Audiences = []string{"data.preview"} }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			authContext := profileAuthContextForTest()
			tc.change(&authContext)
			switch tc.name {
			case "platform", "service", "resource ticket", "delegated", "wrong audience":
				if err := authorization.ValidateAuthContext(authContext); err != nil {
					t.Fatalf("fixture should be canonical before profiling rejects its provenance: %v", err)
				}
			}
			executions := &dataProfileServiceTestExecutionStore{}
			sampler := &dataProfileServiceTestSampler{}
			svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, executions, sampler, &dataProfileServiceTestProtectionGate{})
			wantError := ErrDataProfileActorRequired
			if tc.name == "expired" {
				wantError = ErrDataProfileActorExpired
			}
			if _, err := svc.CreateExecution(context.Background(), authContext, "addp_at_user", DataProfileExecutionRequest{Locator: "item"}); !errors.Is(err, wantError) {
				t.Fatalf("create error = %v", err)
			}
			if _, err := svc.GetCurrent(context.Background(), authContext, "addp_at_user", DataProfileCurrentRequest{Locator: "item"}); !errors.Is(err, wantError) {
				t.Fatalf("query error = %v", err)
			}
			if sampler.resolveCalls != 0 || executions.createCalls != 0 || executions.activeKey != "" || executions.latestKey != "" {
				t.Fatal("invalid actor reached metadata or execution repository")
			}
		})
	}
}

type dataProfileServiceTestProfileStore struct {
	replacedState *models.DataProfile
	state         *models.DataProfile
	profile       *dataprofile.Profile
	replaced      *dataprofile.Profile
	getCalls      int
	replaceCalls  int
}

func profileReadSetForTest(t *testing.T) *plugin.QueryReadSet {
	t.Helper()
	readSet, err := plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "C"), plugin.TabularItemPath(1, "schema", "public", "D"))
	if err != nil {
		t.Fatal(err)
	}
	return readSet
}

func profileResultStateForTest(t *testing.T) *models.DataProfile {
	t.Helper()
	payload, err := json.Marshal(map[string]any{"read_set": profileReadSetForTest(t)})
	if err != nil {
		t.Fatal(err)
	}
	return &models.DataProfile{ID: 12, EngineID: 1, LastExecutionID: "execution-12", DependencySnapshot: payload}
}

type profileResultCheckerForTest struct {
	err        error
	calls      int
	credential string
	paths      []plugin.EngineCatalogPath
}

func (c *profileResultCheckerForTest) CheckManagerProfileResultRead(_ context.Context, credential string, paths []plugin.EngineCatalogPath) error {
	c.calls++
	c.credential = credential
	c.paths = (&plugin.QueryReadSet{Paths: paths}).Clone().Paths
	return c.err
}

func TestProfileResultUsesActualFrozenSourcesAndCurrentCredential(t *testing.T) {
	state := profileResultStateForTest(t)
	store := &dataProfileServiceTestProfileStore{state: state, profile: &dataprofile.Profile{}}
	checker := &profileResultCheckerForTest{}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{EngineID: 1, ItemFingerprint: "current", DependencySnapshot: map[string]interface{}{"read_set": "must-not-be-used"}}}
	svc := newAuthorizedProfileServiceForTest(store, &dataProfileServiceTestExecutionStore{}, sampler, &dataProfileServiceTestProtectionGate{})
	svc.SetResultReadChecker(checker)
	for _, credential := range []string{"addp_at_A", "addp_at_B"} {
		response, err := svc.GetCurrent(t.Context(), profileAuthContextForTest(), credential, DataProfileCurrentRequest{Locator: "current"})
		if err != nil || response.Profile == nil || checker.credential != credential || !reflect.DeepEqual(checker.paths, profileReadSetForTest(t).Paths) {
			t.Fatalf("result used current target instead of actual sources: response=%+v error=%v paths=%+v", response, err, checker.paths)
		}
	}
	if checker.calls != 2 {
		t.Fatal("cached source Allow")
	}
	for _, denied := range []error{commonClient.ErrManagerPreviewReadDenied, commonClient.ErrManagerPreviewCredentialRejected, commonClient.ErrManagerPreviewReadUnavailable} {
		checker.err = denied
		response, err := svc.GetCurrent(t.Context(), profileAuthContextForTest(), "addp_at_B", DataProfileCurrentRequest{Locator: "current"})
		if response != nil || !errors.Is(err, denied) || store.replaceCalls != 0 {
			t.Fatalf("denied result leaked or mutated: %+v %v", response, err)
		}
	}
}

func TestProfileResultRejectsUnprovenSourcesWithoutReconstructing(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*models.DataProfile)
	}{
		{"historical snapshot", func(s *models.DataProfile) { s.DependencySnapshot = []byte(`{"item_id":1}`) }},
		{"malformed", func(s *models.DataProfile) { s.DependencySnapshot = []byte(`{`) }},
		{"empty", func(s *models.DataProfile) { s.DependencySnapshot = []byte(`{"read_set":{"paths":[]}}`) }},
		{"null", func(s *models.DataProfile) { s.DependencySnapshot = []byte(`{"read_set":null}`) }},
		{"unknown proof field", func(s *models.DataProfile) {
			var snapshot map[string]interface{}
			_ = json.Unmarshal(s.DependencySnapshot, &snapshot)
			snapshot["read_set"].(map[string]interface{})["unresolved"] = true
			s.DependencySnapshot, _ = json.Marshal(snapshot)
		}},
		{"oversized", func(s *models.DataProfile) {
			s.DependencySnapshot, _ = json.Marshal(map[string]interface{}{"read_set": plugin.QueryReadSet{Paths: make([]plugin.EngineCatalogPath, 201)}})
		}},
		{"missing execution", func(s *models.DataProfile) { s.LastExecutionID = "" }},
		{"wrong engine", func(s *models.DataProfile) { s.EngineID = 2 }},
		{"duplicate or unsorted", func(s *models.DataProfile) {
			paths := profileReadSetForTest(t).Paths
			s.DependencySnapshot, _ = json.Marshal(map[string]any{"read_set": plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{paths[1], paths[0], paths[0]}}})
		}},
		{"branch", func(s *models.DataProfile) {
			s.DependencySnapshot, _ = json.Marshal(map[string]any{"read_set": plugin.QueryReadSet{Paths: []plugin.EngineCatalogPath{plugin.TabularNamespacePath(1, "schema", "public")}}})
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			state := profileResultStateForTest(t)
			tc.change(state)
			checker := &profileResultCheckerForTest{}
			store := &dataProfileServiceTestProfileStore{state: state, profile: &dataprofile.Profile{}}
			svc := newAuthorizedProfileServiceForTest(store, &dataProfileServiceTestExecutionStore{}, &dataProfileServiceTestSampler{target: &DataProfileTarget{EngineID: 1, ItemFingerprint: "fingerprint", DependencySnapshot: map[string]interface{}{"read_set": profileReadSetForTest(t)}}}, &dataProfileServiceTestProtectionGate{})
			svc.SetResultReadChecker(checker)
			response, err := svc.GetCurrent(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileCurrentRequest{Locator: "current"})
			if response != nil || !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) || checker.calls != 0 || store.replaceCalls != 0 {
				t.Fatalf("unproven result returned/repaired: %+v %v", response, err)
			}
		})
	}
	svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{state: profileResultStateForTest(t), profile: &dataprofile.Profile{}}, &dataProfileServiceTestExecutionStore{}, &dataProfileServiceTestSampler{target: &DataProfileTarget{ItemFingerprint: "fingerprint"}}, &dataProfileServiceTestProtectionGate{})
	if response, err := svc.GetCurrent(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileCurrentRequest{Locator: "current"}); response != nil || !errors.Is(err, ErrDataProfileSourceAuthorizationRequired) {
		t.Fatal("missing checker bypassed")
	}
}

func TestProfilePublicationRequiresAndFreezesActualSamplingSources(t *testing.T) {
	for _, proven := range []bool{false, true} {
		store := &dataProfileServiceTestProfileStore{}
		executions := &dataProfileServiceTestExecutionStore{}
		sample := &DataProfileSample{}
		if proven {
			sample.ReadSet = profileReadSetForTest(t)
		}
		target := &DataProfileTarget{EngineID: 1, ItemFingerprint: "fingerprint", DependencySnapshot: map[string]interface{}{"read_set": "not-a-source-proof"}}
		svc := newAuthorizedProfileServiceForTest(store, executions, &dataProfileServiceTestSampler{sample: sample}, &dataProfileServiceTestProtectionGate{})
		svc.runProfileExecutionForTest(t, t.Context(), target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution"}, DefaultDataProfileBudget)
		if !proven {
			if store.replaceCalls != 0 || executions.completed || executions.failedCode != "source_authorization_required" {
				t.Fatal("unproven sample published")
			}
			continue
		}
		readSet, err := profileResultReadSet(store.replacedState)
		if err != nil || !reflect.DeepEqual(readSet, sample.ReadSet) || target.DependencySnapshot["read_set"] != "not-a-source-proof" {
			t.Fatalf("sampling sources not frozen: %+v %v", readSet, err)
		}
		sample.ReadSet.Paths[0].Segments[1].Name = "changed"
		frozen, err := profileResultReadSet(store.replacedState)
		if err != nil || !reflect.DeepEqual(frozen, profileReadSetForTest(t)) {
			t.Fatal("stored proof shared mutable sample")
		}
	}
}

func (s *dataProfileServiceTestProfileStore) GetCurrent(context.Context, uint, string, string, string) (*models.DataProfile, *dataprofile.Profile, error) {
	s.getCalls++
	return s.state, s.profile, nil
}

func (s *dataProfileServiceTestProfileStore) ReplaceCurrent(_ context.Context, _ *gorm.DB, state *models.DataProfile, profile dataprofile.Profile) error {
	s.replacedState = state
	s.replaceCalls++
	s.replaced = &profile
	return nil
}

type dataProfileServiceTestExecutionStore struct {
	createdExecution  *commonExecution.TaskExecution
	createdKey        string
	activeKey         string
	latestKey         string
	activeByKey       map[string]*commonExecution.TaskExecution
	failedCode        string
	byID              *commonExecution.TaskExecution
	createCalls       int
	completed         bool
	completedMetadata map[string]interface{}
}

func newAuthorizedProfileServiceForTest(profiles dataProfileStore, executions dataProfileExecutionStore, sampler DataProfileSampleProvider, gate dataProfileProtectionStore) *DataProfileService {
	svc := NewDataProfileService(profiles, executions, sampler, gate)
	svc.SetAuthorizationIssuer(&profileIssuerForTest{store: executions})
	svc.authorizationConsumer = &profileConsumerForTest{}
	return svc
}

// Low-level aggregate/protection tests explicitly supply a deterministic gate.
// Worker authorization is covered separately through the real claimed path.
func (s *DataProfileService) runProfileExecutionForTest(t *testing.T, ctx context.Context, target *DataProfileTarget, scope dataprofile.DataScope, hash string, item *commonExecution.TaskExecution, budget DataProfileBudget) {
	t.Helper()
	positions, err := dataProfilePagePositions(target.RowCount, scope, budget)
	if err != nil {
		t.Fatal(err)
	}
	set := profileReadSetForTest(t)
	if sampler, ok := s.sampler.(*dataProfileServiceTestSampler); ok && sampler.sample != nil && sampler.sample.ReadSet != nil {
		set = sampler.sample.ReadSet.Clone()
	}
	plan := &DataProfileSamplePlan{pages: &profilePreparedPagesForTest{set: set, positions: positions}, model: plugin.TabularCatalogModel("schema")}
	s.runExecution(ctx, target, scope, hash, item, budget, plan, func(ctx context.Context) error { return ctx.Err() })
}

type profileIssuerForTest struct {
	store  dataProfileExecutionStore
	calls  int
	err    error
	change func(*commonClient.IssuedManagerProfileAuthorization)
}

func TestDataProfileServiceIssueFailureClosesUnboundExecution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		err    error
		change func(*commonClient.IssuedManagerProfileAuthorization)
		want   error
	}{
		{name: "denied", err: &commonClient.SystemAPIError{StatusCode: 403}, want: ErrDataProfileSourceAuthorizationRequired},
		{name: "expired user", err: &commonClient.SystemAPIError{StatusCode: 401}, want: ErrDataProfileActorExpired},
		{name: "unavailable", err: errors.New("issuer unavailable"), want: ErrDataProfileUnavailable},
		{name: "wrong tenant", change: func(v *commonClient.IssuedManagerProfileAuthorization) { v.TenantID = "8" }, want: ErrDataProfileSourceAuthorizationRequired},
		{name: "wrong execution", change: func(v *commonClient.IssuedManagerProfileAuthorization) { v.ExecutionID = "other" }, want: ErrDataProfileSourceAuthorizationRequired},
		{name: "wrong digest", change: func(v *commonClient.IssuedManagerProfileAuthorization) { v.SourceReadScope.ConfigDigest = "invalid" }, want: ErrDataProfileSourceAuthorizationRequired},
	} {
		t.Run(tc.name, func(t *testing.T) {
			store := &dataProfileServiceTestExecutionStore{}
			sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{EngineID: 1, Locator: "addp://engine/1/item/a", ItemFingerprint: "a", SourceVersion: "v1"}}
			svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, store, sampler, &dataProfileServiceTestProtectionGate{})
			issuer := &profileIssuerForTest{store: store, err: tc.err, change: tc.change}
			svc.SetAuthorizationIssuer(issuer)
			result, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{Locator: sampler.target.Locator})
			if !errors.Is(err, tc.want) || result != nil || issuer.calls != 1 {
				t.Fatalf("response=%#v error=%v issuer=%d", result, err, issuer.calls)
			}
			if store.createdExecution.Status != commonExecution.ExecutionStatusFailed || store.createdExecution.ExecutionAuthorizationID != nil || len(store.activeByKey) != 0 {
				t.Fatal("failed issuance left an active or bound execution")
			}
		})
	}
}

func TestDataProfileServiceBoundReuseDoesNotResignAndUnboundIsNotAccepted(t *testing.T) {
	store := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{EngineID: 1, Locator: "addp://engine/1/item/a", ItemFingerprint: "a", SourceVersion: "v1"}}
	svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, store, sampler, &dataProfileServiceTestProtectionGate{})
	issuer := &profileIssuerForTest{store: store}
	svc.SetAuthorizationIssuer(issuer)
	req := DataProfileExecutionRequest{Locator: sampler.target.Locator}
	first, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", req)
	if err != nil {
		t.Fatal(err)
	}
	second, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", req)
	if err != nil || !second.Reused || first.Execution.ExecutionID != second.Execution.ExecutionID || issuer.calls != 1 {
		t.Fatalf("reuse=%#v %v calls=%d", second, err, issuer.calls)
	}
	for _, execution := range store.activeByKey {
		execution.ExecutionAuthorizationID = nil
		execution.AuthorizationExpiresAt = nil
	}
	if response, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", req); response != nil || !errors.Is(err, ErrDataProfileUnavailable) || issuer.calls != 1 {
		t.Fatalf("unbound accepted or resigned: %#v %v %d", response, err, issuer.calls)
	}
}

func (p *profileIssuerForTest) IssueManagerProfile(ctx context.Context, credential string, req commonClient.IssueManagerProfileAuthorizationRequest) (*commonClient.IssuedManagerProfileAuthorization, error) {
	p.calls++
	if p.err != nil {
		return nil, p.err
	}
	tenantID := 7
	if store, ok := p.store.(*dataProfileServiceTestExecutionStore); ok {
		for _, execution := range store.activeByKey {
			if execution.ExecutionID == req.ExecutionID {
				tenantID = execution.TenantID
			}
		}
	}
	raw, err := p.store.GetRawExecutionConfig(ctx, tenantID, req.ExecutionID)
	if err != nil {
		return nil, err
	}
	var config frozenDataProfileConfig
	if err := json.Unmarshal(raw, &config); err != nil {
		return nil, err
	}
	scope, err := commonExecution.NewManagerProfileReadScope(raw, &config.ReadSet)
	if err != nil {
		return nil, err
	}
	issued := &commonClient.IssuedManagerProfileAuthorization{ID: "41", TenantID: strconv.Itoa(tenantID), ExecutionID: req.ExecutionID, Audience: "manager", SourceReadScope: *scope, ExpiresAt: time.Now().Add(15 * time.Minute)}
	if p.change != nil {
		p.change(issued)
	}
	return issued, nil
}

func (s *dataProfileServiceTestExecutionStore) GetRawExecutionConfig(_ context.Context, _ int, id string) (json.RawMessage, error) {
	for _, e := range s.activeByKey {
		if e.ExecutionID == id {
			return json.Marshal(e.ExecutionConfig)
		}
	}
	return nil, errors.New("missing execution")
}
func (s *dataProfileServiceTestExecutionStore) BindSourceAuthorization(_ context.Context, expected *commonExecution.TaskExecution, scope commonExecution.ManagerProfileReadScope, issued *commonClient.IssuedManagerProfileAuthorization) error {
	if !issued.Matches(uint(expected.TenantID), scope) {
		return errors.New("mismatch")
	}
	id, _ := strconv.ParseInt(issued.ID, 10, 64)
	expected.ExecutionAuthorizationID, expected.AuthorizationExpiresAt = &id, &issued.ExpiresAt
	return nil
}
func (s *dataProfileServiceTestExecutionStore) FailUnbound(_ context.Context, expected *commonExecution.TaskExecution) error {
	if expected.ExecutionAuthorizationID != nil {
		return errors.New("already bound")
	}
	expected.Status = commonExecution.ExecutionStatusFailed
	s.failedCode = "source_authorization_required"
	delete(s.activeByKey, s.createdKey)
	return nil
}

func (s *dataProfileServiceTestExecutionStore) CreateOrReuseActive(_ context.Context, key string, execution *commonExecution.TaskExecution) (*commonExecution.TaskExecution, bool, error) {
	s.createCalls++
	s.createdExecution = execution
	s.createdKey = key
	if previous := s.activeByKey[key]; previous != nil {
		return previous, false, nil
	}
	if s.activeByKey == nil {
		s.activeByKey = make(map[string]*commonExecution.TaskExecution)
	}
	s.activeByKey[key] = execution
	return execution, true, nil
}
func (s *dataProfileServiceTestExecutionStore) GetActive(_ context.Context, _ int, key string) (*commonExecution.TaskExecution, error) {
	s.activeKey = key
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetLatest(_ context.Context, _ int, key string) (*commonExecution.TaskExecution, error) {
	s.latestKey = key
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetByExecutionID(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return s.byID, nil
}
func (s *dataProfileServiceTestExecutionStore) Start(context.Context, int, string, time.Time) error {
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Complete(_ context.Context, _ int, _ string, _ time.Time, _ int64, metadata map[string]interface{}) error {
	s.completed = true
	s.completedMetadata = metadata
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Fail(_ context.Context, _ int, _ string, _ time.Time, code string, _ string) error {
	s.failedCode = code
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Timeout(_ context.Context, _ int, _ string, _ time.Time, code string, _ string) error {
	s.failedCode = code
	return nil
}

type dataProfileServiceTestSampler struct {
	resolveCalls int
	sampleCalls  int
	sampleBudget DataProfileBudget
	deadline     time.Time
	target       *DataProfileTarget
	sample       *DataProfileSample
	sampleErr    error
	onSample     func()
	changePlan   func(*DataProfileSamplePlan)
}

type profilePreparedPagesForTest struct {
	set       *plugin.QueryReadSet
	positions []preview.TablePage
}

func (p *profilePreparedPagesForTest) ReadSet() *plugin.QueryReadSet { return p.set.Clone() }
func (p *profilePreparedPagesForTest) Positions() []preview.TablePage {
	return append([]preview.TablePage(nil), p.positions...)
}
func (p *profilePreparedPagesForTest) Query(int) (plugin.PreparedQuery, error) {
	return nil, errors.New("test preparation has no source query")
}
func (s *dataProfileServiceTestSampler) Prepare(_ context.Context, target *DataProfileTarget, scope dataprofile.DataScope, budget DataProfileBudget) (*DataProfileSamplePlan, error) {
	positions, err := dataProfilePagePositions(target.RowCount, scope, budget)
	if err != nil {
		return nil, err
	}
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(target.EngineID, "schema", "public", "orders"))
	if err != nil {
		return nil, err
	}
	plan := &DataProfileSamplePlan{pages: &profilePreparedPagesForTest{set: set, positions: positions}, model: plugin.TabularCatalogModel("schema")}
	if s.changePlan != nil {
		s.changePlan(plan)
	}
	return plan, nil
}

func (s *dataProfileServiceTestSampler) ResolveTarget(context.Context, uint, string, DataProfileSelection) (*DataProfileTarget, error) {
	s.resolveCalls++
	return s.target, nil
}

func profileAuthContextForTest() authorization.AuthContext {
	tenantID, membershipID, clientID := "7", "12", "addp-web"
	now := time.Now().UTC()
	return authorization.AuthContext{
		SchemaVersion:  authorization.AuthContextSchemaVersion,
		Principal:      authorization.AuthPrincipal{Type: "user", ID: "9"},
		Context:        authorization.AuthSessionContext{Type: "tenant", TenantID: &tenantID, TenantMembershipID: &membershipID},
		Authentication: authorization.AuthenticationFacts{Methods: []string{"password"}, AssuranceLevel: "aal1", AuthenticatedAt: now},
		Client:         authorization.ClientConstraints{ClientID: &clientID, Audiences: []string{"addp.api"}, ScopeMode: "unrestricted", Scopes: []string{}},
		Organization:   authorization.OrganizationContext{Departments: []authorization.DepartmentMembership{}, ProjectGroups: []authorization.ProjectGroupMembership{}},
		Authorization:  authorization.AuthorizationFacts{AuthorizationVersion: "3", RoleAssignments: []authorization.RoleAssignment{}},
		Token:          authorization.TokenFacts{Type: "first_party_access_token", IssuedAt: now, ExpiresAt: now.Add(time.Hour)},
	}
}
func (s *dataProfileServiceTestSampler) Sample(ctx context.Context, _ *DataProfileTarget, _ dataprofile.DataScope, budget DataProfileBudget, plan *DataProfileSamplePlan, beforeRead func(context.Context) error) (*DataProfileSample, error) {
	s.sampleCalls++
	s.sampleBudget = budget
	s.deadline, _ = ctx.Deadline()
	for range plan.Positions() {
		if err := beforeRead(ctx); err != nil {
			return nil, err
		}
	}
	if s.onSample != nil {
		s.onSample()
	}
	return s.sample, s.sampleErr
}

func queuedProfileForTest(t *testing.T, budget DataProfileBudget) (*DataProfileService, *dataProfileServiceTestSampler, *dataProfileServiceTestExecutionStore) {
	t.Helper()
	target := &DataProfileTarget{EngineID: 1, Locator: "addp://engine/1/path/public/orders?type=table", ItemFingerprint: "orders", SourceVersion: "version"}
	set, err := plugin.NewQueryReadSet(plugin.TabularItemPath(1, "schema", "public", "orders"))
	if err != nil {
		t.Fatal(err)
	}
	sampler := &dataProfileServiceTestSampler{target: target, sample: &DataProfileSample{ReadSet: set}}
	executions := &dataProfileServiceTestExecutionStore{}
	svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, executions, sampler, &dataProfileServiceTestProtectionGate{})
	svc.budget = budget
	if _, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{Locator: target.Locator}); err != nil {
		t.Fatal(err)
	}
	// Exercise the durable JSON representation, not the in-memory config types.
	payload, err := json.Marshal(executions.createdExecution.ExecutionConfig)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(payload, &executions.createdExecution.ExecutionConfig); err != nil {
		t.Fatal(err)
	}
	sampler.resolveCalls = 0
	claimProfileForTest(executions.createdExecution)
	return svc, sampler, executions
}

func TestDataProfileWorkerConsumesFrozenBudgetNotCurrentDefaults(t *testing.T) {
	budget := DataProfileBudget{SampleSize: 10, MaxRowsScanned: 20, PageSize: 5, Timeout: 30 * time.Second}
	svc, sampler, executions := queuedProfileForTest(t, budget)
	svc.budget = DataProfileBudget{SampleSize: 1000, MaxRowsScanned: 10000, PageSize: 500, Timeout: 5 * time.Minute}
	started := time.Now()
	if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); err != nil {
		t.Fatal(err)
	}
	if sampler.sampleCalls != 1 || sampler.sampleBudget != budget || !executions.completed {
		t.Fatalf("frozen budget not consumed: calls=%d budget=%+v completed=%v", sampler.sampleCalls, sampler.sampleBudget, executions.completed)
	}
	if sampler.deadline.IsZero() || sampler.deadline.After(time.Now().Add(budget.Timeout)) || sampler.deadline.Before(started.Add(budget.Timeout)) {
		t.Fatalf("timeout did not use frozen budget: %s", sampler.deadline)
	}
}

func TestDataProfileConfigHashIncludesTimeout(t *testing.T) {
	budget := DefaultDataProfileBudget
	first := dataProfileConfigHash(DataProfileSelection{}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget)
	budget.Timeout += time.Millisecond
	if first == dataProfileConfigHash(DataProfileSelection{}, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, budget) {
		t.Fatal("different timeout budgets share configuration identity")
	}
}

func TestDataProfileWorkerRejectsInvalidFrozenConfigBeforeSourceResolution(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(map[string]interface{})
	}{
		{"missing budget", func(c map[string]interface{}) { delete(c, "budget") }},
		{"missing timeout", func(c map[string]interface{}) { delete(c["budget"].(map[string]interface{}), "timeout_ms") }},
		{"zero sample", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["sample_size"] = 0 }},
		{"max rows below sample", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["max_rows_scanned"] = 1 }},
		{"page exceeds max", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["page_size"] = 100000 }},
		{"overflow timeout", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["timeout_ms"] = int64(1<<63 - 1) }},
		{"negative timeout", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["timeout_ms"] = -1 }},
		{"changed timeout without digest", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["timeout_ms"] = 60000 }},
		{"old version", func(c map[string]interface{}) { c["config_version"] = "data-profile-config/v4" }},
		{"missing version", func(c map[string]interface{}) { delete(c, "config_version") }},
		{"missing engine", func(c map[string]interface{}) { delete(c, "engine_id") }},
		{"full mode", func(c map[string]interface{}) { c["profile_mode"] = "full" }},
		{"unknown property", func(c map[string]interface{}) { c["user_access_token"] = "not-a-credential" }},
		{"unknown budget property", func(c map[string]interface{}) { c["budget"].(map[string]interface{})["unlimited"] = true }},
		{"changed sample method", func(c map[string]interface{}) { c["sample_method"] = "unbounded" }},
		{"missing digest", func(c map[string]interface{}) { delete(c, "profile_config_hash") }},
		{"changed reuse key", func(c map[string]interface{}) { c["target_key"] = "other-actor" }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sampler, executions := queuedProfileForTest(t, DefaultDataProfileBudget)
			tc.change(executions.createdExecution.ExecutionConfig)
			if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); !errors.Is(err, ErrDataProfileInvalidRequest) {
				t.Fatalf("invalid frozen config: %v", err)
			}
			if sampler.resolveCalls != 0 || sampler.sampleCalls != 0 || executions.completed {
				t.Fatal("invalid configuration reached source resolution or sampling")
			}
		})
	}
}

func TestDataProfileWorkerRejectsChangedTargetBeforeSampling(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*dataProfileServiceTestSampler)
	}{
		{"engine", func(s *dataProfileServiceTestSampler) { s.target.EngineID = 2 }},
		{"fingerprint", func(s *dataProfileServiceTestSampler) { s.target.ItemFingerprint = "other" }},
		{"source version", func(s *dataProfileServiceTestSampler) { s.target.SourceVersion = "other" }},
		{"locator", func(s *dataProfileServiceTestSampler) { s.target.Locator = "other" }},
		{"selection", func(s *dataProfileServiceTestSampler) { s.target.Selection.ChildName = "other" }},
		{"missing target", func(s *dataProfileServiceTestSampler) { s.target = nil }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			svc, sampler, executions := queuedProfileForTest(t, DefaultDataProfileBudget)
			tc.change(sampler)
			if err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution); !errors.Is(err, ErrDataProfileSourceChanged) {
				t.Fatalf("changed target: %v", err)
			}
			if sampler.sampleCalls != 0 || executions.completed {
				t.Fatal("changed target reached sampling")
			}
		})
	}
}

func TestDataProfileEnqueueRejectsInvalidServerBudget(t *testing.T) {
	for _, budget := range []DataProfileBudget{
		{}, {SampleSize: 1, MaxRowsScanned: 1, PageSize: 1, Timeout: time.Microsecond},
		{SampleSize: 2, MaxRowsScanned: 1, PageSize: 1, Timeout: time.Second},
		{SampleSize: 1, MaxRowsScanned: 1, PageSize: 2, Timeout: time.Second},
	} {
		sampler := &dataProfileServiceTestSampler{}
		executions := &dataProfileServiceTestExecutionStore{}
		svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, executions, sampler, &dataProfileServiceTestProtectionGate{})
		svc.budget = budget
		if _, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{}); !errors.Is(err, ErrDataProfileInvalidRequest) || sampler.resolveCalls != 0 || executions.createCalls != 0 {
			t.Fatalf("invalid budget enqueued or normalized: %+v, %v", budget, err)
		}
	}
}

func TestDataProfileEnqueueFreezesNestedConfiguration(t *testing.T) {
	itemID := uint(12)
	target := &DataProfileTarget{EngineID: 1, ItemID: &itemID, Locator: "addp://engine/1/path/public/orders?type=table",
		ItemFingerprint: "orders", SourceVersion: "version", ConditionSupported: true,
		Fields: []datatype.FieldInfo{{Name: "status", Type: datatype.FieldTypeString}}}
	executions := &dataProfileServiceTestExecutionStore{}
	svc := newAuthorizedProfileServiceForTest(&dataProfileServiceTestProfileStore{}, executions,
		&dataProfileServiceTestSampler{target: target}, &dataProfileServiceTestProtectionGate{})
	response, err := svc.CreateExecution(t.Context(), profileAuthContextForTest(), "addp_at_user", DataProfileExecutionRequest{
		Locator: target.Locator, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindCondition,
			Logic: dataprofile.DataScopeLogicAnd, Conditions: []dataprofile.DataScopeCondition{
				{Field: "status", Operator: "in", Values: []interface{}{"active", "pending"}},
			}},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := json.Marshal(executions.createdExecution.ExecutionConfig)
	if err != nil {
		t.Fatal(err)
	}
	itemID = 99
	response.DataScope.Conditions[0].Values[0] = "other"
	svc.budget.Timeout = time.Hour
	after, err := json.Marshal(executions.createdExecution.ExecutionConfig)
	if err != nil || string(before) != string(after) {
		t.Fatalf("queued config changed with caller references: %s -> %s, %v", before, after, err)
	}
}

func TestDataProfileWorkerRequiresConsistentActorProvenance(t *testing.T) {
	for _, change := range []func(*commonExecution.TaskExecution){
		func(e *commonExecution.TaskExecution) { e.ActorPrincipalID = nil },
		func(e *commonExecution.TaskExecution) { e.ActorTenantMembershipID = nil },
		func(e *commonExecution.TaskExecution) { e.IssuedAuthorizationVersion = nil },
		func(e *commonExecution.TaskExecution) { id := int64(10); e.ActorPrincipalID = &id },
		func(e *commonExecution.TaskExecution) { id := int64(13); e.ActorTenantMembershipID = &id },
		func(e *commonExecution.TaskExecution) { version := int64(4); e.IssuedAuthorizationVersion = &version },
	} {
		svc, sampler, executions := queuedProfileForTest(t, DefaultDataProfileBudget)
		change(executions.createdExecution)
		err := svc.runClaimedExecution(profileClaimContextForTest(t, executions.createdExecution), executions.createdExecution)
		if (!errors.Is(err, ErrDataProfileActorRequired) && !errors.Is(err, ErrDataProfileInvalidRequest)) || sampler.resolveCalls != 0 || sampler.sampleCalls != 0 {
			t.Fatalf("changed provenance reached source: %v, resolves=%d samples=%d", err, sampler.resolveCalls, sampler.sampleCalls)
		}
	}
}

type dataProfileServiceTestProtectionGate struct {
	managed   bool
	result    projectionstore.GateResult
	commitErr error
}

func (g *dataProfileServiceTestProtectionGate) CaptureVersion(_ context.Context, _ int64, observe func(projectionstore.GateReader) error) (projectionstore.Version, error) {
	return projectionstore.Version{}, observe(g)
}

func (g *dataProfileServiceTestProtectionGate) CommitVersion(_ context.Context, _ int64, _ projectionstore.Version, commit func(*gorm.DB, projectionstore.GateReader) error) error {
	if g.commitErr != nil {
		return g.commitErr
	}
	return commit(nil, g)
}

func (g *dataProfileServiceTestProtectionGate) Gate(_ int64, target dataprotection.ResourceReference, _ time.Time) projectionstore.GateResult {
	if g.result.Managed {
		if len(g.result.Projections) > 0 && g.result.Projections[0].Target != target {
			return projectionstore.GateResult{}
		}
		return g.result
	}
	return projectionstore.GateResult{Managed: g.managed}
}

func managedDataProfileServiceTestGate(t *testing.T, itemFingerprint string, fields []datatype.FieldInfo, effect string) *dataProfileServiceTestProtectionGate {
	t.Helper()
	component := dataprotection.Component{
		Key: "phone", Path: []dataprotection.PathSegment{{Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString),
	}
	fingerprint, err := dataprotection.ComponentSchemaFingerprint(fields, component)
	if err != nil {
		t.Fatal(err)
	}
	component.SchemaFingerprint = fingerprint
	snapshotHash, err := dataprotection.TableSchemaSnapshotHash(fields)
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	projection := dataprotection.Projection{
		SchemaVersion: dataprotection.ProjectionSchemaV2, ProjectionID: "manager-profile-test", Revision: "00000000000000000001",
		ConsumerOwner: "manager", State: dataprotection.ProjectionStateActive,
		Target:             dataprotection.ResourceReference{OwnerModule: "meta", ResourceType: "data_item", ResourceIdentity: itemFingerprint},
		SourceSnapshotHash: snapshotHash,
		Rules: []dataprotection.Rule{{
			Action: "profile", Component: component, Decision: dataprotection.Decision{Effect: effect, InvalidValueEffect: effect},
		}},
		ValidFrom: now.Add(-time.Minute), ExpiresAt: now.Add(time.Hour),
	}
	if err := projection.Seal(); err != nil {
		t.Fatal(err)
	}
	return &dataProfileServiceTestProtectionGate{result: projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive, Projections: []dataprotection.Projection{projection},
	}}
}
