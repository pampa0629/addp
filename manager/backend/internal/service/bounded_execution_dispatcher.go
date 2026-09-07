package service

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
)

// BoundedExecutionDispatcher is the single Manager domain dispatch path used
// by the Backend-embedded supervisor. HTTP handlers only enqueue executions.
type BoundedExecutionDispatcher struct {
	tileCache              *TileCacheTaskService
	vectorTileSet          *VectorTileSetTaskService
	vectorMaterializedView *VectorMaterializedViewTaskService
	rasterCOG              *RasterCOGTaskService
	rasterMosaic           *RasterMosaicTaskService
	model3DGLB             *Model3DGLBTaskService
	model3DTiles           *Model3DTilesTaskService
	gaussianSplat          *GaussianSplatKSplatTaskService
	pointCloudCOPC         *PointCloudCOPCTaskService
	pptxPDF                *PPTXPDFTaskService
	embedding              *EmbeddingService
	embeddingTask          *EmbeddingTaskService
	dataProfile            *DataProfileService
}

func NewBoundedExecutionDispatcher(
	tileCache *TileCacheTaskService,
	vectorTileSet *VectorTileSetTaskService,
	vectorMaterializedView *VectorMaterializedViewTaskService,
	rasterCOG *RasterCOGTaskService,
	rasterMosaic *RasterMosaicTaskService,
	model3DGLB *Model3DGLBTaskService,
	model3DTiles *Model3DTilesTaskService,
	gaussianSplat *GaussianSplatKSplatTaskService,
	pointCloudCOPC *PointCloudCOPCTaskService,
	pptxPDF *PPTXPDFTaskService,
	embedding *EmbeddingService,
	embeddingTask *EmbeddingTaskService,
	dataProfile *DataProfileService,
) *BoundedExecutionDispatcher {
	return &BoundedExecutionDispatcher{
		tileCache: tileCache, vectorTileSet: vectorTileSet, vectorMaterializedView: vectorMaterializedView,
		rasterCOG: rasterCOG, rasterMosaic: rasterMosaic, model3DGLB: model3DGLB,
		model3DTiles: model3DTiles, gaussianSplat: gaussianSplat, pointCloudCOPC: pointCloudCOPC,
		pptxPDF: pptxPDF, embedding: embedding, embeddingTask: embeddingTask, dataProfile: dataProfile,
	}
}

