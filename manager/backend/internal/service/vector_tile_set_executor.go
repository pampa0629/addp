package service

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/instanceprovider"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	commonModels "github.com/addp/common/models"
	commonPMTiles "github.com/addp/common/pmtiles"
	"github.com/addp/common/resourcetree"
	"github.com/addp/common/spatial"
	"github.com/addp/manager/internal/mvt"
	"github.com/addp/manager/internal/tilecache"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type ManagerVectorTileSetExecutor struct {
	systemClient                   *commonClient.SystemClient
	workflowEngines                workflowEngineLister
	managerBaseURL, internalAPIKey string
	invokeTimeout                  time.Duration
	infraObjectStore               *minio.Client
	infraEndpoint                  string
	infraAccessKey                 string
	infraSecretKey                 string
	infraUseSSL                    bool
	infraBucket                    string
	nativeGenerator                PostGISPMTilesArchiveGenerator
	nativeConcurrency              int
}

func (e *ManagerVectorTileSetExecutor) SetTemporarySourceStorage(endpoint, accessKey, secretKey string, useSSL bool, bucket string) {
	e.infraEndpoint = strings.TrimSpace(endpoint)
	e.infraAccessKey = accessKey
	e.infraSecretKey = secretKey
	e.infraUseSSL = useSSL
	e.infraBucket = strings.TrimSpace(bucket)
}

func (e *ManagerVectorTileSetExecutor) SetPostGISGenerator(generator PostGISPMTilesArchiveGenerator, concurrency int) {
	e.nativeGenerator = generator
	e.nativeConcurrency = concurrency
	if e.nativeConcurrency <= 0 {
		e.nativeConcurrency = 4
	}
}

func NewManagerVectorTileSetExecutor(systemClient *commonClient.SystemClient, workflowEngines workflowEngineLister, infraObjectStore *minio.Client, managerBaseURL, internalAPIKey string, timeout time.Duration) *ManagerVectorTileSetExecutor {
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	return &ManagerVectorTileSetExecutor{systemClient: systemClient, workflowEngines: workflowEngines, infraObjectStore: infraObjectStore, managerBaseURL: strings.TrimRight(managerBaseURL, "/"), internalAPIKey: strings.TrimSpace(internalAPIKey), invokeTimeout: timeout}
}

func (e *ManagerVectorTileSetExecutor) GenerateVectorTileSet(ctx context.Context, req VectorTileSetExecutionRequest) (*VectorTileSetExecutionResult, error) {
	if e == nil || e.systemClient == nil || req.Task == nil {
		return nil, errors.New("vector tile set executor is not fully configured")
	}
	if err := validateVectorTileSetConfig(req.Config); err != nil {
		return nil, err
	}
	targetEngine, err := e.systemClient.GetEngine(req.Config.TargetEngineID)
	if err != nil || !targetEngine.IsUsable() {
		return nil, errors.New("target business engine is not active")
	}
	if targetEngine.IsBuiltin || targetEngine.TenantID == nil || *targetEngine.TenantID != req.Task.TenantID {
		return nil, ErrEngineAccessDenied
	}
	targetLoc, err := resourcetree.ParseURI(req.Config.TargetLocator)
	if err != nil {
		return nil, err
	}
	targetURI, targetEnv, targetFacts, err := rasterMosaicGDALRoot(targetEngine, targetLoc, req.Config.TargetName)
	if err != nil {
		return nil, fmt.Errorf("prepare business PMTiles target: %w", err)
	}
	if strings.TrimSpace(req.Config.ReusableCacheStorageRef) != "" {
		facts, err := e.copyReusableCache(ctx, req, targetEngine, targetURI)
		if err != nil {
			return nil, err
		}
		return &VectorTileSetExecutionResult{CatalogPath: vectorTileSetCatalogPath(req.Config), Metadata: commonModels.JSONMap{
			"profile_hash": req.Config.ProfileHash, "pmtiles": facts, "reuse": commonModels.JSONMap{"source": "manager_vector_tile_cache", "validated": true},
		}}, nil
	}
	if req.Config.Source.SourceKind == string(resourcetree.TypeTable) {
		sourceEngine, err := e.systemClient.GetEngine(req.Config.Source.EngineID)
		if err != nil {
			return nil, fmt.Errorf("get vector tile set source engine: %w", err)
		}
		if instanceprovider.IsSuperMapSDXPostgreSQLTable(sourceEngine, req.Config.Source.Schema, req.Config.Source.Table) {
			return nil, errors.New("SuperMap SDX+ for PostgreSQL does not support MVT vector tile set generation")
		}
		switch {
		case spatial.IsPostGISEngine(sourceEngine.EngineType):
			return e.generatePostGISVectorTileSet(ctx, req, targetEngine, targetURI)
		case strings.EqualFold(strings.TrimSpace(sourceEngine.EngineType), "mysql"):
			return e.generateMySQLWorkflowVectorTileSet(ctx, req, targetURI, targetEnv, targetFacts)
		default:
			return nil, fmt.Errorf("database table engine %s does not support vector tile set generation", sourceEngine.EngineType)
		}
	}
	if e.workflowEngines == nil {
		return nil, errors.New("workflow runtime is required for file vector tile set generation")
	}
	sourceResolver := &ManagerVectorTileCacheWorkflowExecutor{systemClient: e.systemClient}
	sourceURI, sourceEnv, sourceFacts, err := sourceResolver.prepareSourceURI(ctx, req.Task.TenantID, req.Config.Source)
	if err != nil {
		return nil, err
	}
	return e.invokeWorkflowVectorTileSet(ctx, req, sourceURI, sourceEnv, sourceFacts, targetURI, targetEnv, targetFacts)
}

