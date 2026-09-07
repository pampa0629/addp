package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/addp/common/dataprotection"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/profilefilter"
	managerprotection "github.com/addp/manager/internal/protection"
	"github.com/google/uuid"
)

const dataProfileConfigVersion = "data-profile-config/v4"

type dataProfileStore interface {
	GetCurrent(context.Context, uint, string, string, string) (*models.DataProfile, *dataprofile.Profile, error)
	ReplaceCurrent(context.Context, *models.DataProfile, dataprofile.Profile) error
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
}

type DataProfileService struct {
	profiles       dataProfileStore
	executions     dataProfileExecutionStore
	sampler        DataProfileSampleProvider
	protectionGate managerprotection.LocalProjectionGate
	budget         DataProfileBudget
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
	ErrorCode   string     `json:"error_code,omitempty"`
	Error       string     `json:"error,omitempty"`
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
	protectionGate managerprotection.LocalProjectionGate,
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
	tenantID uint,
	req DataProfileCurrentRequest,
) (*DataProfileCurrentResponse, error) {
	if s == nil || s.profiles == nil || s.executions == nil || s.sampler == nil || s.protectionGate == nil {
		return nil, ErrDataProfileUnavailable
	}
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
	if managed && profile != nil && profile.DataScope.Kind == dataprofile.DataScopeKindCondition {
		return nil, ErrDataProfileProtectionRequired
	}
	profile, err = managerprotection.ProtectProfile(profile, profileRules)
	if err != nil {
		return nil, ErrDataProfileProtectionRequired
	}
	targetKey := profileTargetKey(tenantID, target.Locator, target.Selection, configHash)
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

func (s *DataProfileService) CreateExecution(
	ctx context.Context,
	tenantID uint,
	userID uint,
	req DataProfileExecutionRequest,
) (*DataProfileExecutionResponse, error) {
	if s == nil || s.executions == nil || s.sampler == nil || s.protectionGate == nil {
		return nil, ErrDataProfileUnavailable
	}
	mode := strings.ToLower(strings.TrimSpace(req.Mode))
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
	configHash := dataProfileConfigHash(target.Selection, dataScope, s.budget)
	targetKey := profileTargetKey(tenantID, target.Locator, target.Selection, configHash)
	now := time.Now().UTC()
	executionID := uuid.NewString()
	executionConfig := commonModels.JSONMap{
		"config_version":      dataProfileConfigVersion,
		"target_key":          targetKey,
		"locator":             target.Locator,
		"selection":           target.Selection,
		"item_id":             target.ItemID,
		"item_fingerprint":    target.ItemFingerprint,
		"engine_id":           target.EngineID,
		"source_version":      target.SourceVersion,
		"profile_mode":        mode,
		"profile_config_hash": configHash,
		"data_scope":          dataScope,
		"sample_method":       sampleMethodForScope(dataScope),
		"budget": map[string]interface{}{
			"sample_size":      s.budget.SampleSize,
			"max_rows_scanned": s.budget.MaxRowsScanned,
			"page_size":        s.budget.PageSize,
			"timeout_ms":       s.budget.Timeout.Milliseconds(),
		},
	}
	var triggeredBy *int
	if userID > 0 {
		value := int(userID)
		triggeredBy = &value
	}
	execution := &commonExecution.TaskExecution{
		TenantID:        int(tenantID),
		ExecutionID:     executionID,
		Module:          commonExecution.ModuleManager,
		TaskType:        commonExecution.TaskTypeDataProfiling,
		Source:          commonExecution.ModuleManager,
		Status:          commonExecution.ExecutionStatusPending,
		Progress:        0,
		TriggerType:     commonExecution.TriggerTypeManual,
		TriggeredBy:     triggeredBy,
		ExecutionConfig: executionConfig,
		CreatedAt:       now,
		UpdatedAt:       now,
	}
	stored, created, err := s.executions.CreateOrReuseActive(ctx, targetKey, execution)
	if err != nil {
		return nil, err
	}
	return &DataProfileExecutionResponse{
		Execution:         dataProfileExecutionView(stored),
		Reused:            !created,
		ProfileConfigHash: configHash,
		DataScope:         dataScope,
	}, nil
}

func (s *DataProfileService) runExecution(
	parent context.Context,
	target *DataProfileTarget,
	dataScope dataprofile.DataScope,
	configHash string,
	execution *commonExecution.TaskExecution,
) {
	startedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(parent, s.budget.Timeout)
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
	_, managed, err := s.profileRules(uint(execution.TenantID), target)
	if err != nil {
		fail("security_protection_required", err)
		return
	}
	if managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
		fail("security_protection_required", ErrDataProfileProtectionRequired)
		return
	}
	sample, err := s.sampler.Sample(ctx, target, dataScope, s.budget)
	if err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			fail("timeout", err)
			return
		}
		fail("sample_failed", err)
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
	dependencySnapshot, err := json.Marshal(target.DependencySnapshot)
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
	profileRules, managed, err := s.profileRules(uint(execution.TenantID), target)
	if err != nil {
		fail("security_protection_required", err)
		return
	}
	if managed && dataScope.Kind == dataprofile.DataScopeKindCondition {
		fail("security_protection_required", ErrDataProfileProtectionRequired)
		return
	}
	protectedProfile, err := managerprotection.ProtectProfile(&profile, profileRules)
	if err != nil {
		fail("security_protection_required", err)
		return
	}
	profile = *protectedProfile
	if err := s.profiles.ReplaceCurrent(ctx, state, profile); err != nil {
		fail("result_store_failed", err)
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
	if s == nil || execution == nil {
		return ErrDataProfileUnavailable
	}
	payload, err := json.Marshal(execution.ExecutionConfig)
	if err != nil {
		return fmt.Errorf("encode frozen data profile config: %w", err)
	}
	var frozen struct {
		Locator           string                `json:"locator"`
		Selection         DataProfileSelection  `json:"selection"`
		DataScope         dataprofile.DataScope `json:"data_scope"`
		ProfileConfigHash string                `json:"profile_config_hash"`
		ItemFingerprint   string                `json:"item_fingerprint"`
		SourceVersion     string                `json:"source_version"`
	}
	if err := json.Unmarshal(payload, &frozen); err != nil {
		return fmt.Errorf("decode frozen data profile config: %w", err)
	}
	target, err := s.sampler.ResolveTarget(ctx, uint(execution.TenantID), frozen.Locator, frozen.Selection)
	if err != nil {
		return err
	}
	if target.ItemFingerprint != frozen.ItemFingerprint || target.SourceVersion != frozen.SourceVersion {
		return ErrDataProfileSourceChanged
	}
	s.runExecution(ctx, target, frozen.DataScope, frozen.ProfileConfigHash, execution)
	return nil
}

// profileRules resolves the single local protection path for profiling. An
// unmanaged DataItem returns no rules and keeps the original execution path.
func (s *DataProfileService) profileRules(tenantID uint, target *DataProfileTarget) ([]dataprotection.Rule, bool, error) {
	if s == nil || s.protectionGate == nil || target == nil {
		return nil, false, ErrDataProfileUnavailable
	}
	gate := managerprotection.DataItemGate(s.protectionGate, tenantID, target.ItemFingerprint, time.Now().UTC())
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
