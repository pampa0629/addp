package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
)

func TestDataProfileServiceGetCurrentMarksStoredResultStale(t *testing.T) {
	profile := &dataprofile.Profile{SchemaVersion: dataprofile.SchemaVersionV2, Mode: dataprofile.ModeSample, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}}
	profiles := &dataProfileServiceTestProfileStore{
		state:   &models.DataProfile{ID: 12, SourceVersion: "old-version"},
		profile: profile,
	}
	completedAt := time.Date(2026, 7, 27, 2, 0, 0, 0, time.UTC)
	executions := &dataProfileServiceTestExecutionStore{byID: &commonExecution.TaskExecution{
		ExecutionID: "execution-12", Status: commonExecution.ExecutionStatusSuccess, CompletedAt: &completedAt,
	}}
	profiles.state.LastExecutionID = "execution-12"
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator: "addp://engine/1/item/a", ItemFingerprint: "fingerprint", SourceVersion: "new-version",
	}}
	profileService := NewDataProfileService(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{})

	response, err := profileService.GetCurrent(context.Background(), 7, DataProfileCurrentRequest{Locator: sampler.target.Locator})
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
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{},
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{},
		&dataProfileServiceTestProtectionGate{},
	)
	_, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{
		Locator: "addp://engine/1/item/a",
		Mode:    "full",
	})
	if !errors.Is(err, ErrDataProfileInvalidRequest) {
		t.Fatalf("CreateExecution() error = %v, want ErrDataProfileInvalidRequest", err)
	}
}

func TestDataProfileServiceRejectsConditionalScopeWithoutProviderSupport(t *testing.T) {
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{},
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{target: &DataProfileTarget{
			Locator:         "addp://engine/1/path/public/orders?type=table",
			ItemFingerprint: "sha256:orders",
			Fields:          []datatype.FieldInfo{{Name: "status", Type: datatype.FieldTypeString}},
		}},
		&dataProfileServiceTestProtectionGate{},
	)
	_, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{
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
	profileService := NewDataProfileService(profiles, executions, sampler, &dataProfileServiceTestProtectionGate{})
	target := &DataProfileTarget{ItemFingerprint: "fingerprint", SourceVersion: "version"}
	execution := &commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-1"}

	profileService.runExecution(target, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, "config", execution)

	if profiles.replaceCalls != 0 {
		t.Fatalf("ReplaceCurrent calls = %d, want 0", profiles.replaceCalls)
	}
	if executions.failedCode != "sample_failed" {
		t.Fatalf("failed code = %q, want sample_failed", executions.failedCode)
	}
}

