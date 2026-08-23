package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	neturl "net/url"
	"strings"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugins/objectstore"
	commonModels "github.com/addp/common/models"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/engineaccess"

	"github.com/addp/common/engine/plugin"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type workflowEngineLister interface {
	ListWorkflowEngines(tenantID uint) ([]commonModels.Engine, error)
}

type rasterCOGObjectStore interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	StatObject(ctx context.Context, bucketName string, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ManagerRasterCOGExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     rasterCOGObjectStore
	infraEndpoint   string
	infraAccessKey  string
	infraSecretKey  string
	infraUseSSL     bool
	defaultBucket   string
}

func NewManagerRasterCOGExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore rasterCOGObjectStore,
	infraEndpoint string,
	infraAccessKey string,
	infraSecretKey string,
	infraUseSSL bool,
	defaultBucket string,
) *ManagerRasterCOGExecutor {
	return &ManagerRasterCOGExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		objectStore:     objectStore,
		infraEndpoint:   strings.TrimSpace(infraEndpoint),
		infraAccessKey:  strings.TrimSpace(infraAccessKey),
		infraSecretKey:  strings.TrimSpace(infraSecretKey),
		infraUseSSL:     infraUseSSL,
		defaultBucket:   strings.TrimSpace(defaultBucket),
	}
}

