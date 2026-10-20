package service

import (
	"context"
	"encoding/json"

	commonExecution "github.com/addp/common/execution"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
)

type model3DGLBClaimPlan struct {
	config        Model3DGLBExecutionConfig
	authorization *Model3DGLBSourceAuthorization
}

func (s *Model3DGLBTaskService) prepareClaim(ctx context.Context, task *models.Model3DGLBTask, execution *commonExecution.TaskExecution) (*model3DGLBClaimPlan, error) {
	raw, err := s.repo.RawExecutionConfig(ctx, execution.TenantID, execution.ExecutionID)
	if err != nil {
		return nil, err
	}
	engine, err := s.sourceEngineForSnapshot(ctx, &repository.Model3DGLBTaskSnapshot{TaskID: task.ID, TenantID: task.TenantID, Config: raw})
	if err != nil {
		return nil, err
	}
	var marker struct {
		Version string `json:"config_version"`
	}
	if json.Unmarshal(raw, &marker) != nil {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	if engine.EngineType != "ftp" {
		if marker.Version != "" || execution.ExecutionAuthorizationID != nil {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		task.Config = execution.ExecutionConfig.Clone()
		return nil, nil
	}
	config, err := decodeFTPModel3DGLBConversion(raw)
	if err != nil {
		return nil, err
	}
	if err := validateModel3DGLBExecutionOutput(task.TenantID, execution.ExecutionID, s.bucket, config.Result); err != nil {
		return nil, err
	}
	candidate, err := s.sourceCandidate(ctx, task.TenantID, config)
	if err != nil {
		return nil, err
	}
	authorization, err := newModel3DGLBSourceAuthorization(ctx, s.sourceConsumer, execution, raw, candidate)
	if err != nil {
		return nil, err
	}
	if err := authorization.Checkpoint(ctx); err != nil {
		return nil, err
	}
	return &model3DGLBClaimPlan{config: config, authorization: authorization}, nil
}

func (s *Model3DGLBTaskService) prepareClaimResult(ctx context.Context, task *models.Model3DGLBTask, executionID string, plan *model3DGLBClaimPlan) (*models.Model3DGLB, Model3DGLBExecutionConfig, error) {
	if plan == nil {
		return s.prepareResult(ctx, task, executionID)
	}
	cfg := plan.config
	if err := plan.authorization.ValidateConversion(cfg); err != nil {
		return nil, cfg, err
	}
	if err := plan.authorization.Checkpoint(ctx); err != nil {
		return nil, cfg, err
	}
	result, err := s.repo.PrepareExecutionOutput(ctx, &models.Model3DGLB{
		TenantID: task.TenantID, TaskID: &task.ID, LastExecutionID: &executionID,
		ItemFingerprint: cfg.Source.ItemFingerprint, ItemID: optionalUint(cfg.Source.ItemID),
		Locator: cfg.Source.ItemLocator, SourceEngineID: cfg.Source.SourceEngineID,
		SourceFormat: cfg.Source.Format, SourceSizeBytes: cfg.Source.SourceSizeBytes,
		FileName: cfg.Result.FileName, CreatedBy: task.CreatedBy,
	}, cfg.Result.StorageRef)
	return result, cfg, err
}
