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
	"github.com/addp/common/engine/plugins/objectstore"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type ManagerModel3DTilesExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	invokeTimeout   time.Duration
}

func NewManagerModel3DTilesExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	invokeTimeout time.Duration,
) *ManagerModel3DTilesExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 6 * time.Hour
	}
	return &ManagerModel3DTilesExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerModel3DTilesExecutor) BuildModel3DTiles(ctx context.Context, req Model3DTilesExecutionRequest) (*Model3DTilesExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil {
		return nil, errors.New("model 3d tiles generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("model 3d tiles generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("model 3d tiles execution_id is required")
	}
	accessPlan, err := e.buildAccessPlan(ctx, req.Task.TenantID, req.Config)
	if err != nil {
		return nil, err
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "osgb_scene_to_3dtiles")
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan,
			"tiles": commonModels.JSONMap{
				"format": req.Config.Tiles.Format,
			},
			"options": req.Config.Options.Clone(),
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke OSGB scene to 3D Tiles operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("OSGB scene to 3D Tiles direct operator invocation failed", invokeResult, nil)
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	result := &Model3DTilesExecutionResult{
		TilesetLocator: jsonString(facts["tileset_locator"]),
		TilesetRef:     jsonString(facts["tileset_ref"]),
		TileCount:      jsonInt64(facts["tile_count"]),
		Metadata: commonModels.JSONMap{
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"access_plan": accessPlanAudit(accessPlan),
			"tiles_facts": facts,
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if result.TilesetRef == "" && result.TilesetLocator == "" {
		return nil, errors.New("model 3d tiles generation returned no tileset reference")
	}
	return result, nil
}

func (e *ManagerModel3DTilesExecutor) buildAccessPlan(ctx context.Context, tenantID uint, cfg Model3DTilesExecutionConfig) (commonModels.JSONMap, error) {
	sourceLoc, err := resourcetree.ParseURI(cfg.Source.ItemLocator)
	if err != nil {
		return nil, fmt.Errorf("parse source locator: %w", err)
	}
	targetLoc, err := resourcetree.ParseURI(cfg.Target.StorageLocator)
	if err != nil {
		return nil, fmt.Errorf("parse target locator: %w", err)
	}
	sourceEngine, err := e.getModel3DTilesEngine(ctx, tenantID, cfg.Source.SourceEngineID)
	if err != nil {
		return nil, fmt.Errorf("get source engine: %w", err)
	}
	targetEngine, err := e.getModel3DTilesEngine(ctx, tenantID, cfg.Target.TargetEngineID)
	if err != nil {
		return nil, fmt.Errorf("get target engine: %w", err)
	}
	sourceRootURI, sourceEnv, sourceAccess, sourceStage, err := model3DTilesSourcePlan(sourceEngine, sourceLoc)
	if err != nil {
		return nil, fmt.Errorf("prepare source: %w", err)
	}
	targetRootURI, targetEnv, targetAccess, targetPublish, err := model3DTilesTargetPlan(targetEngine, targetLoc, cfg.Target.DatasetName)
	if err != nil {
		return nil, fmt.Errorf("prepare target: %w", err)
	}

	targetPlan := commonModels.JSONMap{
		"dataset_root_uri": targetRootURI,
		"env":              targetEnv,
		"dataset_name":     cfg.Target.DatasetName,
		"metadata": commonModels.JSONMap{
			"locator":       cfg.Target.StorageLocator,
			"engine_id":     cfg.Target.TargetEngineID,
			"engine_type":   targetEngine.EngineType,
			"format":        "3dtiles",
			"access_method": targetAccess["access_method"],
		},
	}
	if targetPublish != nil {
		targetPlan["publish"] = targetPublish
	}

	sourcePlan := commonModels.JSONMap{
		"root_uri": sourceRootURI,
		"env":      sourceEnv,
		"metadata": commonModels.JSONMap{
			"locator":       cfg.Source.ItemLocator,
			"engine_id":     cfg.Source.SourceEngineID,
			"engine_type":   sourceEngine.EngineType,
			"format":        string(format.FormatOSGBScene),
			"access_method": sourceAccess["access_method"],
		},
	}
	if sourceStage != nil {
		sourcePlan["stage"] = sourceStage
	}

	return commonModels.JSONMap{
		"source": sourcePlan,
		"target": targetPlan,
	}, nil
}

func model3DTilesLocalRoot(engine *commonModels.Engine, loc *resourcetree.ResourceLocator, appendPath string) (string, commonModels.JSONMap, commonModels.JSONMap, error) {
	if engine == nil || loc == nil {
		return "", nil, nil, errors.New("engine and locator are required")
	}
	fullName := strings.Trim(strings.TrimSpace(loc.FullName()), "/")
	if appendPath = strings.Trim(appendPath, "/"); appendPath != "" {
		fullName = joinFilePath(fullName, appendPath)
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	switch engineType {
	case "nfs", "nas", "localfs", "filesystem":
		basePath := firstNonEmptyConfig(plugin.GetString(connInfo, "mount_path"), plugin.GetString(connInfo, "export_path"), plugin.GetString(connInfo, "base_path"))
		if basePath == "" {
			return "", nil, nil, errors.New("file engine requires mount_path, export_path, or base_path for model 3D conversion")
		}
		return joinFilePath(basePath, fullName), commonModels.JSONMap{}, commonModels.JSONMap{
			"engine_type":   engine.EngineType,
			"access_method": "mounted_path",
		}, nil
	case "minio", "s3":
		return "", nil, nil, errors.New("model 3d tiles generation first phase supports nfs/localfs only; object store requires staging support")
	default:
		return "", nil, nil, fmt.Errorf("engine %s is not supported by model 3D conversion runtime", engine.EngineType)
	}
}

func model3DTilesSourcePlan(engine *commonModels.Engine, loc *resourcetree.ResourceLocator) (string, commonModels.JSONMap, commonModels.JSONMap, commonModels.JSONMap, error) {
	if engine == nil || loc == nil {
		return "", nil, nil, nil, errors.New("engine and locator are required")
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	switch engineType {
	case "nfs", "nas", "localfs", "filesystem":
		root, env, access, err := model3DTilesLocalRoot(engine, loc, "")
		return root, env, access, nil, err
	case "minio", "s3":
		fullName := strings.Trim(strings.TrimSpace(loc.FullName()), "/")
		bucket, prefix := objectstore.SplitBucketPrefix(fullName)
		if strings.TrimSpace(bucket) == "" || strings.TrimSpace(prefix) == "" {
			return "", nil, nil, nil, errors.New("object store source requires bucket/prefix scene path")
		}
		cfg, err := sourceObjectClientConfig(engineType, plugin.ConnectionInfo(engine.ConnectionInfo))
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("parse object store config: %w", err)
		}
		return "", commonModels.JSONMap{}, commonModels.JSONMap{
				"engine_type":   engine.EngineType,
				"access_method": "object_store_stage",
			}, commonModels.JSONMap{
				"method":      "object_store",
				"engine_type": engine.EngineType,
				"endpoint":    cfg.Endpoint,
				"access_key":  cfg.AccessKey,
				"secret_key":  cfg.SecretKey,
				"use_ssl":     cfg.UseSSL,
				"bucket":      bucket,
				"prefix":      strings.Trim(prefix, "/"),
				"locator":     loc.ToURI(),
			}, nil
	default:
		return "", nil, nil, nil, fmt.Errorf("engine %s is not supported by model 3D conversion runtime", engine.EngineType)
	}
}

func model3DTilesTargetPlan(engine *commonModels.Engine, loc *resourcetree.ResourceLocator, datasetName string) (string, commonModels.JSONMap, commonModels.JSONMap, commonModels.JSONMap, error) {
	if engine == nil || loc == nil {
		return "", nil, nil, nil, errors.New("engine and locator are required")
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	switch engineType {
	case "nfs", "nas", "localfs", "filesystem":
		root, env, access, err := model3DTilesLocalRoot(engine, loc, datasetName)
		return root, env, access, nil, err
	case "minio", "s3":
		fullName := strings.Trim(strings.TrimSpace(loc.FullName()), "/")
		if datasetName = strings.Trim(datasetName, "/"); datasetName != "" {
			fullName = joinFilePath(fullName, datasetName)
		}
		bucket, prefix := objectstore.SplitBucketPrefix(fullName)
		if strings.TrimSpace(bucket) == "" || strings.TrimSpace(prefix) == "" {
			return "", nil, nil, nil, errors.New("object store target requires bucket/prefix dataset path")
		}
		cfg, err := sourceObjectClientConfig(engineType, plugin.ConnectionInfo(engine.ConnectionInfo))
		if err != nil {
			return "", nil, nil, nil, fmt.Errorf("parse object store config: %w", err)
		}
		datasetLocator := loc.Clone()
		if datasetName != "" {
			datasetLocator.Path = append(append([]string{}, loc.Path...), datasetName)
		}
		datasetLocator.Type = resourcetree.TypeDirectory
		return "", commonModels.JSONMap{}, commonModels.JSONMap{
				"engine_type":   engine.EngineType,
				"access_method": "object_store_publish",
			}, commonModels.JSONMap{
				"method":      "object_store",
				"engine_type": engine.EngineType,
				"endpoint":    cfg.Endpoint,
				"access_key":  cfg.AccessKey,
				"secret_key":  cfg.SecretKey,
				"use_ssl":     cfg.UseSSL,
				"bucket":      bucket,
				"prefix":      strings.Trim(prefix, "/"),
				"locator":     datasetLocator.ToURI(),
			}, nil
	default:
		return "", nil, nil, nil, fmt.Errorf("engine %s is not supported by model 3D conversion runtime", engine.EngineType)
	}
}

func (e *ManagerModel3DTilesExecutor) getModel3DTilesEngine(ctx context.Context, tenantID uint, engineID uint) (*commonModels.Engine, error) {
	if engineID == 0 {
		return nil, errors.New("engine_id is required")
	}
	engine, err := e.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, err
	}
	if engine == nil || !engine.IsActive {
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
