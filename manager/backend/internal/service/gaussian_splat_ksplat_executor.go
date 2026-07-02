package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type gaussianSplatKSplatObjectStore interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	StatObject(ctx context.Context, bucketName string, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ManagerGaussianSplatKSplatExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     gaussianSplatKSplatObjectStore
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerGaussianSplatKSplatExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore gaussianSplatKSplatObjectStore,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	minioUseSSL bool,
	defaultBucket string,
	invokeTimeout time.Duration,
) *ManagerGaussianSplatKSplatExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 30 * time.Minute
	}
	return &ManagerGaussianSplatKSplatExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		objectStore:     objectStore,
		minioEndpoint:   strings.TrimSpace(minioEndpoint),
		minioAccessKey:  minioAccessKey,
		minioSecretKey:  minioSecretKey,
		minioUseSSL:     minioUseSSL,
		defaultBucket:   strings.TrimSpace(defaultBucket),
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerGaussianSplatKSplatExecutor) BuildGaussianSplatKSplat(ctx context.Context, req GaussianSplatKSplatExecutionRequest) (*GaussianSplatKSplatExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("gaussian splat KSplat generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("gaussian splat KSplat generation task is required")
	}
	sourcePath, sourceFacts, err := e.prepareSourcePath(ctx, req.Task.TenantID, req.Config.Source)
	if err != nil {
		return nil, err
	}
	bucket, objectName, err := rastercogref.ObjectLocation(req.Config.Result.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, err
	}
	if err := e.ensureTargetBucket(ctx, bucket); err != nil {
		return nil, err
	}

	sourceFormat := string(format.NormalizeFormat(req.Config.Source.Format))
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "gaussian_splat_to_ksplat")
	if err != nil {
		return nil, err
	}
	options := req.Config.Options.Clone()
	if options == nil {
		options = commonModels.JSONMap{}
	}
	applyGaussianSplatBoundsOptions(options, req.Config.Source)
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"local_path": sourcePath,
					"format":     sourceFormat,
					"metadata":   sourceFacts,
				},
				"target": commonModels.JSONMap{
					"file_name": safeKSplatFileName(req.Config.Result.FileName),
					"metadata": commonModels.JSONMap{
						"storage_ref": req.Config.Result.StorageRef,
					},
					"publish": commonModels.JSONMap{
						"method":       "object_store",
						"endpoint":     e.minioEndpoint,
						"access_key":   e.minioAccessKey,
						"secret_key":   e.minioSecretKey,
						"use_ssl":      e.minioUseSSL,
						"bucket":       bucket,
						"object":       objectName,
						"locator":      fmt.Sprintf("s3://%s/%s", bucket, objectName),
						"content_type": "application/vnd.gaussian-ksplat",
					},
				},
			},
			"options": options,
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke gaussian splat to KSplat operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("gaussian splat to KSplat direct operator invocation failed", invokeResult, nil)
	}
	info, err := e.objectStore.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat gaussian splat KSplat artifact: %w", err)
	}

	facts := operatorInvokeJSONFacts(invokeResult)
	result := &GaussianSplatKSplatExecutionResult{
		StorageRef: req.Config.Result.StorageRef,
		FileName:   safeKSplatFileName(req.Config.Result.FileName),
		SizeBytes:  firstPositiveInt64(info.Size, jsonInt64(facts["size_bytes"]), req.Config.Source.SourceSizeBytes),
		ContentURL: "",
		Metadata: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"access": sourceFacts,
				"format": sourceFormat,
			},
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"ksplat_facts": facts,
			"artifact": commonModels.JSONMap{
				"bucket":       bucket,
				"object":       objectName,
				"storage_ref":  req.Config.Result.StorageRef,
				"content_type": "application/vnd.gaussian-ksplat",
			},
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	return result, nil
}

func applyGaussianSplatBoundsOptions(options commonModels.JSONMap, source GaussianSplatKSplatSourceConfig) {
	if options == nil {
		return
	}
	if _, ok := options["bounds_3d"]; !ok {
		if bounds := bounds3DToTaskConfig(source.Bounds3D); bounds != nil {
			options["bounds_3d"] = bounds
		}
	}
	if _, ok := options["sampled_bounds_3d"]; !ok {
		if bounds := bounds3DToTaskConfig(source.SampledBounds3D); bounds != nil {
			options["sampled_bounds_3d"] = bounds
		}
	}
	if _, ok := options["sampled_bounds_sample_count"]; !ok && source.SampledBoundsSampleCount != nil {
		options["sampled_bounds_sample_count"] = *source.SampledBoundsSampleCount
	}
}

func (e *ManagerGaussianSplatKSplatExecutor) prepareSourcePath(ctx context.Context, tenantID uint, source GaussianSplatKSplatSourceConfig) (string, commonModels.JSONMap, error) {
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return "", nil, fmt.Errorf("parse gaussian splat KSplat source locator: %w", err)
	}
	engine, err := e.systemClient.GetEngine(source.SourceEngineID)
	if err != nil {
		return "", nil, fmt.Errorf("get source engine: %w", err)
	}
	if engine == nil || !engine.IsActive {
		return "", nil, errors.New("source engine is not active")
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return "", nil, ErrEngineAccessDenied
	}
	sourcePath, _, access, err := model3DTilesLocalRoot(engine, loc, "")
	if err != nil {
		return "", nil, fmt.Errorf("prepare gaussian splat KSplat source path: %w", err)
	}
	return sourcePath, access, nil
}

func (e *ManagerGaussianSplatKSplatExecutor) ensureTargetBucket(ctx context.Context, bucket string) error {
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check gaussian splat KSplat bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create gaussian splat KSplat bucket: %w", err)
	}
	return nil
}

func (e *ManagerGaussianSplatKSplatExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for gaussian splat KSplat generation: %w", operatorName, err)
	}
	return engine, operator, nil
}