func (e *ManagerVectorTileSetExecutor) generateMySQLWorkflowVectorTileSet(ctx context.Context, req VectorTileSetExecutionRequest, targetURI string, targetEnv, targetFacts commonModels.JSONMap) (*VectorTileSetExecutionResult, error) {
	if e.workflowEngines == nil || e.infraObjectStore == nil {
		return nil, errors.New("workflow runtime and infra object store are required for MySQL vector tile set generation")
	}
	sourceMaterializer := &ManagerVectorTileCacheWorkflowExecutor{
		systemClient: e.systemClient, objectStore: e.infraObjectStore,
		minioEndpoint: e.infraEndpoint, minioAccessKey: e.infraAccessKey, minioSecretKey: e.infraSecretKey,
		minioUseSSL: e.infraUseSSL, defaultBucket: e.infraBucket,
	}
	sourceURI, sourceEnv, sourceFacts, cleanup, err := sourceMaterializer.prepareMySQLTableFlatGeobufSource(
		ctx, req.Task.TenantID, req.ExecutionID, req.Config.Source, req.Config.Options,
	)
	if err != nil {
		return nil, err
	}
	result, invokeErr := e.invokeWorkflowVectorTileSet(ctx, req, sourceURI, sourceEnv, sourceFacts, targetURI, targetEnv, targetFacts)
	cleanupErr := cleanup(context.Background())
	if invokeErr != nil || cleanupErr != nil {
		return nil, errors.Join(invokeErr, cleanupErr)
	}
	return result, nil
}

func (e *ManagerVectorTileSetExecutor) invokeWorkflowVectorTileSet(ctx context.Context, req VectorTileSetExecutionRequest, sourceURI string, sourceEnv, sourceFacts commonModels.JSONMap, targetURI string, targetEnv, targetFacts commonModels.JSONMap) (*VectorTileSetExecutionResult, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(req.Task.TenantID)
	if err != nil {
		return nil, err
	}
	workflowEngine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{OperatorName: "vector_to_pmtiles"})
	if err != nil {
		return nil, err
	}
	result, err := dbbridge.InvokeOperator(ctx, &workflowEngine, operator.Name, plugin.OperatorInvokeRequest{Params: map[string]interface{}{
		"access_plan": commonModels.JSONMap{
			"source":            commonModels.JSONMap{"root_uri": sourceURI, "layer_name": sourceFacts["layer_name"], "gdal_env": sourceEnv, "metadata": sourceFacts},
			"target":            commonModels.JSONMap{"archive_uri": targetURI, "gdal_env": targetEnv, "metadata": targetFacts},
			"progress_callback": commonModels.JSONMap{"endpoint": e.managerBaseURL + "/api/v1/manager/internal/executions/" + req.ExecutionID + "/events", "tenant_id": req.Task.TenantID, "execution_id": req.ExecutionID, "internal_api_key": e.internalAPIKey},
		}, "tile": req.Config.Tile, "options": req.Config.Options,
	}, Timeout: e.invokeTimeout})
	if err != nil {
		return nil, operatorInvokeError("invoke vector to PMTiles operator", result, err)
	}
	if result.Status != "" && result.Status != "success" {
		return nil, operatorInvokeError("vector to PMTiles operator failed", result, nil)
	}
	facts := operatorInvokeJSONFacts(result)
	if jsonString(facts["archive_format"]) != "pmtiles" || jsonString(facts["header_hash"]) == "" {
		return nil, errors.New("vector tile set generation returned invalid PMTiles facts")
	}
	return &VectorTileSetExecutionResult{CatalogPath: vectorTileSetCatalogPath(req.Config), Metadata: commonModels.JSONMap{
		"profile_hash": req.Config.ProfileHash, "pmtiles": facts,
		"workflow_runtime": commonModels.JSONMap{"engine_id": workflowEngine.ID, "engine_name": workflowEngine.Name, "operator": operator.Name, "execution_id": result.ExecutionID, "mode": "direct"},
	}}, nil
}

