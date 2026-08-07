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
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

type ManagerRasterMosaicExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	managerBaseURL  string
	invokeTimeout   time.Duration
}

func NewManagerRasterMosaicExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	managerBaseURL string,
	invokeTimeout time.Duration,
) *ManagerRasterMosaicExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 2 * time.Hour
	}
	return &ManagerRasterMosaicExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		managerBaseURL:  strings.TrimRight(strings.TrimSpace(managerBaseURL), "/"),
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerRasterMosaicExecutor) BuildRasterMosaic(ctx context.Context, req RasterMosaicExecutionRequest) (*RasterMosaicExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil {
		return nil, errors.New("raster mosaic generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("raster mosaic generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("raster mosaic execution_id is required")
	}
	accessPlan, err := e.buildAccessPlan(ctx, req.Task.TenantID, req.ExecutionID, req.Config)
	if err != nil {
		return nil, err
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "build_raster_mosaic")
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan,
			"placement": commonModels.JSONMap{
				"mode": req.Config.Placement.Mode,
			},
			"cog": commonModels.JSONMap{
				"compression":         req.Config.COG.Compression,
				"blocksize":           req.Config.COG.BlockSize,
				"overview_resampling": req.Config.COG.OverviewResampling,
				"validate_source_cog": req.Config.COG.ValidateSourceCOG,
				"leaf_concurrency":    req.Config.COG.LeafConcurrency,
				"num_threads":         req.Config.COG.NumThreads,
				"leaf_retry_attempts": req.Config.COG.LeafRetryAttempts,
			},
			"overview": commonModels.JSONMap{
				"enabled":    req.Config.Overview.Enabled,
				"max_pixels": req.Config.Overview.MaxPixels,
				"resampling": req.Config.Overview.Resampling,
			},
			"tiles": commonModels.JSONMap{
				"enabled":  req.Config.Tiles.Enabled,
				"min_zoom": req.Config.Tiles.MinZoom,
				"max_zoom": req.Config.Tiles.MaxZoom,
				"format":   req.Config.Tiles.Format,
			},
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke raster mosaic operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("raster mosaic direct operator invocation failed", invokeResult, nil)
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	result := &RasterMosaicExecutionResult{
		ManifestLocator: jsonString(facts["manifest_locator"]),
		ManifestRef:     jsonString(facts["manifest_ref"]),
		IndexRef:        jsonString(facts["index_ref"]),
		OverviewRef:     jsonString(facts["overview_ref"]),
		LeafCount:       jsonInt64(facts["leaf_count"]),
		Metadata: commonModels.JSONMap{
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"access_plan":  rasterMosaicAccessPlanAudit(accessPlan),
			"mosaic_facts": facts,
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if result.ManifestRef == "" && result.ManifestLocator == "" {
		return nil, errors.New("raster mosaic generation returned no manifest reference")
	}
	return result, nil
}

func (e *ManagerRasterMosaicExecutor) buildAccessPlan(ctx context.Context, tenantID uint, executionID string, cfg RasterMosaicExecutionConfig) (commonModels.JSONMap, error) {
	sourceLoc, err := resourcetree.ParseURI(cfg.Source.NodeLocator)
	if err != nil {
		return nil, fmt.Errorf("parse source locator: %w", err)
	}
	targetLoc, err := resourcetree.ParseURI(cfg.Target.StorageLocator)
	if err != nil {
		return nil, fmt.Errorf("parse target locator: %w", err)
	}
	sourceEngine, err := e.getRasterMosaicEngine(ctx, tenantID, cfg.Source.SourceEngineID)
	if err != nil {
		return nil, fmt.Errorf("get source engine: %w", err)
	}
	targetEngine, err := e.getRasterMosaicEngine(ctx, tenantID, cfg.Target.TargetEngineID)
	if err != nil {
		return nil, fmt.Errorf("get target engine: %w", err)
	}
	if cfg.Placement.Mode == "in_place" && rasterMosaicObjectStoreEngine(sourceEngine.EngineType) {
		return nil, fmt.Errorf("raster mosaic in_place placement is not supported for object store engine %s; use detached placement", sourceEngine.EngineType)
	}
	sourceRootURI, sourceEnv, sourceAccess, err := rasterMosaicGDALRoot(sourceEngine, sourceLoc, "")
	if err != nil {
		return nil, fmt.Errorf("prepare source GDAL root: %w", err)
	}
	targetDatasetName := ""
	if cfg.Placement.Mode == "detached" {
		targetDatasetName = cfg.Target.DatasetName
	}
	targetRootURI, targetEnv, targetAccess, err := rasterMosaicGDALRoot(targetEngine, targetLoc, targetDatasetName)
	if err != nil {
		return nil, fmt.Errorf("prepare target GDAL root: %w", err)
	}

	return commonModels.JSONMap{
		"source": commonModels.JSONMap{
			"root_uri":         sourceRootURI,
			"gdal_env":         sourceEnv,
			"recursive":        cfg.Source.Recursive,
			"include_patterns": cfg.Source.IncludePatterns,
			"exclude_patterns": cfg.Source.ExcludePatterns,
			"metadata": commonModels.JSONMap{
				"locator":       cfg.Source.NodeLocator,
				"engine_id":     cfg.Source.SourceEngineID,
				"engine_type":   sourceEngine.EngineType,
				"access_method": sourceAccess["access_method"],
			},
		},
		"target": commonModels.JSONMap{
			"dataset_root_uri": targetRootURI,
			"gdal_env":         targetEnv,
			"dataset_name":     cfg.Target.DatasetName,
			"metadata": commonModels.JSONMap{
				"locator":       cfg.Target.StorageLocator,
				"engine_id":     cfg.Target.TargetEngineID,
				"engine_type":   targetEngine.EngineType,
				"access_method": targetAccess["access_method"],
			},
		},
		"progress_callback": commonModels.JSONMap{
			"endpoint":     e.managerBaseURL + "/api/v1/manager/executions/" + strings.TrimSpace(executionID) + "/events",
			"tenant_id":    tenantID,
			"execution_id": strings.TrimSpace(executionID),
		},
	}, nil
}

func (e *ManagerRasterMosaicExecutor) getRasterMosaicEngine(ctx context.Context, tenantID uint, engineID uint) (*commonModels.Engine, error) {
	if engineID == 0 {
		return nil, errors.New("engine_id is required")
	}
	engine, err := e.systemClient.GetEngineForTenant(ctx, tenantID, engineID)
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

func (e *ManagerRasterMosaicExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for raster mosaic generation: %w", operatorName, err)
	}
	return engine, operator, nil
}

func rasterMosaicObjectStoreEngine(engineType string) bool {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3":
		return true
	default:
		return false
	}
}

func rasterMosaicGDALRoot(engine *commonModels.Engine, loc *resourcetree.ResourceLocator, appendPath string) (string, commonModels.JSONMap, commonModels.JSONMap, error) {
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
			return "", nil, nil, errors.New("file engine requires mount_path or export_path for GDAL access")
		}
		return joinFilePath(basePath, fullName), commonModels.JSONMap{}, commonModels.JSONMap{
			"engine_type":   engine.EngineType,
			"access_method": "mounted_path",
		}, nil
	case "minio", "s3":
		if fullName == "" {
			return "", nil, nil, errors.New("object store GDAL root requires bucket or bucket/prefix path")
		}
		cfg, err := sourceObjectClientConfig(engineType, connInfo)
		if err != nil {
			return "", nil, nil, fmt.Errorf("parse object store config: %w", err)
		}
		endpoint, useSSL := objectstore.ParseEndpoint(cfg.Endpoint, cfg.UseSSL)
		endpoint = objectstore.NormalizeEndpoint(endpoint)
		if endpoint == "" {
			return "", nil, nil, errors.New("object store endpoint is required for GDAL access")
		}
		env := commonModels.JSONMap{
			"AWS_S3_ENDPOINT":                         endpoint,
			"AWS_ACCESS_KEY_ID":                       cfg.AccessKey,
			"AWS_SECRET_ACCESS_KEY":                   cfg.SecretKey,
			"AWS_VIRTUAL_HOSTING":                     "FALSE",
			"AWS_HTTPS":                               gdalHTTPSValue(useSSL),
			"GDAL_DISABLE_READDIR_ON_OPEN":            "EMPTY_DIR",
			"CPL_VSIL_USE_TEMP_FILE_FOR_RANDOM_WRITE": "YES",
		}
		return "/vsis3/" + fullName, env, commonModels.JSONMap{
			"engine_type":   engine.EngineType,
			"access_method": "vsis3",
		}, nil
	default:
		return "", nil, nil, fmt.Errorf("engine %s is not supported by raster mosaic GDAL runtime", engine.EngineType)
	}
}

func rasterMosaicAccessPlanAudit(plan commonModels.JSONMap) commonModels.JSONMap {
	audit := commonModels.JSONMap{}
	for _, section := range []string{"source", "target"} {
		if part, ok := asJSONMap(plan[section]); ok {
			metadata, _ := asJSONMap(part["metadata"])
			audit[section] = metadata.Clone()
		}
	}
	return audit
}