func (e *ManagerRasterCOGExecutor) BuildRasterCOG(ctx context.Context, req RasterCOGExecutionRequest) (*RasterCOGExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil {
		return nil, errors.New("raster COG generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("raster COG generation task is required")
	}
	if req.Config.Target.SourceEngineID == 0 || strings.TrimSpace(req.Config.Target.FullName) == "" {
		return nil, errors.New("raster COG source target is incomplete")
	}

	sourceURI, sourceFacts, err := e.prepareSourceURI(ctx, req.Task.TenantID, req.Config.Target.SourceEngineID, req.Config.Target.FullName)
	if err != nil {
		return nil, err
	}
	targetURI, gdalEnv, err := e.prepareTargetURI(ctx, req.Config.Result.StorageRef)
	if err != nil {
		return nil, err
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "tiff_to_cog")
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"source_uri":          sourceURI,
			"target_uri":          targetURI,
			"gdal_env":            gdalEnv,
			"assign_srs":          rasterCOGAssignSRS(req.Config.Raster),
			"compression":         req.Config.COG.Compression,
			"blocksize":           req.Config.COG.BlockSize,
			"overview_resampling": req.Config.COG.OverviewResampling,
			"overwrite":           true,
		},
	})
	if err != nil {
		return nil, operatorInvokeError("invoke COG operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("COG direct operator invocation failed", invokeResult, nil)
	}
	objectSize, err := e.statTargetObjectSize(ctx, req.Config.Result.StorageRef)
	if err != nil {
		return nil, err
	}

	facts := operatorInvokeJSONFacts(invokeResult)
	sourceSRID := authoritativeRasterSRID(jsonInt(facts["source_srid"]), jsonInt(facts["extent_srid"]), req.Config.Raster.SourceSRID)
	extentSRID := authoritativeExtentSRID(jsonInt(facts["extent_srid"]), req.Config.Raster.ExtentSRID, sourceSRID)
	sourceCRS := authoritativeRasterCRS(jsonString(facts["source_crs"]), req.Config.Raster.SourceCRS, sourceSRID)
	if strings.Contains(sourceCRS, "[") || len(sourceCRS) > 255 {
		sourceCRS = authoritativeRasterCRS("", req.Config.Raster.SourceCRS, sourceSRID)
	}
	if sourceSRID > 0 {
		facts["source_srid"] = sourceSRID
	}
	if extentSRID > 0 {
		facts["extent_srid"] = extentSRID
	}
	if strings.TrimSpace(sourceCRS) != "" {
		facts["source_crs"] = sourceCRS
	}
	result := &RasterCOGExecutionResult{
		StorageRef: req.Config.Result.StorageRef,
		FileName:   safeCOGFileName(req.Config.Result.FileName),
		SizeBytes:  firstPositiveInt64(objectSize, jsonInt64(facts["size_bytes"]), req.Config.Raster.SourceSizeBytes),
		Width:      firstPositiveInt64(jsonInt64(facts["width"]), req.Config.Raster.Width),
		Height:     firstPositiveInt64(jsonInt64(facts["height"]), req.Config.Raster.Height),
		BandCount:  firstPositiveInt64(jsonInt64(facts["band_count"]), req.Config.Raster.BandCount),
		SourceSRID: sourceSRID,
		SourceCRS:  sourceCRS,
		Extent:     firstExtent(jsonFloatSlice(facts["extent"]), req.Config.Raster.Extent),
		ExtentSRID: extentSRID,
		Metadata: commonModels.JSONMap{
			"source": commonModels.JSONMap{
				"access": sourceFacts,
			},
			"workflow_runtime": commonModels.JSONMap{
				"engine_id":    workflowEngine.ID,
				"engine_name":  workflowEngine.Name,
				"engine_type":  workflowEngine.EngineType,
				"execution_id": invokeResult.ExecutionID,
				"operator":     workflowOperator.Name,
				"mode":         "direct",
			},
			"raster_facts": facts,
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if len(result.Extent) != 4 {
		result.Extent = nil
	}
	return result, nil
}

func authoritativeRasterSRID(runtimeSourceSRID, runtimeExtentSRID, configuredSourceSRID int) int {
	runtimeSRID := firstPositiveInt(runtimeSourceSRID, runtimeExtentSRID)
	if configuredSourceSRID <= 0 {
		return runtimeSRID
	}
	if runtimeSRID <= 0 || runtimeSRID != configuredSourceSRID {
		return configuredSourceSRID
	}
	return runtimeSRID
}

func authoritativeExtentSRID(runtimeExtentSRID, configuredExtentSRID, sourceSRID int) int {
	if configuredExtentSRID > 0 && (runtimeExtentSRID <= 0 || runtimeExtentSRID != configuredExtentSRID) {
		return configuredExtentSRID
	}
	return firstPositiveInt(runtimeExtentSRID, configuredExtentSRID, sourceSRID)
}

func authoritativeRasterCRS(runtimeCRS, configuredCRS string, sourceSRID int) string {
	runtimeCRS = strings.TrimSpace(runtimeCRS)
	configuredCRS = strings.TrimSpace(configuredCRS)
	expectedCRS := ""
	if sourceSRID > 0 {
		expectedCRS = fmt.Sprintf("EPSG:%d", sourceSRID)
	}
	if expectedCRS != "" && strings.HasPrefix(strings.ToUpper(runtimeCRS), "EPSG:") && !strings.EqualFold(runtimeCRS, expectedCRS) {
		if configuredCRS != "" {
			return configuredCRS
		}
		return expectedCRS
	}
	return firstNonEmptyConfig(runtimeCRS, configuredCRS, expectedCRS)
}

func rasterCOGAssignSRS(raster RasterCOGRasterConfig) string {
	if raster.SourceSRID == 4326 {
		return "+proj=longlat +datum=WGS84 +no_defs"
	}
	if raster.SourceSRID > 0 {
		return fmt.Sprintf("EPSG:%d", raster.SourceSRID)
	}
	return strings.TrimSpace(raster.SourceCRS)
}

func (e *ManagerRasterCOGExecutor) prepareSourceURI(ctx context.Context, tenantID, engineID uint, fullName string) (string, commonModels.JSONMap, error) {
	engine, err := e.systemClient.GetEngineForTenant(ctx, tenantID, engineID)
	if err != nil {
		return "", nil, fmt.Errorf("get source engine: %w", err)
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return "", nil, err
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return "", nil, ErrEngineAccessDenied
	}
	engineType := strings.ToLower(strings.TrimSpace(engine.EngineType))
	connInfo := plugin.ConnectionInfo(engine.ConnectionInfo)
	switch engineType {
	case "nfs", "nas", "localfs", "filesystem":
		basePath := firstNonEmptyConfig(plugin.GetString(connInfo, "mount_path"), plugin.GetString(connInfo, "export_path"), plugin.GetString(connInfo, "base_path"))
		if basePath == "" {
			return "", nil, errors.New("source file engine requires mount_path or export_path for GDAL access")
		}
		sourcePath := joinFilePath(basePath, fullName)
		return sourcePath, commonModels.JSONMap{"engine_type": engine.EngineType, "access_method": "mounted_path"}, nil
	case "minio", "s3":
		rawURL, err := presignObjectURL(ctx, engineType, connInfo, fullName)
		if err != nil {
			return "", nil, err
		}
		return "/vsicurl/" + rawURL, commonModels.JSONMap{"engine_type": engine.EngineType, "access_method": "vsicurl_presigned_url"}, nil
	default:
		return "", nil, fmt.Errorf("source engine %s is not supported by COG GDAL runtime", engine.EngineType)
	}
}

func (e *ManagerRasterCOGExecutor) prepareTargetURI(ctx context.Context, storageRef string) (string, commonModels.JSONMap, error) {
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, e.defaultBucket)
	if err != nil {
		return "", nil, err
	}
	if e.infraEndpoint == "" || e.infraAccessKey == "" || e.infraSecretKey == "" {
		return "", nil, errors.New("infra MinIO config is required for raster COG generation")
	}
	if err := e.ensureTargetBucket(ctx, bucket); err != nil {
		return "", nil, err
	}
	gdalEndpoint, gdalUseSSL := objectstore.ParseEndpoint(e.infraEndpoint, e.infraUseSSL)
	gdalEndpoint = objectstore.NormalizeEndpoint(gdalEndpoint)
	if gdalEndpoint == "" {
		return "", nil, errors.New("infra MinIO endpoint is required for COG GDAL environment")
	}
	env := commonModels.JSONMap{
		"AWS_S3_ENDPOINT":                         gdalEndpoint,
		"AWS_ACCESS_KEY_ID":                       e.infraAccessKey,
		"AWS_SECRET_ACCESS_KEY":                   e.infraSecretKey,
		"AWS_VIRTUAL_HOSTING":                     "FALSE",
		"AWS_HTTPS":                               gdalHTTPSValue(gdalUseSSL),
		"CPL_VSIL_USE_TEMP_FILE_FOR_RANDOM_WRITE": "YES",
	}
	return "/vsis3/" + strings.Trim(bucket, "/") + "/" + strings.Trim(objectName, "/"), env, nil
}

func (e *ManagerRasterCOGExecutor) ensureTargetBucket(ctx context.Context, bucket string) error {
	if e.objectStore == nil {
		return errors.New("raster COG object store is not configured")
	}
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check raster COG bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create raster COG bucket: %w", err)
	}
	return nil
}

