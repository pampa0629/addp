package service

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	commonModels "github.com/addp/common/models"
	"github.com/addp/transfer/internal/models"
	"github.com/addp/transfer/internal/planner"
)

const ReplayExecutionVersion = "transfer.bounded_replay/v1"

var ErrInvalidReplayRequest = errors.New("invalid bounded replay request")
var ErrReplayRuntimeUnavailable = errors.New("bounded replay runtime is unavailable")
var ErrReplayRangeUnavailable = planner.ErrReplayRangeUnavailable
var ErrReplayTargetExists = planner.ErrReplayTargetExists

type BoundedReplayRuntime interface {
	Prepare(ctx context.Context, plan *planner.ContinuousPlan, ranges []planner.ReplayOffsetRange, executionApplyIdentity string) ([]planner.ReplayRetentionSnapshot, error)
	Run(ctx context.Context, plan *planner.ContinuousPlan, ranges []planner.ReplayOffsetRange, executionApplyIdentity string, recordProgress func(context.Context, planner.ReplayProgress) error) (*planner.ReplayResult, error)
}

type ReplayExecutionRequest struct {
	Ranges []planner.ReplayOffsetRange `json:"ranges"`
	Target planner.ReplayTargetSpec    `json:"target"`
}

type ReplayExecutionPreparation struct {
	ExecutionConfig commonModels.JSONMap
	Metadata        commonModels.JSONMap
}

type replayExecutionConfig struct {
	TaskConfig map[string]interface{} `json:"task_config"`
	Replay     replayExecutionSpec    `json:"replay"`
}

type replayExecutionSpec struct {
	Version       string                      `json:"version"`
	Ranges        []planner.ReplayOffsetRange `json:"ranges"`
	Target        planner.ReplayTargetSpec    `json:"target"`
	ApplyIdentity string                      `json:"apply_identity"`
}

func (s *ExecutionEngineService) PrepareReplayExecution(
	ctx context.Context,
	taskConfig map[string]interface{},
	request ReplayExecutionRequest,
	executionApplyIdentity string,
) (*ReplayExecutionPreparation, error) {
	if s == nil || s.systemClient == nil || s.replayRuntime == nil {
		return nil, ErrReplayRuntimeUnavailable
	}
	if err := planner.ValidateReplayOffsetRanges(request.Ranges); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidReplayRequest, err)
	}
	spec, err := planner.ParseContinuousTaskSpec(taskConfig)
	if err != nil {
		return nil, fmt.Errorf("%w: owner task is not a business Kafka continuous task: %v", ErrInvalidReplayRequest, err)
	}
	plan, err := planner.BuildReplayContinuousPlan(spec, request.Target, planner.NewSystemEngineResolver(s.systemClient))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidReplayRequest, err)
	}
	snapshot, err := s.replayRuntime.Prepare(ctx, plan, request.Ranges, executionApplyIdentity)
	if err != nil {
		return nil, err
	}

	executionConfig, err := replayStructMap(replayExecutionConfig{
		TaskConfig: taskConfig,
		Replay: replayExecutionSpec{
			Version: ReplayExecutionVersion, Ranges: append([]planner.ReplayOffsetRange(nil), request.Ranges...),
			Target: request.Target, ApplyIdentity: executionApplyIdentity,
		},
	})
	if err != nil {
		return nil, err
	}
	positions := make(map[string]int64, len(request.Ranges))
	for _, replayRange := range request.Ranges {
		positions[strings.TrimSpace(replayRange.Partition)] = replayRange.StartOffset
	}
	metadata := commonModels.JSONMap{"replay": map[string]interface{}{
		"version": ReplayExecutionVersion, "ranges": request.Ranges, "retention_snapshot": snapshot,
		"target_identity": replayTargetIdentity(plan, request.Target), "apply_identity": executionApplyIdentity,
		"positions": positions, "records_read": int64(0), "records_written": int64(0), "status": "pending",
	}}
	return &ReplayExecutionPreparation{ExecutionConfig: executionConfig, Metadata: metadata}, nil
}