func (e *ManagerVectorTileSetExecutor) generatePostGISVectorTileSet(ctx context.Context, req VectorTileSetExecutionRequest, targetEngine *commonModels.Engine, targetURI string) (*VectorTileSetExecutionResult, error) {
	if e.nativeGenerator == nil {
		return nil, errors.New("PostGIS PMTiles generator is not connected")
	}
	extent, ok := floatSliceFromConfig(req.Config.Tile["extent"])
	if !ok || len(extent) != 4 {
		return nil, errors.New("PostGIS vector tile set extent is required")
	}
	optimizationConfig := commonModels.JSONMap{"optimization": req.Config.Optimization}
	cfg := mvt.QuickViewConfig{
		EngineID: req.Config.Source.EngineID, TenantID: req.Task.TenantID,
		Schema: req.Config.Source.Schema, Table: req.Config.Source.Table,
		GeomColumn: stringFromConfig(req.Config.Options["geometry_column"]),
		PrimaryKey: stringFromConfig(req.Config.Options["primary_key"]),
		SRID:       intFromTileCacheConfig(req.Config.Tile["source_srid"], 0),
		Extent:     extent, ExtentSRID: intFromTileCacheConfig(req.Config.Tile["extent_srid"], spatial.SRIDWGS84),
		MinZoom: intFromTileCacheConfig(req.Config.Tile["min_zoom"], 0), MaxZoom: intFromTileCacheConfig(req.Config.Tile["max_zoom"], 0),
		LayerName: stringFromConfig(req.Config.Options["layer_name"]), Concurrency: e.nativeConcurrency,
		OptimizationConfig: tileCacheOptimizationConfig(optimizationConfig),
	}
	archive, err := e.nativeGenerator.Generate(ctx, cfg, nil)
	if err != nil {
		return nil, err
	}
	defer archive.Close()
	if err := validatePMTilesFile(archive.Path); err != nil {
		return nil, fmt.Errorf("validate generated PostGIS PMTiles: %w", err)
	}
	if err := publishGeneratedPMTiles(ctx, archive, req, targetEngine, targetURI); err != nil {
		return nil, err
	}
	facts := commonModels.JSONMap{
		"archive_format": "pmtiles", "spec_version": 3, "tile_format": "mvt", "tile_compression": "gzip",
		"header_hash": archive.HeaderHash, "archive_size_bytes": archive.Size,
		"total_tiles": archive.Result.TotalTiles, "generated_tiles": archive.Result.GeneratedTiles,
		"empty_tiles": archive.Result.EmptyTiles, "actual_max_zoom": archive.Result.ActualMaxZoom,
		"stop_reason": archive.Result.StopReason, "generation_seconds": archive.Result.GenerationSec,
	}
	return &VectorTileSetExecutionResult{CatalogPath: vectorTileSetCatalogPath(req.Config), Metadata: commonModels.JSONMap{
		"profile_hash": req.Config.ProfileHash, "pmtiles": facts,
		"generator": commonModels.JSONMap{"kind": "postgis_native", "sql": "ST_AsMVT"},
	}}, nil
}