func TestDataProfileServiceRejectsManagedCurrentResultBeforeStoreRead(t *testing.T) {
	profiles := &dataProfileServiceTestProfileStore{profile: &dataprofile.Profile{}}
	sampler := &dataProfileServiceTestSampler{target: &DataProfileTarget{
		Locator:         "addp://engine/1/path/Outdoor/Persons?type=collection",
		ItemFingerprint: "sha256:outdoor-persons",
	}}
	profileService := NewDataProfileService(
		profiles,
		&dataProfileServiceTestExecutionStore{},
		sampler,
		&dataProfileServiceTestProtectionGate{managed: true},
	)

	_, err := profileService.GetCurrent(context.Background(), 7, DataProfileCurrentRequest{Locator: sampler.target.Locator})
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
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{},
		executions,
		sampler,
		&dataProfileServiceTestProtectionGate{managed: true},
	)

	_, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{Locator: sampler.target.Locator})
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
	profiles := &dataProfileServiceTestProfileStore{profile: storedProfile}
	target := &DataProfileTarget{
		Locator: "addp://engine/1/path/Outdoor/Persons?type=collection", ItemFingerprint: "sha256:outdoor-persons", Fields: fields, ConditionSupported: true,
	}
	profileService := NewDataProfileService(
		profiles,
		&dataProfileServiceTestExecutionStore{},
		&dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)

	response, err := profileService.GetCurrent(context.Background(), 7, DataProfileCurrentRequest{Locator: target.Locator})
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
		Fields: fields, ConditionSupported: true,
	}
	executions := &dataProfileServiceTestExecutionStore{}
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{}, executions, &dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)
	if _, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{Locator: target.Locator}); err != nil {
		t.Fatalf("all-scope CreateExecution() error = %v", err)
	}
	if executions.createCalls != 1 {
		t.Fatalf("all-scope execution creates = %d", executions.createCalls)
	}
	_, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{
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
	profileService := NewDataProfileService(
		&dataProfileServiceTestProfileStore{}, executions, &dataProfileServiceTestSampler{target: target},
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectDeny),
	)
	if _, err := profileService.CreateExecution(context.Background(), 7, 9, DataProfileExecutionRequest{Locator: target.Locator}); !errors.Is(err, ErrDataProfileProtectionRequired) {
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
	target := &DataProfileTarget{ItemFingerprint: "sha256:outdoor-persons", SourceVersion: "version-1", Fields: fields}
	profiles := &dataProfileServiceTestProfileStore{}
	executions := &dataProfileServiceTestExecutionStore{}
	sampler := &dataProfileServiceTestSampler{
		target: target,
		sample: &DataProfileSample{
			Fields:      fields,
			Rows:        []map[string]interface{}{{"name": "daydayup", "phone": "13661384499"}},
			RowsScanned: 1,
		},
	}
	profileService := NewDataProfileService(
		profiles, executions, sampler,
		managedDataProfileServiceTestGate(t, target.ItemFingerprint, fields, dataprotection.EffectSuppress),
	)
	profileService.runExecution(
		target,
		dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
		"config",
		&commonExecution.TaskExecution{TenantID: 7, ExecutionID: "execution-protected"},
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
}

type dataProfileServiceTestProfileStore struct {
	state        *models.DataProfile
	profile      *dataprofile.Profile
	replaced     *dataprofile.Profile
	getCalls     int
	replaceCalls int
}

func (s *dataProfileServiceTestProfileStore) GetCurrent(context.Context, uint, string, string, string) (*models.DataProfile, *dataprofile.Profile, error) {
	s.getCalls++
	return s.state, s.profile, nil
}

func (s *dataProfileServiceTestProfileStore) ReplaceCurrent(_ context.Context, _ *models.DataProfile, profile dataprofile.Profile) error {
	s.replaceCalls++
	s.replaced = &profile
	return nil
}

type dataProfileServiceTestExecutionStore struct {
	failedCode  string
	byID        *commonExecution.TaskExecution
	createCalls int
	completed   bool
}

func (s *dataProfileServiceTestExecutionStore) CreateOrReuseActive(context.Context, string, *commonExecution.TaskExecution) (*commonExecution.TaskExecution, bool, error) {
	s.createCalls++
	return nil, false, nil
}
func (s *dataProfileServiceTestExecutionStore) GetActive(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetLatest(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return nil, nil
}
func (s *dataProfileServiceTestExecutionStore) GetByExecutionID(context.Context, int, string) (*commonExecution.TaskExecution, error) {
	return s.byID, nil
}
func (s *dataProfileServiceTestExecutionStore) Start(context.Context, int, string, time.Time) error {
	return nil
}
func (s *dataProfileServiceTestExecutionStore) Complete(context.Context, int, string, time.Time, int64, map[string]interface{}) error {
	s.completed = true
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
	target    *DataProfileTarget
	sample    *DataProfileSample
	sampleErr error
}

func (s *dataProfileServiceTestSampler) ResolveTarget(context.Context, uint, string, DataProfileSelection) (*DataProfileTarget, error) {
	return s.target, nil
}
func (s *dataProfileServiceTestSampler) Sample(context.Context, *DataProfileTarget, dataprofile.DataScope, DataProfileBudget) (*DataProfileSample, error) {
	return s.sample, s.sampleErr
}

type dataProfileServiceTestProtectionGate struct {
	managed bool
	result  projectionstore.GateResult
}

func (g *dataProfileServiceTestProtectionGate) Gate(int64, dataprotection.ResourceReference, time.Time) projectionstore.GateResult {
	if g.result.Managed {
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
		SchemaVersion: dataprotection.ProjectionSchemaV1, ProjectionID: "manager-profile-test", Revision: "00000000000000000001",
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
