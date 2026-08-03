package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
)

type vectorTileCacheObjectStore interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	PutObject(ctx context.Context, bucketName, objectName string, reader io.Reader, objectSize int64, opts minio.PutObjectOptions) (minio.UploadInfo, error)
	RemoveObject(ctx context.Context, bucketName, objectName string, opts minio.RemoveObjectOptions) error
}

type ManagerVectorTileCacheWorkflowExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     vectorTileCacheObjectStore
	managerBaseURL  string
	internalAPIKey  string
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerVectorTileCacheWorkflowExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore vectorTileCacheObjectStore,
	managerBaseURL string,
	internalAPIKey string,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	minioUseSSL bool,
	defaultBucket string,
	invokeTimeout time.Duration,
) *ManagerVectorTileCacheWorkflowExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 2 * time.Hour
	}
	return &ManagerVectorTileCacheWorkflowExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		objectStore:     objectStore,
		managerBaseURL:  strings.TrimRight(strings.TrimSpace(managerBaseURL), "/"),
		internalAPIKey:  strings.TrimSpace(internalAPIKey),
		minioEndpoint:   strings.TrimSpace(minioEndpoint),
		minioAccessKey:  minioAccessKey,
		minioSecretKey:  minioSecretKey,
		minioUseSSL:     minioUseSSL,
		defaultBucket:   strings.TrimSpace(defaultBucket),
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerVectorTileCacheWorkflowExecutor) GenerateVectorTileCache(ctx context.Context, req WorkflowTileCacheRequest) (output *mvt.GenerateResult, outputMetadata commonModels.JSONMap, returnErr error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, nil, errors.New("vector tile cache workflow executor is not fully configured")
	}
	if req.Task == nil {
		return nil, nil, errors.New("vector tile cache generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, nil, errors.New("vector tile cache execution_id is required")
	}
	var sourceURI string
	var sourceEnv, sourceFacts commonModels.JSONMap
	var err error
	cleanupSource := func(context.Context) error { return nil }
	if req.Identity.SourceKind == "table" {
		sourceURI, sourceEnv, sourceFacts, cleanupSource, err = e.prepareMySQLTableFlatGeobufSource(ctx, req.Task.TenantID, req.ExecutionID, req.Identity, req.Options)
	} else {
		sourceURI, sourceEnv, sourceFacts, err = e.prepareSourceURI(ctx, req.Task.TenantID, req.Identity)
	}
	if err != nil {
		return nil, nil, err
	}
	defer func() {
		if cleanupErr := cleanupSource(context.Background()); cleanupErr != nil {
			output = nil
			outputMetadata = nil
			returnErr = errors.Join(returnErr, fmt.Errorf("clean temporary vector source: %w", cleanupErr))
		}
	}()
	bucket, objectName, err := tilecache.ObjectLocation(req.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, nil, err
	}
	if err := e.ensureTargetBucket(ctx, bucket); err != nil {
		return nil, nil, err
	}
	targetURI, targetEnv, err := e.prepareTargetArchiveURI(bucket, objectName)
	if err != nil {
		return nil, nil, err
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "vector_to_pmtiles")
	if err != nil {
		return nil, nil, err
	}

	tile := req.Tile.Clone()
	sourceSRID := intFromTileCacheConfig(tile["source_srid"], 0)
	if sourceSRID > 0 {
		tile["source_srs"] = fmt.Sprintf("EPSG:%d", sourceSRID)
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": commonModels.JSONMap{
				"source": commonModels.JSONMap{
					"root_uri":         sourceURI,
					"layer_name":       sourceFacts["layer_name"],
					"gdal_env":         sourceEnv,
					"engine_id":        req.Identity.EngineID,
					"source_kind":      req.Identity.SourceKind,
					"full_name":        req.Identity.FullName,
					"item_fingerprint": req.Identity.ItemFingerprint,
					"row_count":        int64FromConfig(req.Tile["row_count"], 0),
					"metadata":         sourceFacts,
				},
				"target": commonModels.JSONMap{
					"archive_uri": targetURI,
					"object":      objectName,
					"storage_ref": req.StorageRef,
					"gdal_env":    targetEnv,
				},
				"progress_callback": commonModels.JSONMap{
					"endpoint":         e.managerBaseURL + "/api/v1/manager/internal/executions/" + strings.TrimSpace(req.ExecutionID) + "/events",
					"tenant_id":        req.Task.TenantID,
					"execution_id":     strings.TrimSpace(req.ExecutionID),
					"internal_api_key": e.internalAPIKey,
				},
			},
			"tile":    tile,
			"options": req.Options.Clone(),
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, nil, operatorInvokeError("invoke vector to PMTiles operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, nil, operatorInvokeError("vector to PMTiles direct operator invocation failed", invokeResult, nil)
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	result := generateResultFromWorkflowFacts(facts)
	metadata := commonModels.JSONMap{
		"engine_id":    workflowEngine.ID,
		"engine_name":  workflowEngine.Name,
		"engine_type":  workflowEngine.EngineType,
		"execution_id": invokeResult.ExecutionID,
		"operator":     workflowOperator.Name,
		"mode":         "direct",
		"source":       sourceFacts,
	}
	if invokeResult.ExecutionTimeMs != nil {
		metadata["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if mvtOptions, ok := asJSONMap(facts["mvt_options"]); ok {
		metadata["mvt_options"] = mvtOptions.Clone()
	}
	for _, key := range []string{"archive_format", "spec_version", "tile_format", "tile_compression", "header_hash", "archive_size_bytes"} {
		if value, ok := facts[key]; ok {
			metadata[key] = value
		}
	}
	return result, metadata, nil
}

func (e *ManagerVectorTileCacheWorkflowExecutor) prepareSourceURI(ctx context.Context, tenantID uint, identity tileCacheTaskTargetIdentity) (string, commonModels.JSONMap, commonModels.JSONMap, error) {
	engine, err := e.systemClient.GetEngine(identity.EngineID)
	if err != nil {
		return "", nil, nil, fmt.Errorf("get source engine: %w", err)
	}
	if !engine.IsUsable() {
		return "", nil, nil, errors.New("source engine is not active")
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return "", nil, nil, ErrEngineAccessDenied
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	fullName := strings.Trim(strings.TrimSpace(identity.FullName), "/")
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	switch engineType {
	case "nfs", "nas", "localfs", "filesystem":
		basePath := firstNonEmptyConfig(plugin.GetString(connInfo, "mount_path"), plugin.GetString(connInfo, "export_path"), plugin.GetString(connInfo, "base_path"))
		if basePath == "" {
			return "", nil, nil, errors.New("file engine requires mount_path, export_path, or base_path for vector tile generation")
		}
		return joinFilePath(basePath, fullName), commonModels.JSONMap{}, commonModels.JSONMap{
			"engine_type":   engine.EngineType,
			"access_method": "mounted_path",
		}, nil
	case "minio", "s3":
		sourceURI, env, facts, err := prepareObjectStoreGDALSource(engine.EngineType, engineType, connInfo, fullName)
		if err != nil {
			return "", nil, nil, err
		}
		return sourceURI, env, facts, nil
	default:
		return "", nil, nil, fmt.Errorf("source engine %s is not supported by vector tile workflow runtime", engine.EngineType)
	}
}

func prepareObjectStoreGDALSource(displayEngineType, engineType string, connInfo plugin.ConnectionInfo, fullName string) (string, commonModels.JSONMap, commonModels.JSONMap, error) {
	fullName = strings.Trim(strings.TrimSpace(fullName), "/")
	if fullName == "" {
		return "", nil, nil, errors.New("object store source full_name is required for vector tile GDAL access")
	}
	if _, _, err := splitObjectFullName(fullName); err != nil {
		return "", nil, nil, err
	}
	cfg, err := sourceObjectClientConfig(engineType, connInfo)
	if err != nil {
		return "", nil, nil, fmt.Errorf("parse object store config: %w", err)
	}
	endpoint, useSSL := objectstore.ParseEndpoint(cfg.Endpoint, cfg.UseSSL)
	endpoint = objectstore.NormalizeEndpoint(endpoint)
	if endpoint == "" {
		return "", nil, nil, errors.New("object store endpoint is required for vector tile GDAL access")
	}
	env := commonModels.JSONMap{
		"AWS_S3_ENDPOINT":       endpoint,
		"AWS_ACCESS_KEY_ID":     cfg.AccessKey,
		"AWS_SECRET_ACCESS_KEY": cfg.SecretKey,
		"AWS_VIRTUAL_HOSTING":   "FALSE",
		"AWS_HTTPS":             gdalHTTPSValue(useSSL),
	}
	return "/vsis3/" + fullName, env, commonModels.JSONMap{
		"engine_type":   displayEngineType,
		"access_method": "vsis3_object_storage",
	}, nil
}

func (e *ManagerVectorTileCacheWorkflowExecutor) prepareTargetArchiveURI(bucket, objectName string) (string, commonModels.JSONMap, error) {
	if e.minioEndpoint == "" || e.minioAccessKey == "" || e.minioSecretKey == "" {
		return "", nil, errors.New("infra MinIO config is required for vector tile cache generation")
	}
	gdalEndpoint, gdalUseSSL := objectstore.ParseEndpoint(e.minioEndpoint, e.minioUseSSL)
	gdalEndpoint = objectstore.NormalizeEndpoint(gdalEndpoint)
	if gdalEndpoint == "" {
		return "", nil, errors.New("infra MinIO endpoint is required for vector tile GDAL environment")
	}
	env := commonModels.JSONMap{
		"AWS_S3_ENDPOINT":       gdalEndpoint,
		"AWS_ACCESS_KEY_ID":     e.minioAccessKey,
		"AWS_SECRET_ACCESS_KEY": e.minioSecretKey,
		"AWS_VIRTUAL_HOSTING":   "FALSE",
		"AWS_HTTPS":             gdalHTTPSValue(gdalUseSSL),
	}
	return "/vsis3/" + strings.Trim(bucket, "/") + "/" + strings.Trim(objectName, "/"), env, nil
}

func (e *ManagerVectorTileCacheWorkflowExecutor) ensureTargetBucket(ctx context.Context, bucket string) error {
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check vector tile cache bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create vector tile cache bucket: %w", err)
	}
	return nil
}

func (e *ManagerVectorTileCacheWorkflowExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for vector tile cache generation: %w", operatorName, err)
	}
	return engine, operator, nil
}

func generateResultFromWorkflowFacts(facts commonModels.JSONMap) *mvt.GenerateResult {
	return &mvt.GenerateResult{
		TotalTiles:         jsonInt(facts["total_tiles"]),
		CachedTiles:        jsonInt(facts["cached_tiles"]),
		TilesTotalEstimate: jsonInt(facts["tiles_total_estimate"]),
		TilesProcessed:     jsonInt(facts["tiles_processed"]),
		GeneratedTiles:     jsonInt(facts["generated_tiles"]),
		EmptyTiles:         jsonInt(facts["empty_tiles"]),
		SkippedTiles:       jsonInt(facts["skipped_tiles"]),
		OversizedTiles:     jsonInt(facts["oversized_skipped_tiles"]),
		FailedTiles:        jsonInt(facts["failed_tiles"]),
		TotalSizeBytes:     jsonInt64(facts["total_size_bytes"]),
		MaxTileSizeBytes:   jsonInt64(facts["max_tile_size_bytes"]),
		MinTileSizeBytes:   jsonInt64(facts["min_tile_size_bytes"]),
		ZoomLevels:         workflowZoomLevels(facts["zoom_levels"]),
		ActualMaxZoom:      jsonInt(facts["actual_max_zoom"]),
		StopReason:         jsonString(facts["stop_reason"]),
		GenerationSec:      floatFromTileCacheConfig(facts["generation_seconds"], 0),
		ExtentWGS84:        jsonFloatSlice(facts["extent"]),
	}
}

func workflowZoomLevels(raw interface{}) map[string]mvt.ZoomLevelStats {
	values, ok := raw.(map[string]interface{})
	if !ok {
		if typed, ok := raw.(commonModels.JSONMap); ok {
			values = map[string]interface{}(typed)
		}
	}
	if len(values) == 0 {
		return nil
	}
	out := make(map[string]mvt.ZoomLevelStats, len(values))
	for key, value := range values {
		item, _ := asJSONMap(value)
		out[key] = mvt.ZoomLevelStats{
			Zoom:           intFromTileCacheConfig(item["zoom"], 0),
			TotalTiles:     intFromTileCacheConfig(item["total_tiles"], 0),
			GeneratedTiles: intFromTileCacheConfig(item["generated_tiles"], 0),
			EmptyTiles:     intFromTileCacheConfig(item["empty_tiles"], 0),
			SkippedTiles:   intFromTileCacheConfig(item["skipped_tiles"], 0),
			OversizedTiles: intFromTileCacheConfig(item["oversized_tiles"], 0),
			FailedTiles:    intFromTileCacheConfig(item["failed_tiles"], 0),
			AvgGenTimeMs:   floatFromTileCacheConfig(item["avg_gen_time_ms"], 0),
			AvgSizeKB:      floatFromTileCacheConfig(item["avg_size_kb"], 0),
			TotalSizeBytes: int64FromConfig(item["total_size_bytes"], 0),
			MaxSizeBytes:   int64FromConfig(item["max_size_bytes"], 0),
			MinSizeBytes:   int64FromConfig(item["min_size_bytes"], 0),
		}
	}
	return out
}
