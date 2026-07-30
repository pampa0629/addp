package service

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/models"
	"github.com/minio/minio-go/v7"
)

type ManagerModel3DTilesExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     model3DTilesObjectStore
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

type model3DTilesObjectStore interface {
	BucketExists(context.Context, string) (bool, error)
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	ListObjects(context.Context, string, minio.ListObjectsOptions) <-chan minio.ObjectInfo
	RemoveObject(context.Context, string, string, minio.RemoveObjectOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

func NewManagerModel3DTilesExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore model3DTilesObjectStore,
	minioEndpoint, minioAccessKey, minioSecretKey string,
	minioUseSSL bool,
	defaultBucket string,
	invokeTimeout time.Duration,
) *ManagerModel3DTilesExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 6 * time.Hour
	}
	return &ManagerModel3DTilesExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		objectStore:     objectStore,
		minioEndpoint:   strings.TrimSpace(minioEndpoint), minioAccessKey: minioAccessKey, minioSecretKey: minioSecretKey,
		minioUseSSL: minioUseSSL, defaultBucket: strings.TrimSpace(defaultBucket),
		invokeTimeout: invokeTimeout,
	}
}

func (e *ManagerModel3DTilesExecutor) BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("model 3d tiles generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("model 3d tiles generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("model 3d tiles execution_id is required")
	}
	accessPlan, bucket, prefix, err := e.buildAccessPlan(ctx, req.Task.TenantID, req.Config)
	if err != nil {
		return nil, err
	}
	operatorName := "osgb_scene_to_3dtiles"
	if req.Config.TargetFormat == models.Model3DTilesTargetFormatS3M {
		operatorName = "osgb_scene_to_s3m"
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, operatorName)
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan,
			"options":     req.Config.Options.Clone(),
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke OSGB scene tiles operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("OSGB scene tiles direct operator invocation failed", invokeResult, nil)
	}
	manifestRef := model3DTilesManifestRef(req.Config.TargetFormat)
	manifestInfo, err := e.objectStore.StatObject(ctx, bucket, path.Join(prefix, manifestRef), minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat model3d tiles manifest: %w", err)
	}
	var fileCount, sizeBytes int64
	for object := range e.objectStore.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: strings.Trim(prefix, "/") + "/", Recursive: true}) {
		if object.Err != nil {
			return nil, object.Err
		}
		fileCount++
		sizeBytes += object.Size
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	if req.Config.TargetFormat == models.Model3DTilesTargetFormatS3M {
		expectedRootTiles := int64FromConfig(facts["root_tile_count"], 0)
		if fileCount <= 1 || (expectedRootTiles > 0 && fileCount < expectedRootTiles+1) {
			return nil, fmt.Errorf("incomplete S3M artifact: published_files=%d root_tile_count=%d", fileCount, expectedRootTiles)
		}
	}
	result := &Model3DTilesExecutionResult{
		StorageRef: req.Config.Result.StorageRef, ManifestRef: manifestRef, FileCount: fileCount, SizeBytes: sizeBytes,
		Metadata: commonModels.JSONMap{
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"access_plan":   accessPlanAudit(accessPlan),
			"target_format": req.Config.TargetFormat,
			"tiles_facts":   facts,
			"artifact":      commonModels.JSONMap{"bucket": bucket, "prefix": prefix, "manifest_ref": manifestRef, "manifest_size_bytes": manifestInfo.Size},
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	return result, nil
}

func (e *ManagerModel3DTilesExecutor) buildAccessPlan(ctx context.Context, tenantID uint, cfg Model3DTilesExecutionConfig) (commonModels.JSONMap, string, string, error) {
	sourceLoc, err := resourcetree.ParseURI(cfg.Source.ItemLocator)
	if err != nil {
		return nil, "", "", fmt.Errorf("parse source locator: %w", err)
	}
	sourceEngine, err := e.getModel3DTilesEngine(ctx, tenantID, cfg.Source.SourceEngineID)
	if err != nil {
		return nil, "", "", fmt.Errorf("get source engine: %w", err)
	}
	source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: sourceEngine, Locator: sourceLoc, Kind: workflowaccess.KindDirectory, Format: string(format.FormatOSGBScene),
		Metadata: commonModels.JSONMap{"locator": cfg.Source.ItemLocator, "engine_id": cfg.Source.SourceEngineID, "engine_type": sourceEngine.EngineType},
	})
	if err != nil {
		return nil, "", "", fmt.Errorf("prepare source: %w", err)
	}
	bucket, prefix, err := rastercogref.ObjectLocation(cfg.Result.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, "", "", fmt.Errorf("resolve model3d tiles target: %w", err)
	}
	if err := ensureModel3DTilesBucket(ctx, e.objectStore, bucket); err != nil {
		return nil, "", "", err
	}
	if err := deleteModel3DTilesPrefix(ctx, e.objectStore, bucket, prefix); err != nil {
		return nil, "", "", err
	}
	targetAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL}, bucket, prefix, workflowaccess.KindDirectory)
	if err != nil {
		return nil, "", "", fmt.Errorf("prepare model3d tiles target access: %w", err)
	}
	targetRuntimeFormat := "3dtiles"
	if cfg.TargetFormat == models.Model3DTilesTargetFormatS3M {
		targetRuntimeFormat = "s3m"
	}
	plan, err := workflowaccess.New(source, workflowaccess.Target{Kind: workflowaccess.KindDirectory, Format: targetRuntimeFormat, Name: path.Base(prefix), WriteMode: workflowaccess.WriteModeReplace, Access: targetAccess, Metadata: commonModels.JSONMap{"storage_ref": cfg.Result.StorageRef}})
	if err != nil {
		return nil, "", "", err
	}
	return plan.JSONMap(), bucket, strings.Trim(prefix, "/"), nil
}

func ensureModel3DTilesBucket(ctx context.Context, store model3DTilesObjectStore, bucket string) error {
	exists, err := store.BucketExists(ctx, bucket)
	if err != nil {
		return err
	}
	if exists {
		return nil
	}
	return store.MakeBucket(ctx, bucket, minio.MakeBucketOptions{})
}

func deleteModel3DTilesPrefix(ctx context.Context, store model3DTilesObjectStore, bucket, prefix string) error {
	for object := range store.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: strings.Trim(prefix, "/") + "/", Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		if err := store.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}

func (e *ManagerModel3DTilesExecutor) getModel3DTilesEngine(ctx context.Context, tenantID uint, engineID uint) (*commonModels.Engine, error) {
	if engineID == 0 {
		return nil, errors.New("engine_id is required")
	}
	engine, err := e.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, err
	}
	if !engine.IsUsable() {
		return nil, errors.New("engine is not active")
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return nil, ErrEngineAccessDenied
	}
	return engine, nil
}

func (e *ManagerModel3DTilesExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for model 3d tiles generation: %w", operatorName, err)
	}
	return engine, operator, nil
}

func accessPlanAudit(plan commonModels.JSONMap) commonModels.JSONMap {
	audit := commonModels.JSONMap{}
	for _, section := range []string{"source", "target"} {
		if part, ok := asJSONMap(plan[section]); ok {
			metadata, _ := asJSONMap(part["metadata"])
			audit[section] = metadata.Clone()
		}
	}
	return audit
}
