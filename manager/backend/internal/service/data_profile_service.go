package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/authorization"
	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	"github.com/addp/manager/internal/profilefilter"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

const dataProfileConfigVersion = "data-profile-config/v6"

type frozenDataProfileBudget struct {
	SampleSize     int   `json:"sample_size"`
	MaxRowsScanned int   `json:"max_rows_scanned"`
	PageSize       int   `json:"page_size"`
	TimeoutMS      int64 `json:"timeout_ms"`
}

func (b frozenDataProfileBudget) executionBudget() (DataProfileBudget, error) {
	if b.SampleSize <= 0 || b.MaxRowsScanned < b.SampleSize || b.PageSize <= 0 ||
		b.PageSize > b.MaxRowsScanned || b.TimeoutMS <= 0 || b.TimeoutMS > (1<<63-1)/int64(time.Millisecond) {
		return DataProfileBudget{}, ErrDataProfileInvalidRequest
	}
	return DataProfileBudget{SampleSize: b.SampleSize, MaxRowsScanned: b.MaxRowsScanned,
		PageSize: b.PageSize, Timeout: time.Duration(b.TimeoutMS) * time.Millisecond}, nil
}

type frozenDataProfileConfig struct {
	Version           string                  `json:"config_version"`
	TargetKey         string                  `json:"target_key"`
	Locator           string                  `json:"locator"`
	Selection         DataProfileSelection    `json:"selection"`
	ItemID            *uint                   `json:"item_id"`
	ItemFingerprint   string                  `json:"item_fingerprint"`
	EngineID          uint                    `json:"engine_id"`
	SourceVersion     string                  `json:"source_version"`
	Mode              string                  `json:"profile_mode"`
	ProfileConfigHash string                  `json:"profile_config_hash"`
	DataScope         dataprofile.DataScope   `json:"data_scope"`
	SampleMethod      string                  `json:"sample_method"`
	Budget            frozenDataProfileBudget `json:"budget"`
	ReadSet           plugin.QueryReadSet     `json:"read_set"`
	Pages             []preview.TablePage     `json:"pages"`
}

type dataProfileStore interface {
	GetCurrent(context.Context, uint, string, string, string) (*models.DataProfile, *dataprofile.Profile, error)
	ReplaceCurrent(context.Context, *gorm.DB, *models.DataProfile, dataprofile.Profile) error
}

type dataProfileProtectionStore interface {
	managerprotection.LocalProjectionGate
	CaptureVersion(context.Context, int64, func(projectionstore.GateReader) error) (projectionstore.Version, error)
	CommitVersion(context.Context, int64, projectionstore.Version, func(*gorm.DB, projectionstore.GateReader) error) error
}

type dataProfileExecutionStore interface {
	CreateOrReuseActive(context.Context, string, *commonExecution.TaskExecution) (*commonExecution.TaskExecution, bool, error)
	GetActive(context.Context, int, string) (*commonExecution.TaskExecution, error)
	GetLatest(context.Context, int, string) (*commonExecution.TaskExecution, error)
	GetByExecutionID(context.Context, int, string) (*commonExecution.TaskExecution, error)
	Start(context.Context, int, string, time.Time) error
	Complete(context.Context, int, string, time.Time, int64, map[string]interface{}) error
	Fail(context.Context, int, string, time.Time, string, string) error
	Timeout(context.Context, int, string, time.Time, string, string) error
	GetRawExecutionConfig(context.Context, int, string) (json.RawMessage, error)
	BindSourceAuthorization(context.Context, *commonExecution.TaskExecution, commonExecution.ManagerProfileReadScope, *commonClient.IssuedManagerProfileAuthorization) error
	FailUnbound(context.Context, *commonExecution.TaskExecution) error
}

type DataProfileService struct {
	profiles              dataProfileStore
	executions            dataProfileExecutionStore
	sampler               DataProfileSampleProvider
	protectionGate        dataProfileProtectionStore
	budget                DataProfileBudget
	resultChecker         dataProfileResultReadChecker
	authorizationIssuer   dataProfileAuthorizationIssuer
	authorizationConsumer dataProfileAuthorizationConsumer
}

type dataProfileAuthorizationIssuer interface {
	IssueManagerProfile(context.Context, string, commonClient.IssueManagerProfileAuthorizationRequest) (*commonClient.IssuedManagerProfileAuthorization, error)
}

func (s *DataProfileService) SetAuthorizationIssuer(issuer dataProfileAuthorizationIssuer) {
	s.authorizationIssuer = issuer
}