func publishGeneratedPMTiles(ctx context.Context, archive *mvt.GeneratedPMTilesArchive, req VectorTileSetExecutionRequest, targetEngine *commonModels.Engine, targetURI string) error {
	switch strings.ToLower(strings.TrimSpace(targetEngine.EngineType)) {
	case "nfs", "nas", "localfs", "filesystem":
		tempPath := targetURI + "." + req.ExecutionID + ".partial"
		if err := os.MkdirAll(filepath.Dir(targetURI), 0o755); err != nil {
			return err
		}
		source, err := os.Open(archive.Path)
		if err != nil {
			return err
		}
		target, err := os.Create(tempPath)
		if err != nil {
			_ = source.Close()
			return err
		}
		_, copyErr := io.Copy(target, source)
		sourceCloseErr := source.Close()
		targetCloseErr := target.Close()
		if copyErr != nil || sourceCloseErr != nil || targetCloseErr != nil {
			_ = os.Remove(tempPath)
			return errors.Join(copyErr, sourceCloseErr, targetCloseErr)
		}
		if err := validatePMTilesFile(tempPath); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		if err := os.Rename(tempPath, targetURI); err != nil {
			_ = os.Remove(tempPath)
			return err
		}
		return nil
	case "minio", "s3":
		cfg, err := sourceObjectClientConfig(strings.ToLower(targetEngine.EngineType), plugin.ConnectionInfo(targetEngine.ConnectionInfo))
		if err != nil {
			return err
		}
		endpoint, secure := objectstore.ParseEndpoint(cfg.Endpoint, cfg.UseSSL)
		client, err := minio.New(objectstore.NormalizeEndpoint(endpoint), &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: secure})
		if err != nil {
			return err
		}
		bucket, objectName, err := splitObjectFullName(vectorTileSetCatalogPath(req.Config))
		if err != nil {
			return err
		}
		if err := ensurePMTilesBucket(ctx, client, bucket); err != nil {
			return err
		}
		tempObject := objectName + "." + req.ExecutionID + ".partial"
		file, err := os.Open(archive.Path)
		if err != nil {
			return err
		}
		_, putErr := client.PutObject(ctx, bucket, tempObject, file, archive.Size, minio.PutObjectOptions{ContentType: "application/vnd.pmtiles"})
		closeErr := file.Close()
		if putErr != nil || closeErr != nil {
			_ = client.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
			return errors.Join(putErr, closeErr)
		}
		if _, _, err := validatePMTilesObject(ctx, client, bucket, tempObject); err != nil {
			_ = client.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
			return err
		}
		_, err = client.CopyObject(ctx, minio.CopyDestOptions{Bucket: bucket, Object: objectName}, minio.CopySrcOptions{Bucket: bucket, Object: tempObject})
		_ = client.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
		return err
	default:
		return fmt.Errorf("target engine %s cannot store PMTiles", targetEngine.EngineType)
	}
}