func (s *ExecutionEngineService) executeBoundedReplay(ctx context.Context, task *models.TransferTask, executionID uint, executionConfig commonModels.JSONMap) error {
	if s.systemClient == nil || s.replayRuntime == nil {
		return s.failReplayExecution(executionID, ErrReplayRuntimeUnavailable)
	}
	config, err := parseReplayExecutionConfig(executionConfig)
	if err != nil {
		return s.failReplayExecution(executionID, err)
	}
	if config.Replay.ApplyIdentity == task.ApplyIdentity {
		return s.failReplayExecution(executionID, fmt.Errorf("bounded replay apply identity must differ from owner task apply identity"))
	}
	if err := s.executionService.UpdateStatus(ctx, executionID, models.ExecutionStatusRunning); err != nil {
		return err
	}
	spec, err := planner.ParseContinuousTaskSpec(config.TaskConfig)
	if err != nil {
		return s.failReplayExecution(executionID, fmt.Errorf("parse bounded replay owner task snapshot: %w", err))
	}
	plan, err := planner.BuildReplayContinuousPlan(spec, config.Replay.Target, planner.NewSystemEngineResolver(s.systemClient))
	if err != nil {
		return s.failReplayExecution(executionID, fmt.Errorf("build bounded replay plan: %w", err))
	}
	recordProgress := func(progressCtx context.Context, progress planner.ReplayProgress) error {
		if err := s.executionService.UpdateMetrics(progressCtx, executionID, map[string]interface{}{
			"records_read": progress.RecordsRead, "records_written": progress.RecordsWritten,
		}); err != nil {
			return err
		}
		return s.updateReplayMetadata(progressCtx, executionID, map[string]interface{}{
			"positions": progress.Positions, "records_read": progress.RecordsRead,
			"records_written": progress.RecordsWritten, "status": "running", "updated_at": time.Now().Format(time.RFC3339),
		})
	}
	result, err := s.replayRuntime.Run(ctx, plan, config.Replay.Ranges, config.Replay.ApplyIdentity, recordProgress)
	if err != nil {
		return s.failReplayExecution(executionID, err)
	}
	if err := s.executionService.UpdateMetrics(ctx, executionID, map[string]interface{}{
		"records_read": result.RecordsRead, "records_written": result.RecordsWritten,
	}); err != nil {
		return s.failReplayExecution(executionID, err)
	}
	if err := s.updateReplayMetadata(ctx, executionID, map[string]interface{}{
		"positions": result.Positions, "records_read": result.RecordsRead, "records_written": result.RecordsWritten,
		"status": "success", "completed_at": time.Now().Format(time.RFC3339),
	}); err != nil {
		return s.failReplayExecution(executionID, err)
	}
	return s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusSuccess, "")
}

func (s *ExecutionEngineService) updateReplayMetadata(ctx context.Context, executionID uint, updates map[string]interface{}) error {
	execution, err := s.executionService.taskExecutionRepo.GetByID(ctx, int64(executionID), 0)
	if err != nil {
		return err
	}
	replayMetadata := map[string]interface{}{}
	if existing, ok := execution.Metadata["replay"].(map[string]interface{}); ok {
		for key, value := range existing {
			replayMetadata[key] = value
		}
	}
	for key, value := range updates {
		replayMetadata[key] = value
	}
	return s.executionService.UpdateExecution(ctx, executionID, map[string]interface{}{"metadata": map[string]interface{}{"replay": replayMetadata}})
}

func (s *ExecutionEngineService) failReplayExecution(executionID uint, executionErr error) error {
	if executionErr == nil {
		return nil
	}
	ctx := context.Background()
	if err := s.updateReplayMetadata(ctx, executionID, map[string]interface{}{
		"status": "failed", "completed_at": time.Now().Format(time.RFC3339),
	}); err != nil {
		s.logger.Error("failed to update bounded replay failure metadata", "error", err, "execution_id", executionID)
	}
	if err := s.executionService.FinishExecution(ctx, executionID, models.ExecutionStatusFailed, executionErr.Error()); err != nil {
		s.logger.Error("failed to mark bounded replay execution failed", "error", err, "execution_id", executionID)
	}
	return executionErr
}

func parseReplayExecutionConfig(raw commonModels.JSONMap) (*replayExecutionConfig, error) {
	payload, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("marshal bounded replay execution config: %w", err)
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var config replayExecutionConfig
	if err := decoder.Decode(&config); err != nil {
		return nil, fmt.Errorf("decode bounded replay execution config: %w", err)
	}
	if config.Replay.Version != ReplayExecutionVersion || strings.TrimSpace(config.Replay.ApplyIdentity) == "" {
		return nil, fmt.Errorf("unsupported bounded replay execution config")
	}
	if err := planner.ValidateReplayOffsetRanges(config.Replay.Ranges); err != nil {
		return nil, err
	}
	return &config, nil
}

func isReplayExecutionConfig(raw commonModels.JSONMap) bool {
	_, ok := raw["replay"]
	return ok
}

func replayStructMap(value interface{}) (commonModels.JSONMap, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("marshal bounded replay snapshot: %w", err)
	}
	var result commonModels.JSONMap
	if err := json.Unmarshal(payload, &result); err != nil {
		return nil, fmt.Errorf("decode bounded replay snapshot: %w", err)
	}
	return result, nil
}

func replayTargetIdentity(plan *planner.ContinuousPlan, target planner.ReplayTargetSpec) map[string]interface{} {
	return map[string]interface{}{
		"engine_id": plan.Target.Path.EngineID, "catalog_path": plan.Target.Path.StringPath(),
		"parent_locator": target.ParentLocator, "name": target.Name,
	}
}