func (e *ManagerRasterCOGExecutor) statTargetObjectSize(ctx context.Context, storageRef string) (int64, error) {
	if e.objectStore == nil {
		return 0, errors.New("raster COG object store is not configured")
	}
	bucket, objectName, err := rastercogref.ObjectLocation(storageRef, e.defaultBucket)
	if err != nil {
		return 0, err
	}
	info, err := e.objectStore.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return 0, fmt.Errorf("stat raster COG object: %w", err)
	}
	return info.Size, nil
}

func (e *ManagerRasterCOGExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for raster COG generation: %w", operatorName, err)
	}
	return engine, operator, nil
}

func presignObjectURL(ctx context.Context, engineType string, connInfo plugin.ConnectionInfo, fullName string) (string, error) {
	bucket, objectName, err := splitObjectFullName(fullName)
	if err != nil {
		return "", err
	}
	cfg, err := sourceObjectClientConfig(engineType, connInfo)
	if err != nil {
		return "", fmt.Errorf("parse source object engine config: %w", err)
	}
	client, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	})
	if err != nil {
		return "", fmt.Errorf("create source object client: %w", err)
	}
	rawURL, err := client.PresignedGetObject(ctx, bucket, objectName, 2*time.Hour, neturl.Values{})
	if err != nil {
		return "", fmt.Errorf("presign source object: %w", err)
	}
	return rawURL.String(), nil
}