func (e *ManagerVectorTileSetExecutor) copyReusableCache(ctx context.Context, req VectorTileSetExecutionRequest, targetEngine *commonModels.Engine, targetURI string) (commonModels.JSONMap, error) {
	if e.infraObjectStore == nil {
		return nil, errors.New("infra object store is required for PMTiles cache reuse")
	}
	sourceBucket, sourceObject, err := tilecache.ObjectLocation(req.Config.ReusableCacheStorageRef, "manager")
	if err != nil {
		return nil, err
	}
	sourceHeader, sourceSize, err := validatePMTilesObject(ctx, e.infraObjectStore, sourceBucket, sourceObject)
	if err != nil {
		return nil, fmt.Errorf("validate reusable PMTiles cache: %w", err)
	}
	source, err := e.infraObjectStore.GetObject(ctx, sourceBucket, sourceObject, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer source.Close()
	switch strings.ToLower(strings.TrimSpace(targetEngine.EngineType)) {
	case "nfs", "nas", "localfs", "filesystem":
		tempPath := targetURI + "." + req.ExecutionID + ".partial"
		if err := os.MkdirAll(filepath.Dir(targetURI), 0o755); err != nil {
			return nil, err
		}
		file, err := os.Create(tempPath)
		if err != nil {
			return nil, err
		}
		_, copyErr := io.Copy(file, source)
		closeErr := file.Close()
		if copyErr != nil {
			_ = os.Remove(tempPath)
			return nil, copyErr
		}
		if closeErr != nil {
			_ = os.Remove(tempPath)
			return nil, closeErr
		}
		if err := validatePMTilesFile(tempPath); err != nil {
			_ = os.Remove(tempPath)
			return nil, err
		}
		if err := os.Rename(tempPath, targetURI); err != nil {
			_ = os.Remove(tempPath)
			return nil, err
		}
	case "minio", "s3":
		cfg, err := sourceObjectClientConfig(strings.ToLower(targetEngine.EngineType), plugin.ConnectionInfo(targetEngine.ConnectionInfo))
		if err != nil {
			return nil, err
		}
		endpoint, secure := objectstore.ParseEndpoint(cfg.Endpoint, cfg.UseSSL)
		client, err := minio.New(objectstore.NormalizeEndpoint(endpoint), &minio.Options{Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: secure})
		if err != nil {
			return nil, err
		}
		catalogPath := vectorTileSetCatalogPath(req.Config)
		bucket, objectName, err := splitObjectFullName(catalogPath)
		if err != nil {
			return nil, err
		}
		tempObject := objectName + "." + req.ExecutionID + ".partial"
		if _, err := client.PutObject(ctx, bucket, tempObject, source, sourceSize, minio.PutObjectOptions{ContentType: "application/vnd.pmtiles"}); err != nil {
			return nil, err
		}
		if _, _, err := validatePMTilesObject(ctx, client, bucket, tempObject); err != nil {
			_ = client.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
			return nil, err
		}
		_, err = client.CopyObject(ctx, minio.CopyDestOptions{Bucket: bucket, Object: objectName}, minio.CopySrcOptions{Bucket: bucket, Object: tempObject})
		_ = client.RemoveObject(ctx, bucket, tempObject, minio.RemoveObjectOptions{})
		if err != nil {
			return nil, err
		}
	default:
		return nil, fmt.Errorf("target engine %s cannot store PMTiles", targetEngine.EngineType)
	}
	hash, _ := commonPMTiles.HeaderHash(sourceHeader)
	return commonModels.JSONMap{"archive_format": "pmtiles", "spec_version": 3, "tile_format": "mvt", "tile_compression": "gzip", "header_hash": hash, "archive_size_bytes": sourceSize}, nil
}

func validatePMTilesObject(ctx context.Context, client *minio.Client, bucket, objectName string) ([]byte, int64, error) {
	info, err := client.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, 0, err
	}
	readRange := func(ctx context.Context, offset, length int64) ([]byte, error) {
		opts := minio.GetObjectOptions{}
		if err := opts.SetRange(offset, offset+length-1); err != nil {
			return nil, err
		}
		obj, err := client.GetObject(ctx, bucket, objectName, opts)
		if err != nil {
			return nil, err
		}
		defer obj.Close()
		data, err := io.ReadAll(io.LimitReader(obj, length))
		if err != nil {
			return nil, err
		}
		if int64(len(data)) != length {
			return nil, io.ErrUnexpectedEOF
		}
		return data, nil
	}
	headerData, err := readRange(ctx, 0, commonPMTiles.HeaderSize)
	if err != nil {
		return nil, 0, err
	}
	header, err := commonPMTiles.ParseHeaderBytes(headerData)
	if err != nil {
		return nil, 0, err
	}
	if err := commonPMTiles.ValidateHeader(header, info.Size); err != nil {
		return nil, 0, err
	}
	archive, err := commonPMTiles.NewArchive(header, readRange)
	if err != nil {
		return nil, 0, err
	}
	if err := archive.ValidateDirectories(ctx); err != nil {
		return nil, 0, err
	}
	return headerData, info.Size, nil
}

func validatePMTilesFile(path string) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return err
	}
	headerData := make([]byte, commonPMTiles.HeaderSize)
	if _, err := io.ReadFull(file, headerData); err != nil {
		return err
	}
	header, err := commonPMTiles.ParseHeaderBytes(headerData)
	if err != nil {
		return err
	}
	if err := commonPMTiles.ValidateHeader(header, info.Size()); err != nil {
		return err
	}
	archive, err := commonPMTiles.NewArchive(header, func(_ context.Context, offset, length int64) ([]byte, error) {
		data := make([]byte, length)
		_, err := file.ReadAt(data, offset)
		return data, err
	})
	if err != nil {
		return err
	}
	return archive.ValidateDirectories(context.Background())
}