type dataProfileSamplePlanner interface {
	Prepare(context.Context, *DataProfileTarget, dataprofile.DataScope, DataProfileBudget) (*DataProfileSamplePlan, error)
}

type dataProfileResultReadChecker interface {
	CheckManagerProfileResultRead(context.Context, string, []plugin.EngineCatalogPath) error
}

func (s *DataProfileService) SetResultReadChecker(checker dataProfileResultReadChecker) {
	s.resultChecker = checker
}

type DataProfileCurrentRequest struct {
	Locator           string `form:"locator" json:"locator"`
	ProfileConfigHash string `form:"profile_config_hash" json:"profile_config_hash,omitempty"`
	DataProfileSelection
}

type DataProfileExecutionRequest struct {
	Locator   string                `json:"locator"`
	Mode      string                `json:"mode,omitempty"`
	DataScope dataprofile.DataScope `json:"data_scope,omitempty"`
	DataProfileSelection
}

type DataProfileExecutionView struct {
	ExecutionID string     `json:"execution_id"`
	Status      string     `json:"status"`
	Progress    int        `json:"progress"`
	CreatedAt   time.Time  `json:"created_at"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	// 稳定执行错误码；protection_version_changed 表示规则已变化，source_authorization_required 表示缺少独立执行源授权、本次未读取源数据 | Stable execution error code; protection_version_changed means rules changed; source_authorization_required means execution source authorization is missing and no source rows were read.
	ErrorCode string `json:"error_code,omitempty"`
	Error     string `json:"error,omitempty"`
}

type DataProfileCurrentResponse struct {
	Supported           bool                      `json:"supported"`
	Profile             *dataprofile.Profile      `json:"profile,omitempty"`
	ResultID            *uint                     `json:"result_id,omitempty"`
	ItemFingerprint     string                    `json:"item_fingerprint"`
	SourceVersion       string                    `json:"source_version"`
	StoredSourceVersion string                    `json:"stored_source_version,omitempty"`
	ProfileConfigHash   string                    `json:"profile_config_hash"`
	Stale               bool                      `json:"stale"`
	StaleReason         string                    `json:"stale_reason,omitempty"`
	ActiveExecution     *DataProfileExecutionView `json:"active_execution,omitempty"`
	LatestExecution     *DataProfileExecutionView `json:"latest_execution,omitempty"`
	ProfileExecution    *DataProfileExecutionView `json:"profile_execution,omitempty"`
	ConditionSupported  bool                      `json:"condition_supported"`
}

type DataProfileExecutionResponse struct {
	Execution         *DataProfileExecutionView `json:"execution"`
	Reused            bool                      `json:"reused"`
	ProfileConfigHash string                    `json:"profile_config_hash"`
	DataScope         dataprofile.DataScope     `json:"data_scope"`
}

func NewDataProfileService(
	profiles dataProfileStore,
	executions dataProfileExecutionStore,
	sampler DataProfileSampleProvider,
	protectionGate dataProfileProtectionStore,
) *DataProfileService {
	return &DataProfileService{
		profiles:       profiles,
		executions:     executions,
		sampler:        sampler,
		protectionGate: protectionGate,
		budget:         DefaultDataProfileBudget,
	}
}

func (s *DataProfileService) GetCurrent(
	ctx context.Context,
	authContext authorization.AuthContext,
	credential string,
	req DataProfileCurrentRequest,
) (*DataProfileCurrentResponse, error) {
	if s == nil || s.profiles == nil || s.executions == nil || s.sampler == nil || s.protectionGate == nil {
		return nil, ErrDataProfileUnavailable
	}
	actor, err := profileActorFromAuthContext(authContext)
	if err != nil {
		return nil, err
	}
	tenantID := actor.TenantID
	target, err := s.sampler.ResolveTarget(ctx, tenantID, req.Locator, req.DataProfileSelection)
	if err != nil {
		return nil, err
	}
	profileRules, managed, err := s.profileRules(tenantID, target)
	if err != nil {
		return nil, err
	}
	configHash := strings.TrimSpace(req.ProfileConfigHash)
	if configHash == "" {
		configHash = dataProfileConfigHash(target.Selection, dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll}, s.budget)
	} else if !validProfileConfigHash(configHash) {
		return nil, fmt.Errorf("%w: invalid profile_config_hash", ErrDataProfileInvalidRequest)
	}
	state, profile, err := s.profiles.GetCurrent(ctx, tenantID, target.ItemFingerprint, dataprofile.ModeSample, configHash)
	if err != nil {
		return nil, err
	}
	// Derived values require the ACTUAL historical read set. Current target
	// resolution cannot prove which dependencies generated a stored result.
	if state != nil || profile != nil {
		readSet, err := profileResultReadSet(state)
		if err != nil || profile == nil || s.resultChecker == nil {
			return nil, ErrDataProfileSourceAuthorizationRequired
		}
		if err := s.resultChecker.CheckManagerProfileResultRead(ctx, credential, readSet.Paths); err != nil {
			return nil, err
		}
	}
	if managed && profile != nil && profile.DataScope.Kind == dataprofile.DataScopeKindCondition {
		return nil, ErrDataProfileProtectionRequired
	}
	profile, err = managerprotection.ProtectProfile(profile, profileRules)
	if err != nil {
		return nil, ErrDataProfileProtectionRequired
	}
	targetKey := profileTargetKey(actor, target.Locator, target.Selection, configHash)
	active, err := s.executions.GetActive(ctx, int(tenantID), targetKey)
	if err != nil {
		return nil, err
	}
	latest, err := s.executions.GetLatest(ctx, int(tenantID), targetKey)
	if err != nil {
		return nil, err
	}
	response := &DataProfileCurrentResponse{
		Supported:          true,
		Profile:            profile,
		ItemFingerprint:    target.ItemFingerprint,
		SourceVersion:      target.SourceVersion,
		ProfileConfigHash:  configHash,
		ActiveExecution:    dataProfileExecutionView(active),
		LatestExecution:    dataProfileExecutionView(latest),
		ConditionSupported: target.ConditionSupported && !managed,
	}
	if state != nil {
		profileExecution, err := s.executions.GetByExecutionID(ctx, int(tenantID), state.LastExecutionID)
		if err != nil {
			return nil, err
		}
		response.ResultID = &state.ID
		response.StoredSourceVersion = state.SourceVersion
		response.ProfileExecution = dataProfileExecutionView(profileExecution)
		if state.SourceVersion != target.SourceVersion {
			response.Stale = true
			response.StaleReason = "source_changed"
		}
	}
	return response, nil
}

func profileResultReadSet(state *models.DataProfile) (*plugin.QueryReadSet, error) {
	if state == nil || state.EngineID == 0 || strings.TrimSpace(state.LastExecutionID) == "" || strings.TrimSpace(state.LastExecutionID) != state.LastExecutionID {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	var snapshot map[string]json.RawMessage
	if err := json.Unmarshal(state.DependencySnapshot, &snapshot); err != nil {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	var readSet plugin.QueryReadSet
	decoder := json.NewDecoder(bytes.NewReader(snapshot["read_set"]))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&readSet); err != nil {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	return canonicalProfileReadSet(&readSet, state.EngineID)
}

func canonicalProfileReadSet(readSet *plugin.QueryReadSet, engineID uint) (*plugin.QueryReadSet, error) {
	if engineID == 0 || readSet == nil || len(readSet.Paths) == 0 || len(readSet.Paths) > 200 {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	canonical, err := plugin.NewQueryReadSet(readSet.Paths...)
	if err != nil || !reflect.DeepEqual(readSet, canonical) {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	for _, path := range readSet.Paths {
		if path.EngineID != engineID {
			return nil, ErrDataProfileSourceAuthorizationRequired
		}
		if _, err := authorization.EncodeSharingTarget(path); err != nil {
			return nil, ErrDataProfileSourceAuthorizationRequired
		}
	}
	return canonical, nil
}

func (s *DataProfileService) CreateExecution(
	ctx context.Context,
	authContext authorization.AuthContext,
	credential string,
	req DataProfileExecutionRequest,
) (*DataProfileExecutionResponse, error) {
	if s == nil || s.executions == nil || s.sampler == nil || s.protectionGate == nil {
		return nil, ErrDataProfileUnavailable
	}
	actor, err := profileActorFromAuthContext(authContext)
	if err != nil {
		return nil, err
	}
	tenantID := actor.TenantID
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
	// Do not persist a rounded timeout or silently substitute worker defaults.
	if _, err := (frozenDataProfileBudget{s.budget.SampleSize, s.budget.MaxRowsScanned, s.budget.PageSize, s.budget.Timeout.Milliseconds()}).executionBudget(); err != nil || s.budget.Timeout%time.Millisecond != 0 {
		return nil, ErrDataProfileInvalidRequest
	}
	if mode == "" {
		mode = dataprofile.ModeSample
	}
	if mode != dataprofile.ModeSample {
		return nil, fmt.Errorf("%w: only sample profiling mode is currently available", ErrDataProfileInvalidRequest)
	}
	target, err := s.sampler.ResolveTarget(ctx, tenantID, req.Locator, req.DataProfileSelection)
	if err != nil {
		return nil, err
	}
	_, managed, err := s.profileRules(tenantID, target)
	if err != nil {
		return nil, err
	}
	dataScope, err := profilefilter.Normalize(req.DataScope, target.Fields)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrDataProfileInvalidRequest, err)
	}
	if dataScope.Kind == dataprofile.DataScopeKindCondition && !target.ConditionSupported {
		return nil, fmt.Errorf("%w: conditional profiling is not supported", ErrDataProfileUnsupported)
	}
	if managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
		return nil, ErrDataProfileProtectionRequired
	}
	if s.authorizationIssuer == nil {
		return nil, ErrDataProfileUnavailable
	}
	if !strings.HasPrefix(credential, "addp_at_") || len(credential) <= len("addp_at_") || strings.ContainsAny(credential, " \t\r\n") {
		return nil, ErrDataProfileActorRequired
	}
	planner, ok := s.sampler.(dataProfileSamplePlanner)
	if !ok {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	plan, err := planner.Prepare(ctx, target, dataScope, s.budget)
	if err != nil {
		return nil, err
	}
	readSet, err := canonicalProfileReadSet(plan.ReadSet(), target.EngineID)
	if err != nil || len(plan.Positions()) == 0 {
		return nil, ErrDataProfileSourceAuthorizationRequired
	}
	configHash := dataProfileConfigHash(target.Selection, dataScope, s.budget)
	targetKey := profileTargetKey(actor, target.Locator, target.Selection, configHash)
	now := time.Now().UTC()
	executionID := uuid.NewString()
	frozen := frozenDataProfileConfig{
		Version: dataProfileConfigVersion, TargetKey: targetKey, Locator: target.Locator,
		Selection: target.Selection, ItemID: target.ItemID, ItemFingerprint: target.ItemFingerprint,
		EngineID: target.EngineID, SourceVersion: target.SourceVersion, Mode: mode,
		ProfileConfigHash: configHash, DataScope: dataScope, SampleMethod: sampleMethodForScope(dataScope),
		Budget:  frozenDataProfileBudget{s.budget.SampleSize, s.budget.MaxRowsScanned, s.budget.PageSize, s.budget.Timeout.Milliseconds()},
		ReadSet: *readSet, Pages: plan.Positions(),
	}
	// The producer and consumer share one schema. Freeze nested condition values
	// and resource references before passing configuration to the queue store.
	frozenJSON, err := json.Marshal(frozen)
	if err != nil {
		return nil, fmt.Errorf("encode data profile config: %w", err)
	}
	var executionConfig commonModels.JSONMap
	freezeDecoder := json.NewDecoder(bytes.NewReader(frozenJSON))
	freezeDecoder.UseNumber()
	if err := freezeDecoder.Decode(&executionConfig); err != nil {
		return nil, fmt.Errorf("freeze data profile config: %w", err)
	}
	triggeredBy := int(actor.PrincipalID)
	execution := &commonExecution.TaskExecution{
		TenantID:                   int(tenantID),
		ExecutionID:                executionID,
		Module:                     commonExecution.ModuleManager,
		TaskType:                   commonExecution.TaskTypeDataProfiling,
		Source:                     commonExecution.ModuleManager,
		ExecutionBoundary:          commonExecution.ExecutionBoundaryBounded,
		Status:                     commonExecution.ExecutionStatusPending,
		Progress:                   0,
		TriggerType:                commonExecution.TriggerTypeManual,
		TriggeredBy:                &triggeredBy,
		ActorPrincipalID:           &actor.PrincipalID,
		ActorTenantMembershipID:    &actor.TenantMembershipID,
		IssuedAuthorizationVersion: &actor.AuthorizationVersion,
		ExecutionConfig:            executionConfig,
		CreatedAt:                  now,
		UpdatedAt:                  now,
	}
	stored, created, err := s.executions.CreateOrReuseActive(ctx, targetKey, execution)
	if err != nil {
		return nil, err
	}
	if stored == nil {
		return nil, ErrDataProfileUnavailable
	}
	if created {
		if err := s.issueProfileAuthorization(ctx, credential, stored, readSet); err != nil {
			return nil, err
		}
	} else if stored == nil || stored.ExecutionAuthorizationID == nil || stored.AuthorizationExpiresAt == nil {
		return nil, ErrDataProfileUnavailable
	}
	return &DataProfileExecutionResponse{
		Execution:         dataProfileExecutionView(stored),
		Reused:            !created,
		ProfileConfigHash: configHash,
		DataScope:         dataScope,
	}, nil
}

var (
	ErrDataProfileActorRequired = errors.New("data profile requires current tenant user authorization provenance")
	ErrDataProfileActorExpired  = errors.New("data profile user authentication has expired")
)

// This is provenance, not an Allow decision. System must independently authorize
// execution source reads before production sampling can be restored.
type dataProfileActor struct {
	TenantID             uint  `json:"tenant_id"`
	PrincipalID          int64 `json:"principal_id"`
	TenantMembershipID   int64 `json:"tenant_membership_id"`
	AuthorizationVersion int64 `json:"authorization_version"`
}

func profileActorFromAuthContext(value authorization.AuthContext) (dataProfileActor, error) {
	if authorization.ValidateAuthContext(value) != nil || value.Principal.Type != "user" ||
		value.Context.Type != "tenant" || value.Context.TenantID == nil || value.Context.TenantMembershipID == nil ||
		value.Delegation != nil || (value.Token.Type != "first_party_access_token" && value.Token.Type != "oauth_access_token") {
		return dataProfileActor{}, ErrDataProfileActorRequired
	}
	if !value.Token.ExpiresAt.After(time.Now().UTC()) {
		return dataProfileActor{}, ErrDataProfileActorExpired
	}
	apiAudience := false
	for _, audience := range value.Client.Audiences {
		if audience == "addp.api" {
			apiAudience = true
		}
	}
	if !apiAudience {
		return dataProfileActor{}, ErrDataProfileActorRequired
	}
	// Shared AuthContext validation has already proved canonical positive int64 IDs.
	tenantID, _ := strconv.ParseInt(*value.Context.TenantID, 10, 64)
	principalID, _ := strconv.ParseInt(value.Principal.ID, 10, 64)
	membershipID, _ := strconv.ParseInt(*value.Context.TenantMembershipID, 10, 64)
	version, _ := strconv.ParseInt(value.Authorization.AuthorizationVersion, 10, 64)
	return dataProfileActor{uint(tenantID), principalID, membershipID, version}, nil
}

func (s *DataProfileService) runExecution(
	parent context.Context,
	target *DataProfileTarget,
	dataScope dataprofile.DataScope,
	configHash string,
	execution *commonExecution.TaskExecution,
	budget DataProfileBudget,
	plan *DataProfileSamplePlan,
	checkSource func(context.Context) error,
) {
	startedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(parent, budget.Timeout)
	defer cancel()
	if err := s.executions.Start(ctx, execution.TenantID, execution.ExecutionID, startedAt); err != nil {
		logger.L().Error("启动数据剖析 execution 失败", "execution_id", execution.ExecutionID, "error", err)
		return
	}
	fail := func(code string, err error) {
		logger.L().Error("数据剖析 execution 失败", "execution_id", execution.ExecutionID, "code", code)
		finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
		defer finishCancel()
		var updateErr error
		if code == "timeout" {
			updateErr = s.executions.Timeout(finishCtx, execution.TenantID, execution.ExecutionID, startedAt, code, "data profiling execution timed out")
		} else {
			updateErr = s.executions.Fail(finishCtx, execution.TenantID, execution.ExecutionID, startedAt, code, "data profiling execution failed")
		}
		if updateErr != nil {
			logger.L().Error("更新数据剖析失败状态失败", "execution_id", execution.ExecutionID, "error", updateErr)
		}
	}
	if plan == nil || checkSource == nil {
		fail("source_authorization_required", ErrDataProfileSourceAuthorizationRequired)
		return
	}
	version, err := s.protectionGate.CaptureVersion(ctx, int64(execution.TenantID), func(gate projectionstore.GateReader) error {
		_, managed, err := profilePlanRules(gate, uint(execution.TenantID), target, plan)
		if err != nil {
			return err
		}
		if managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
			return ErrDataProfileProtectionRequired
		}
		return nil
	})
	if err != nil {
		fail("security_protection_required", err)
		return
	}
	beforeRead := func(ctx context.Context) error {
		if err := checkSource(ctx); err != nil {
			return err
		}
		// This transaction only observes local protection. It never performs
		// remote I/O or holds the checkpoint while reading business content.
		return s.protectionGate.CommitVersion(ctx, int64(execution.TenantID), version, func(_ *gorm.DB, gate projectionstore.GateReader) error {
			_, managed, err := profilePlanRules(gate, uint(execution.TenantID), target, plan)
			if err != nil || managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
				return ErrDataProfileProtectionRequired
			}
			return ctx.Err()
		})
	}
	sample, err := s.sampler.Sample(ctx, target, dataScope, budget, plan, beforeRead)
	if err != nil {
		if errors.Is(err, ErrDataProfileSourceAuthorizationRequired) {
			fail("source_authorization_required", err)
			return
		}
		if errors.Is(err, projectionstore.ErrVersionChanged) {
			fail("protection_version_changed", err)
			return
		}
		if errors.Is(err, ErrDataProfileProtectionRequired) {
			fail("security_protection_required", err)
			return
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail("timeout", err)
			return
		}
		fail("sample_failed", err)
		return
	}
	if sample == nil {
		fail("source_authorization_required", ErrDataProfileSourceAuthorizationRequired)
		return
	}
	readSet, err := canonicalProfileReadSet(sample.ReadSet, target.EngineID)
	if err != nil || !reflect.DeepEqual(readSet, plan.ReadSet()) {
		fail("source_authorization_required", err)
		return
	}
	profile := dataprofile.Build(sample.Rows, sample.Fields, dataprofile.BuildOptions{
		Mode:          dataprofile.ModeSample,
		DataScope:     dataScope,
		SampleMethod:  sampleMethodForScope(dataScope),
		RowsScanned:   sample.RowsScanned,
		RowCount:      sample.RowCount,
		RowCountExact: sample.RowCountExact,
		Truncated:     sample.Truncated,
		Partial:       sample.Partial,
		TopN:          10,
		HistogramBins: 10,
		ProfiledAt:    time.Now().UTC(),
	})
	// Clone the source metadata; never mutate shared target facts or replace
	// actual sampling sources with the current target's dependencies.
	snapshot := make(map[string]interface{}, len(target.DependencySnapshot)+1)
	for key, value := range target.DependencySnapshot {
		snapshot[key] = value
	}
	snapshot["read_set"] = readSet
	dependencySnapshot, err := json.Marshal(snapshot)
	if err != nil {
		fail("result_encode_failed", err)
		return
	}
	state := &models.DataProfile{
		TenantID:           uint(execution.TenantID),
		ItemFingerprint:    target.ItemFingerprint,
		ItemID:             target.ItemID,
		EngineID:           target.EngineID,
		Locator:            target.Locator,
		SourceVersion:      target.SourceVersion,
		DependencySnapshot: dependencySnapshot,
		ProfileMode:        dataprofile.ModeSample,
		ProfileConfigHash:  configHash,
		LastExecutionID:    execution.ExecutionID,
	}
	// System takes a shared lock on the execution. Consume before acquiring
	// Manager's result/lease update locks, not from inside the write callback.
	if err := checkSource(ctx); err != nil {
		code := "source_authorization_required"
		if errors.Is(ctx.Err(), context.DeadlineExceeded) {
			code = "timeout"
		}
		fail(code, err)
		return
	}
	err = s.protectionGate.CommitVersion(ctx, int64(execution.TenantID), version, func(tx *gorm.DB, gate projectionstore.GateReader) error {
		rules, managed, err := profilePlanRules(gate, uint(execution.TenantID), target, plan)
		if err != nil {
			return err
		}
		if managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
			return ErrDataProfileProtectionRequired
		}
		protected, err := managerprotection.ProtectProfile(&profile, rules)
		if err != nil {
			return ErrDataProfileProtectionRequired
		}
		profile = *protected
		return s.profiles.ReplaceCurrent(ctx, tx, state, profile)
	})
	if err != nil {
		code := "result_store_failed"
		if errors.Is(err, projectionstore.ErrVersionChanged) {
			code = "protection_version_changed"
		}
		if errors.Is(err, ErrDataProfileProtectionRequired) {
			code = "security_protection_required"
		}
		fail(code, err)
		return
	}
	metadata := managerExecutionLineage(commonModels.JSONMap{
		"result_id":      state.ID,
		"sample_size":    profile.SampleSize,
		"rows_scanned":   profile.RowsScanned,
		"field_count":    profile.FieldCount,
		"source_version": target.SourceVersion,
	}, commonExecution.TaskTypeDataProfiling, []commonExecution.LineageResourceRef{
		managerItemLineageRefWithID(target.Locator, target.ItemFingerprint, target.ItemID),
	}, nil)
	finishCtx, finishCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer finishCancel()
	if err := s.executions.Complete(finishCtx, execution.TenantID, execution.ExecutionID, startedAt, sample.RowsScanned, metadata); err != nil {
		logger.L().Error("更新数据剖析成功状态失败", "execution_id", execution.ExecutionID, "error", err)
	}
}

func (s *DataProfileService) runClaimedExecution(ctx context.Context, execution *commonExecution.TaskExecution) error {
	if s == nil || s.sampler == nil || execution == nil || execution.TenantID <= 0 {
		return ErrDataProfileUnavailable
	}
	if err := validateProfileClaim(ctx, execution); err != nil {
		return err
	}
	payload, err := s.executions.GetRawExecutionConfig(ctx, execution.TenantID, execution.ExecutionID)
	if err != nil {
		return fmt.Errorf("encode frozen data profile config: %w", err)
	}
	var frozen frozenDataProfileConfig
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	decoder.UseNumber()
	if err := decoder.Decode(&frozen); err != nil {
		return fmt.Errorf("%w: decode frozen data profile config: %v", ErrDataProfileInvalidRequest, err)
	}
	budget, err := frozen.Budget.executionBudget()
	if execution.ActorPrincipalID == nil || *execution.ActorPrincipalID <= 0 ||
		execution.ActorTenantMembershipID == nil || *execution.ActorTenantMembershipID <= 0 ||
		execution.IssuedAuthorizationVersion == nil || *execution.IssuedAuthorizationVersion <= 0 {
		return ErrDataProfileActorRequired
	}
	actor := dataProfileActor{uint(execution.TenantID), *execution.ActorPrincipalID,
		*execution.ActorTenantMembershipID, *execution.IssuedAuthorizationVersion}
	if err != nil || frozen.Version != dataProfileConfigVersion || frozen.Mode != dataprofile.ModeSample ||
		frozen.EngineID == 0 || frozen.ItemFingerprint == "" || frozen.SourceVersion == "" || frozen.Locator == "" ||
		frozen.Selection != normalizeDataProfileSelection(frozen.Selection) ||
		frozen.SampleMethod != sampleMethodForScope(frozen.DataScope) ||
		frozen.ProfileConfigHash != dataProfileConfigHash(frozen.Selection, frozen.DataScope, budget) ||
		frozen.TargetKey != profileTargetKey(actor, frozen.Locator, frozen.Selection, frozen.ProfileConfigHash) {
		return ErrDataProfileInvalidRequest
	}
	frozenScope, err := commonExecution.NewManagerProfileReadScope(payload, &frozen.ReadSet)
	if err != nil || frozenScope.ReadSet.Paths[0].EngineID != frozen.EngineID {
		return ErrDataProfileSourceAuthorizationRequired
	}
	ctx, cancel := context.WithTimeout(ctx, budget.Timeout)
	defer cancel()
	target, err := s.sampler.ResolveTarget(ctx, uint(execution.TenantID), frozen.Locator, frozen.Selection)
	if err != nil {
		return err
	}
	if target == nil || target.EngineID != frozen.EngineID || target.Locator != frozen.Locator ||
		target.Selection != frozen.Selection || target.ItemFingerprint != frozen.ItemFingerprint || target.SourceVersion != frozen.SourceVersion ||
		!reflect.DeepEqual(target.ItemID, frozen.ItemID) {
		return ErrDataProfileSourceChanged
	}
	normalizedScope, err := profilefilter.Normalize(frozen.DataScope, target.Fields)
	if err != nil || dataProfileConfigHash(frozen.Selection, normalizedScope, budget) != frozen.ProfileConfigHash {
		return ErrDataProfileInvalidRequest
	}
	if normalizedScope.Kind == dataprofile.DataScopeKindCondition && !target.ConditionSupported {
		return ErrDataProfileUnsupported
	}
	planner, ok := s.sampler.(dataProfileSamplePlanner)
	if !ok {
		return ErrDataProfileSourceAuthorizationRequired
	}
	plan, err := planner.Prepare(ctx, target, normalizedScope, budget)
	if err != nil {
		return err
	}
	if err := s.consumeProfileAuthorization(ctx, execution, payload, &frozen, plan); err != nil {
		return err
	}
	checkSource := func(ctx context.Context) error {
		return s.consumeProfileAuthorization(ctx, execution, payload, &frozen, plan)
	}
	s.runExecution(ctx, target, normalizedScope, frozen.ProfileConfigHash, execution, budget, plan, checkSource)
	return nil
}

// Non-selected protected dependencies need an aggregate output mapping before
// they can be sampled. Never treat an unmanaged view as an unmanaged read set.
func profilePlanRules(reader projectionstore.GateReader, tenantID uint, target *DataProfileTarget, plan *DataProfileSamplePlan) ([]dataprotection.Rule, bool, error) {
	if reader == nil || target == nil || plan == nil {
		return nil, false, ErrDataProfileProtectionRequired
	}
	sources, err := dataprotection.DataItemTargetsFromQueryReadSet(plan.model, plan.ReadSet())
	if err != nil || len(sources) == 0 {
		return nil, false, ErrDataProfileProtectionRequired
	}
	for _, source := range sources {
		if source.ResourceIdentity != target.ItemFingerprint && reader.Gate(int64(tenantID), source, time.Now().UTC()).Managed {
			return nil, true, ErrDataProfileProtectionRequired
		}
	}
	return profileRulesForGate(reader, tenantID, target)
}

// profileRules resolves the single local protection path for profiling. An
// unmanaged DataItem returns no rules and keeps the original execution path.
func (s *DataProfileService) profileRules(tenantID uint, target *DataProfileTarget) ([]dataprotection.Rule, bool, error) {
	if s == nil {
		return nil, false, ErrDataProfileUnavailable
	}
	return profileRulesForGate(s.protectionGate, tenantID, target)
}

func profileRulesForGate(reader projectionstore.GateReader, tenantID uint, target *DataProfileTarget) ([]dataprotection.Rule, bool, error) {
	if reader == nil || target == nil {
		return nil, false, ErrDataProfileUnavailable
	}
	gate := managerprotection.DataItemGate(reader, tenantID, target.ItemFingerprint, time.Now().UTC())
	rules, err := managerprotection.TableRules(target.ItemFingerprint, target.Fields, gate, managerprotection.ActionProfile, time.Now().UTC())
	if err != nil {
		return nil, gate.Managed, ErrDataProfileProtectionRequired
	}
	if err := managerprotection.ValidateProfileRules(rules); err != nil {
		return nil, gate.Managed, ErrDataProfileProtectionRequired
	}
	return rules, gate.Managed, nil
}

func dataProfileConfigHash(selection DataProfileSelection, dataScope dataprofile.DataScope, budget DataProfileBudget) string {
	payload, _ := json.Marshal(struct {
		Version        string                `json:"version"`
		Mode           string                `json:"mode"`
		Selection      DataProfileSelection  `json:"selection"`
		DataScope      dataprofile.DataScope `json:"data_scope"`
		SampleSize     int                   `json:"sample_size"`
		MaxRowsScanned int                   `json:"max_rows_scanned"`
		PageSize       int                   `json:"page_size"`
		TimeoutMS      int64                 `json:"timeout_ms"`
		TopN           int                   `json:"top_n"`
		HistogramBins  int                   `json:"histogram_bins"`
	}{
		Version:        dataProfileConfigVersion,
		Mode:           dataprofile.ModeSample,
		Selection:      normalizeDataProfileSelection(selection),
		DataScope:      dataScope,
		SampleSize:     budget.SampleSize,
		MaxRowsScanned: budget.MaxRowsScanned,
		PageSize:       budget.PageSize,
		TimeoutMS:      budget.Timeout.Milliseconds(),
		TopN:           10,
		HistogramBins:  10,
	})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
}

func validProfileConfigHash(value string) bool {
	if len(value) != sha256.Size*2 {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func sampleMethodForScope(scope dataprofile.DataScope) string {
	if scope.Kind == dataprofile.DataScopeKindCondition {
		return "filtered_bounded_reservoir"
	}
	return "systematic_pages_reservoir"
}

func dataProfileExecutionView(execution *commonExecution.TaskExecution) *DataProfileExecutionView {
	if execution == nil {
		return nil
	}
	view := &DataProfileExecutionView{
		ExecutionID: execution.ExecutionID,
		Status:      execution.Status,
		Progress:    execution.Progress,
		CreatedAt:   execution.CreatedAt,
		StartedAt:   execution.StartedAt,
		CompletedAt: execution.CompletedAt,
	}
	if execution.ErrorDetails != nil {
		view.ErrorCode = strings.TrimSpace(fmt.Sprint(execution.ErrorDetails["code"]))
		view.Error = strings.TrimSpace(fmt.Sprint(execution.ErrorDetails["message"]))
	}
	return view
}