func sourceObjectClientConfig(engineType string, connInfo plugin.ConnectionInfo) (objectstore.ClientConfig, error) {
	engineType = strings.ToLower(strings.TrimSpace(engineType))
	defaultSSL := engineType == "s3"
	normalizeEndpoint := engineType == "minio"
	return objectstore.ParseClientConfig(connInfo, defaultSSL, normalizeEndpoint)
}

func splitObjectFullName(fullName string) (string, string, error) {
	trimmed := strings.Trim(strings.TrimSpace(fullName), "/")
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
		return "", "", errors.New("object source full_name must be bucket/object")
	}
	return parts[0], parts[1], nil
}

func joinFilePath(basePath, relativePath string) string {
	base := strings.TrimRight(strings.TrimSpace(basePath), "/")
	rel := strings.TrimLeft(strings.TrimSpace(relativePath), "/")
	if base == "" {
		return "/" + rel
	}
	if rel == "" {
		return base
	}
	return base + "/" + rel
}

func safeCOGFileName(name string) string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(name), "/"), "/")
	base := ""
	if len(parts) > 0 {
		base = parts[len(parts)-1]
	}
	if base == "." || base == "" {
		return "raster.cog.tif"
	}
	return base
}

func gdalHTTPSValue(useSSL bool) string {
	if useSSL {
		return "YES"
	}
	return "NO"
}

func operatorInvokeJSONFacts(result *plugin.OperatorInvokeResult) commonModels.JSONMap {
	if result == nil || result.Result == nil {
		return commonModels.JSONMap{}
	}
	raw := result.Result["result"]
	switch typed := raw.(type) {
	case string:
		facts := commonModels.JSONMap{}
		if err := json.Unmarshal([]byte(typed), &facts); err == nil {
			return facts
		}
	case map[string]interface{}:
		return commonModels.JSONMap(typed)
	}
	return jsonMapFromInterface(result.Result)
}

func operatorInvokeError(prefix string, result *plugin.OperatorInvokeResult, err error) error {
	parts := []string{strings.TrimSpace(prefix)}
	if result != nil {
		if result.Error != "" {
			parts = append(parts, result.Error)
		}
		if result.ErrorCode != "" {
			parts = append(parts, "error_code="+result.ErrorCode)
		}
		if result.Details != "" {
			parts = append(parts, "details="+result.Details)
		}
	}
	if err != nil {
		parts = append(parts, err.Error())
	}
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			cleaned = append(cleaned, strings.TrimSpace(part))
		}
	}
	if len(cleaned) == 0 {
		return errors.New("COG direct operator invocation failed")
	}
	return errors.New(strings.Join(cleaned, ": "))
}

func jsonMapFromInterface(raw map[string]interface{}) commonModels.JSONMap {
	if raw == nil {
		return commonModels.JSONMap{}
	}
	facts := commonModels.JSONMap{}
	for key, value := range raw {
		facts[key] = value
	}
	if nested, ok := facts["result"].(commonModels.JSONMap); ok {
		return nested
	}
	if nested, ok := facts["result"].(map[string]interface{}); ok {
		return commonModels.JSONMap(nested)
	}
	return facts
}

func jsonString(value interface{}) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func jsonInt(value interface{}) int {
	return int(jsonInt64(value))
}

func jsonInt64(value interface{}) int64 {
	switch typed := value.(type) {
	case int:
		return int64(typed)
	case int64:
		return typed
	case float64:
		return int64(typed)
	case json.Number:
		parsed, _ := typed.Int64()
		return parsed
	default:
		return 0
	}
}

func jsonFloatSlice(value interface{}) []float64 {
	switch typed := value.(type) {
	case []float64:
		return typed
	case []interface{}:
		result := make([]float64, 0, len(typed))
		for _, item := range typed {
			switch v := item.(type) {
			case float64:
				result = append(result, v)
			case int:
				result = append(result, float64(v))
			default:
				return nil
			}
		}
		return result
	default:
		return nil
	}
}

func firstPositiveInt64(values ...int64) int64 {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstPositiveInt(values ...int) int {
	for _, value := range values {
		if value > 0 {
			return value
		}
	}
	return 0
}

func firstExtent(values ...[]float64) []float64 {
	for _, value := range values {
		if len(value) == 4 {
			return value
		}
	}
	return nil
}