func (d *BoundedExecutionDispatcher) RunClaimedExecution(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease) error {
	if execution == nil || execution.ExecutionID != lease.ExecutionID || execution.TenantID != lease.TenantID {
		return fmt.Errorf("claimed Manager execution and lease are inconsistent")
	}
	taskID := uint(0)
	var err error
	if execution.SourceTaskID != nil {
		taskID, err = commonExecution.ParseSourceTaskIDUint(execution.SourceTaskID)
		if err != nil {
			return err
		}
	}
	tenantID := uint(execution.TenantID)
	ctx = commonExecution.ContextWithLease(ctx, lease)
	switch execution.TaskType {
	case commonExecution.TaskTypeVectorTileCacheGeneration:
		task, err := d.tileCache.tileCacheRepo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.tileCache.runTileCacheGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeVectorTileSetGeneration:
		task, err := d.vectorTileSet.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.vectorTileSet.run(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeVectorMaterializedViewGeneration:
		task, err := d.vectorMaterializedView.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.vectorMaterializedView.runVectorMaterializedView(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeRasterCOGGeneration:
		task, err := d.rasterCOG.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.rasterCOG.runRasterCOGGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeRasterMosaicGeneration:
		task, err := d.rasterMosaic.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.rasterMosaic.runRasterMosaicGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeModel3DGLBGeneration:
		task, err := d.model3DGLB.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.model3DGLB.runModel3DGLBGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeModel3DTilesGeneration:
		task, err := d.model3DTiles.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.model3DTiles.runModel3DTilesGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypeGaussianSplatKSplatGeneration:
		task, err := d.gaussianSplat.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.gaussianSplat.runGaussianSplatKSplatGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypePointCloudCOPCGeneration:
		task, err := d.pointCloudCOPC.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		d.pointCloudCOPC.runPointCloudCOPCGeneration(ctx, task, execution.ExecutionID)
	case commonExecution.TaskTypePPTXPDFGeneration:
		task, err := d.pptxPDF.repo.GetTask(ctx, taskID, tenantID)
		if err != nil || task == nil {
			return taskLoadError(execution, err)
		}
		task.Config = execution.ExecutionConfig.Clone()
		var frozen struct {
			Source struct {
				Locator         string `json:"item_locator"`
				SourceEngineID  uint   `json:"source_engine_id"`
				ItemID          uint   `json:"item_id"`
				ItemFingerprint string `json:"item_fingerprint"`
				SourceVersion   string `json:"source_version"`
				SourceSizeBytes int64  `json:"source_size_bytes"`
			} `json:"source"`
		}
		payload, marshalErr := json.Marshal(execution.ExecutionConfig)
		if marshalErr != nil {
			return fmt.Errorf("encode frozen PPTX PDF execution config: %w", marshalErr)
		}
		if err := json.Unmarshal(payload, &frozen); err != nil {
			return fmt.Errorf("decode frozen PPTX PDF execution config: %w", err)
		}
		task.Locator, task.SourceEngineID, task.ItemID = frozen.Source.Locator, frozen.Source.SourceEngineID, frozen.Source.ItemID
		task.ItemFingerprint, task.SourceVersion, task.SourceSizeBytes = frozen.Source.ItemFingerprint, frozen.Source.SourceVersion, frozen.Source.SourceSizeBytes
		return d.pptxPDF.RunClaimedExecution(ctx, execution, lease, task)
	case commonExecution.TaskTypeEmbedding:
		return d.runEmbedding(ctx, execution, lease, taskID)
	case commonExecution.TaskTypeDataProfiling:
		return d.dataProfile.runClaimedExecution(ctx, execution)
	default:
		return fmt.Errorf("unsupported Manager bounded task type %q", execution.TaskType)
	}
	return nil
}

func taskLoadError(execution *commonExecution.TaskExecution, err error) error {
	if err != nil {
		return fmt.Errorf("load %s task for execution %s: %w", execution.TaskType, execution.ExecutionID, err)
	}
	return fmt.Errorf("load %s task for execution %s: task not found", execution.TaskType, execution.ExecutionID)
}

func (d *BoundedExecutionDispatcher) runEmbedding(ctx context.Context, execution *commonExecution.TaskExecution, lease commonExecution.Lease, taskID uint) error {
	if d.embedding == nil {
		return fmt.Errorf("embedding service is not configured")
	}
	payload, err := json.Marshal(execution.ExecutionConfig)
	if err != nil {
		return fmt.Errorf("encode frozen embedding config: %w", err)
	}
	var req EmbeddingExecutionRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return fmt.Errorf("decode frozen embedding config: %w", err)
	}
	runtime, binding, profile, err := d.embedding.runtimeSnapshot(ctx, uint(execution.TenantID))
	if err != nil {
		return err
	}
	startedAt := time.Now().UTC()
	if execution.StartedAt != nil {
		startedAt = execution.StartedAt.UTC()
	}
	stats, runErr := d.embedding.RunEmbeddingExecution(ctx, uint(execution.TenantID), req, &EmbeddingExecutionContext{
		ExecutionID: execution.ExecutionID, TenantID: execution.TenantID, StartedAt: startedAt,
		Config: execution.ExecutionConfig, Runtime: runtime, Binding: binding, Profile: *profile,
		client: d.embedding.inferenceClient,
	})
	status := commonExecution.ExecutionStatusSuccess
	progress := 100
	var errorDetails commonModels.JSONMap
	if runErr != nil {
		status = commonExecution.ExecutionStatusFailed
		progress = 0
		errorDetails = commonModels.JSONMap{"message": runErr.Error()}
	}
	metadata := statsToJSONMap(stats)
	if status == commonExecution.ExecutionStatusSuccess {
		metadata = managerEmbeddingExecutionLineage(metadata, execution.ExecutionConfig)
	}
	completedAt := time.Now().UTC()
	fields := map[string]interface{}{
		"status": status, "progress": progress, "metadata": metadata, "error_details": errorDetails,
		"completed_at": completedAt, "execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "updated_at": completedAt,
	}
	if taskID > 0 {
		return d.embeddingTask.embeddingRepo.CompleteTaskExecution(ctx, taskID, uint(execution.TenantID), execution.ExecutionID, fields, completedAt)
	}
	return d.embedding.taskExecRepo.CompleteWithLease(ctx, lease, status, completedAt, map[string]interface{}{
		"progress": progress, "metadata": metadata, "error_details": errorDetails,
		"execution_time_ms": completedAt.Sub(startedAt).Milliseconds(),
	})
}
