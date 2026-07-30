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

	"github.com/addp/common/dataprofile"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/models"
	"github.com/google/uuid"
)

const (
	dataProfileConfigVersion      = "data-profile-config/v2"
	defaultDataProfileConcurrency = 4
)

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
	budget         DataProfileBudget
	executionSlots chan struct{}
}

type DataProfileCurrentRequest struct {
	Locator string `form:"locator" json:"locator"`
	DataProfileSelection
}

type DataProfileExecutionRequest struct {
	Locator string `json:"locator"`
	Mode    string `json:"mode,omitempty"`
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
}

type DataProfileExecutionResponse struct {
	Execution *DataProfileExecutionView `json:"execution"`
	Reused    bool                      `json:"reused"`
}

func NewDataProfileService(
	profiles dataProfileStore,
	executions dataProfileExecutionStore,
	sampler DataProfileSampleProvider,
) *DataProfileService {
	return &DataProfileService{
		profiles:       profiles,
		executions:     executions,
		sampler:        sampler,
		budget:         DefaultDataProfileBudget,
		executionSlots: make(chan struct{}, defaultDataProfileConcurrency),
	}
}

func (s *DataProfileService) GetCurrent(
	ctx context.Context,
	tenantID uint,
	req DataProfileCurrentRequest,
) (*DataProfileCurrentResponse, error) {
	if s == nil || s.profiles == nil || s.executions == nil || s.sampler == nil {
		return nil, ErrDataProfileUnavailable
	}
	target, err := s.sampler.ResolveTarget(ctx, tenantID, req.Locator, req.DataProfileSelection)
	if err != nil {
		return nil, err
	}
	configHash := dataProfileConfigHash(target.Selection, s.budget)
	state, profile, err := s.profiles.GetCurrent(ctx, tenantID, target.ItemFingerprint, dataprofile.ModeSample, configHash)
	if err != nil {
		return nil, err
	}
	targetKey := profileTargetKey(tenantID, target.Locator, target.Selection)
	active, err := s.executions.GetActive(ctx, int(tenantID), targetKey)
	if err != nil {
		return nil, err
	}
	latest, err := s.executions.GetLatest(ctx, int(tenantID), targetKey)
	if err != nil {
		return nil, err
	}
	response := &DataProfileCurrentResponse{
		Supported:         true,
		Profile:           profile,
		ItemFingerprint:   target.ItemFingerprint,
		SourceVersion:     target.SourceVersion,
		ProfileConfigHash: configHash,
		ActiveExecution:   dataProfileExecutionView(active),
		LatestExecution:   dataProfileExecutionView(latest),
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
	if s == nil || s.executions == nil || s.sampler == nil {
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
	targetKey := profileTargetKey(tenantID, target.Locator, target.Selection)
	configHash := dataProfileConfigHash(target.Selection, s.budget)
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
		"sample_method":       "systematic_pages_reservoir",
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
	if created {
		go s.runExecution(target, configHash, stored)
	}
	return &DataProfileExecutionResponse{
		Execution: dataProfileExecutionView(stored),
		Reused:    !created,
	}, nil
}

func (s *DataProfileService) runExecution(
	target *DataProfileTarget,
	configHash string,
	execution *commonExecution.TaskExecution,
) {
	if s.executionSlots != nil {
		s.executionSlots <- struct{}{}
		defer func() { <-s.executionSlots }()
	}
	startedAt := time.Now().UTC()
	ctx, cancel := context.WithTimeout(context.Background(), s.budget.Timeout)
	defer cancel()
	if err := s.executions.Start(ctx, execution.TenantID, execution.ExecutionID, startedAt); err != nil {
		logger.L().Error("启动数据剖析 execution 失败", "execution_id", execution.ExecutionID, "error", err)
		return
	}
	fail := func(code string, err error) {
		logger.L().Error("数据剖析 execution 失败", "execution_id", execution.ExecutionID, "code", code, "error", err)
		var updateErr error
		if code == "timeout" {
			updateErr = s.executions.Timeout(context.Background(), execution.TenantID, execution.ExecutionID, startedAt, code, "data profiling execution timed out")
		} else {
			updateErr = s.executions.Fail(context.Background(), execution.TenantID, execution.ExecutionID, startedAt, code, "data profiling execution failed")
		}
		if updateErr != nil {
			logger.L().Error("更新数据剖析失败状态失败", "execution_id", execution.ExecutionID, "error", updateErr)
		}
	}
	sample, err := s.sampler.Sample(ctx, target, s.budget)
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
		SampleMethod:  "systematic_pages_reservoir",
		RowsScanned:   sample.RowsScanned,
		RowCount:      target.RowCount,
		RowCountExact: target.RowCountExact,
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
	if err := s.profiles.ReplaceCurrent(ctx, state, profile); err != nil {
		fail("result_store_failed", err)
		return
	}
	if err := s.executions.Complete(context.Background(), execution.TenantID, execution.ExecutionID, startedAt, sample.RowsScanned, map[string]interface{}{
		"result_id":      state.ID,
		"sample_size":    profile.SampleSize,
		"rows_scanned":   profile.RowsScanned,
		"field_count":    profile.FieldCount,
		"source_version": target.SourceVersion,
	}); err != nil {
		logger.L().Error("更新数据剖析成功状态失败", "execution_id", execution.ExecutionID, "error", err)
	}
}

func dataProfileConfigHash(selection DataProfileSelection, budget DataProfileBudget) string {
	payload, _ := json.Marshal(struct {
		Version        string               `json:"version"`
		Mode           string               `json:"mode"`
		Selection      DataProfileSelection `json:"selection"`
		SampleSize     int                  `json:"sample_size"`
		MaxRowsScanned int                  `json:"max_rows_scanned"`
		PageSize       int                  `json:"page_size"`
		TopN           int                  `json:"top_n"`
		HistogramBins  int                  `json:"histogram_bins"`
	}{
		Version:        dataProfileConfigVersion,
		Mode:           dataprofile.ModeSample,
		Selection:      normalizeDataProfileSelection(selection),
		SampleSize:     budget.SampleSize,
		MaxRowsScanned: budget.MaxRowsScanned,
		PageSize:       budget.PageSize,
		TopN:           10,
		HistogramBins:  10,
	})
	hash := sha256.Sum256(payload)
	return hex.EncodeToString(hash[:])
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
