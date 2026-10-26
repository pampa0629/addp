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
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/engineaccess"
	managerModels "github.com/addp/manager/internal/models"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/lifecycle"
)

type model3DGLBObjectStore interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	StatObject(ctx context.Context, bucketName string, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
	RemoveObject(ctx context.Context, bucketName string, objectName string, opts minio.RemoveObjectOptions) error
	GetBucketLifecycle(ctx context.Context, bucketName string) (*lifecycle.Configuration, error)
	GetBucketVersioning(ctx context.Context, bucketName string) (minio.BucketVersioningConfiguration, error)
}

type ManagerModel3DGLBExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     model3DGLBObjectStore
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerModel3DGLBExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore model3DGLBObjectStore,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	minioUseSSL bool,
	defaultBucket string,
	invokeTimeout time.Duration,
) *ManagerModel3DGLBExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 30 * time.Minute
	}
	return &ManagerModel3DGLBExecutor{
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

func (e *ManagerModel3DGLBExecutor) BuildModel3DGLB(ctx context.Context, req Model3DGLBExecutionRequest) (output *Model3DGLBExecutionResult, buildErr error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("model 3d GLB generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("model 3d GLB generation task is required")
	}
	if req.Config.ConfigVersion != "" && req.Config.ConfigVersion != managerModels.Model3DGLBFileConfigVersion {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	fileConfig := req.Config.ConfigVersion == managerModels.Model3DGLBFileConfigVersion
	operatorName, sourceFormat, err := model3DGLBOperatorForFormat(req.Config.Source.Format)
	if err != nil {
		return nil, err
	}
	engine, err := e.lookupModel3DSourceEngine(ctx, req.Task.TenantID, req.Config.Source.SourceEngineID)
	if err != nil {
		return nil, err
	}
	if (fileConfig || req.SourceAuthorization != nil) && !managedModel3DFileSource(engine.EngineType, sourceFormat) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	if managedModel3DFileSource(engine.EngineType, sourceFormat) {
		if !fileConfig || req.SourceAuthorization == nil {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		var result *Model3DGLBExecutionResult
		err = e.withFileModel3DSource(ctx, req, engine, func(ctx context.Context, sourcePlan workflowaccess.Source, sourceFacts commonModels.JSONMap) error {
			var invokeErr error
			result, invokeErr = e.buildModel3DGLBWithSource(ctx, req, operatorName, sourceFormat, sourcePlan, sourceFacts)
			return invokeErr
		})
		if err != nil {
			return nil, err
		}
		return result, nil
	}
	sourcePlan, sourceFacts, err := e.prepareSourceForEngine(ctx, engine, req.Config.Source, sourceFormat)
	if err != nil {
		return nil, err
	}
	return e.buildModel3DGLBWithSource(ctx, req, operatorName, sourceFormat, sourcePlan, sourceFacts)
}

func (e *ManagerModel3DGLBExecutor) buildModel3DGLBWithSource(ctx context.Context, req Model3DGLBExecutionRequest, operatorName, sourceFormat string, sourcePlan workflowaccess.Source, sourceFacts commonModels.JSONMap) (output *Model3DGLBExecutionResult, buildErr error) {
	fileSource := req.Config.ConfigVersion == managerModels.Model3DGLBFileConfigVersion
	if fileSource {
		if req.SourceAuthorization == nil {
			return nil, ErrModel3DGLBSourceAuthorizationRequired
		}
		if err := req.SourceAuthorization.ValidateConversion(req.Config); err != nil {
			return nil, err
		}
		if err := req.SourceAuthorization.Checkpoint(ctx); err != nil {
			return nil, err
		}
	}
	bucket, objectName, err := rastercogref.ObjectLocation(req.Config.Result.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, err
	}
	if err := e.ensureTargetBucket(ctx, bucket); err != nil {
		return nil, err
	}
	targetAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{
		"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL,
	}, bucket, objectName, workflowaccess.KindFile)
	if err != nil {
		return nil, fmt.Errorf("prepare model 3d GLB target access: %w", err)
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, operatorName)
	if err != nil {
		return nil, err
	}
	var sgmStage commonModels.JSONMap
	if sourceFormat == string(format.FormatSGM) {
		executionID, err := uuid.Parse(req.ExecutionID)
		if err != nil {
			return nil, fmt.Errorf("SGM conversion requires a valid execution ID: %w", err)
		}
		sgmEngine, sgmOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, "sgm_to_osgb")
		if err != nil {
			return nil, err
		}
		if err := e.verifyTemporaryModelExpiration(ctx, bucket); err != nil {
			return nil, err
		}
		intermediateObject := fmt.Sprintf("temp/model3d-sgm/tenant_%d/%s/model.osgb", req.Task.TenantID, executionID.String())
		intermediateAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{
			"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL,
		}, bucket, intermediateObject, workflowaccess.KindFile)
		if err != nil {
			return nil, err
		}
		defer func() {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 30*time.Second)
			defer cancel()
			if err := e.objectStore.RemoveObject(cleanupCtx, bucket, intermediateObject, minio.RemoveObjectOptions{}); err != nil {
				output = nil
				buildErr = errors.Join(buildErr, fmt.Errorf("clean temporary SGM OSGB: %w", err))
			}
		}()
		sgmPlan, err := workflowaccess.New(sourcePlan, workflowaccess.Target{
			Kind: workflowaccess.KindFile, Format: "osgb", Name: "model.osgb", WriteMode: workflowaccess.WriteModeCreate,
			ContentType: "application/octet-stream", Access: intermediateAccess,
		})
		if err != nil {
			return nil, err
		}
		sgmResult, err := dbbridge.InvokeOperator(ctx, &sgmEngine, sgmOperator.Name, plugin.OperatorInvokeRequest{
			Params: map[string]interface{}{"access_plan": sgmPlan.JSONMap()}, Timeout: e.invokeTimeout,
		})
		if err != nil || sgmResult == nil || (sgmResult.Status != "" && sgmResult.Status != "success") {
			return nil, operatorInvokeError("invoke SGM to OSGB operator", sgmResult, err)
		}
		sgmStage = commonModels.JSONMap{
			"access_plan": sgmPlan.AuditJSONMap(), "engine_id": sgmEngine.ID, "operator": sgmOperator.Name,
			"execution_id": sgmResult.ExecutionID, "facts": operatorInvokeJSONFacts(sgmResult),
		}
		sourcePlan = workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "osgb", Access: intermediateAccess}
	}
	writeMode := workflowaccess.WriteModeReplace
	if fileSource {
		writeMode = workflowaccess.WriteModeCreate
	}
	accessPlan, err := workflowaccess.New(
		sourcePlan,
		workflowaccess.Target{
			Kind: workflowaccess.KindFile, Format: "glb", Name: safeGLBFileName(req.Config.Result.FileName), WriteMode: writeMode,
			ContentType: "model/gltf-binary", Access: targetAccess, Metadata: commonModels.JSONMap{"storage_ref": req.Config.Result.StorageRef},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build model 3d GLB access plan: %w", err)
	}
	if fileSource {
		if err := req.SourceAuthorization.ValidateConversion(req.Config); err != nil {
			return nil, err
		}
		if err := req.SourceAuthorization.Checkpoint(ctx); err != nil {
			return nil, err
		}
	}
	options := req.Config.Options
	if !fileSource {
		options = req.Config.Options.Clone()
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan.JSONMap(),
			"options":     options,
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil || invokeResult == nil {
		return nil, operatorInvokeError("invoke model 3d to GLB operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("model 3d to GLB direct operator invocation failed", invokeResult, nil)
	}
	info, err := e.objectStore.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat model 3d GLB GLB: %w", err)
	}

	facts := operatorInvokeJSONFacts(invokeResult)
	if fileSource {
		if err := validateFileModel3DGLBPublication(invokeResult, info.Size, bucket, objectName); err != nil {
			return nil, err
		}
	}
	sizeBytes := info.Size
	if !fileSource {
		sizeBytes = firstPositiveInt64(info.Size, jsonInt64(facts["size_bytes"]), req.Config.Source.SourceSizeBytes)
	}
	result := &Model3DGLBExecutionResult{
		StorageRef: req.Config.Result.StorageRef,
		FileName:   safeGLBFileName(req.Config.Result.FileName),
		SizeBytes:  sizeBytes,
		ContentURL: "",
		Metadata: commonModels.JSONMap{
			"access_plan": accessPlan.AuditJSONMap(),
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
			"glb_facts": facts,
			"artifact": commonModels.JSONMap{
				"bucket":       bucket,
				"object":       objectName,
				"storage_ref":  req.Config.Result.StorageRef,
				"content_type": "model/gltf-binary",
			},
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	if sgmStage != nil {
		result.Metadata["sgm_conversion"] = sgmStage
	}
	if fileSource {
		if err := req.SourceAuthorization.BeforePublish(ctx); err != nil {
			return nil, err
		}
	}
	return result, nil
}

func (e *ManagerModel3DGLBExecutor) lookupModel3DSourceEngine(ctx context.Context, tenantID, engineID uint) (*commonModels.Engine, error) {
	engine, err := e.systemClient.GetEngineForTenant(ctx, tenantID, engineID)
	if err != nil {
		return nil, fmt.Errorf("get source engine: %w", err)
	}
	if engine == nil || engine.EngineType == "" || engine.EngineType != strings.TrimSpace(engine.EngineType) || engine.EngineType != strings.ToLower(engine.EngineType) {
		return nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	if engine.ID != engineID || (engine.TenantID != nil && *engine.TenantID != tenantID) {
		return nil, ErrEngineAccessDenied
	}
	return engine, nil
}

func (e *ManagerModel3DGLBExecutor) prepareSourceForEngine(ctx context.Context, engine *commonModels.Engine, source Model3DGLBSourceConfig, sourceFormat string) (workflowaccess.Source, commonModels.JSONMap, error) {
	if engine == nil || managedModel3DFileSource(engine.EngineType, sourceFormat) {
		return workflowaccess.Source{}, nil, ErrModel3DGLBSourceAuthorizationRequired
	}
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return workflowaccess.Source{}, nil, fmt.Errorf("parse model 3d GLB source locator: %w", err)
	}
	kind := workflowaccess.KindFile
	entrypoint := ""
	if model3DGLBUsesDirectorySource(sourceFormat) {
		if len(loc.Path) == 0 {
			return workflowaccess.Source{}, nil, errors.New("model 3d GLB source locator has no entrypoint")
		}
		entrypoint = loc.Path[len(loc.Path)-1]
		loc = loc.Clone()
		loc.Path = append([]string{}, loc.Path[:len(loc.Path)-1]...)
		loc.Type = resourcetree.TypeDirectory
		kind = workflowaccess.KindDirectory
	}
	resolved, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: engine, Locator: loc, Kind: kind, Format: sourceFormat, Entrypoint: entrypoint,
		Metadata: commonModels.JSONMap{"item_locator": source.ItemLocator, "engine_id": source.SourceEngineID, "engine_type": engine.EngineType},
	})
	if err != nil {
		return workflowaccess.Source{}, nil, fmt.Errorf("prepare model 3d GLB source access: %w", err)
	}
	return resolved, commonModels.JSONMap{
		"engine_type": engine.EngineType, "access_method": resolved.Access.Method,
	}, nil
}

func model3DGLBUsesDirectorySource(sourceFormat string) bool {
	switch format.NormalizeFormat(sourceFormat) {
	case format.FormatGLTF, format.FormatOBJ, format.FormatDAE, format.Format3DS, format.FormatMAX:
		return true
	default:
		return false
	}
}

func model3DGLBOperatorForFormat(sourceFormat string) (operatorName string, normalizedFormat string, err error) {
	switch format.NormalizeFormat(sourceFormat) {
	case format.FormatOSGB, format.FormatSGM:
		return "osgb_to_glb", string(format.NormalizeFormat(sourceFormat)), nil
	case format.FormatGLTF:
		return "gltf_to_glb", string(format.FormatGLTF), nil
	case format.FormatFBX:
		return "fbx_to_glb", string(format.FormatFBX), nil
	case format.FormatOBJ:
		return "obj_to_glb", string(format.FormatOBJ), nil
	case format.FormatSTL:
		return "stl_to_glb", string(format.FormatSTL), nil
	case format.FormatDAE:
		return "dae_to_glb", string(format.FormatDAE), nil
	case format.Format3DS:
		return "3ds_to_glb", string(format.Format3DS), nil
	case format.FormatMAX:
		return "max_to_glb", string(format.FormatMAX), nil
	case format.FormatSKP:
		return "skp_to_glb", string(format.FormatSKP), nil
	case format.FormatIFC:
		return "ifc_to_glb", string(format.FormatIFC), nil
	default:
		return "", "", fmt.Errorf("model 3d GLB source format %q is not supported", strings.TrimSpace(sourceFormat))
	}
}

func (e *ManagerModel3DGLBExecutor) ensureTargetBucket(ctx context.Context, bucket string) error {
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check model 3d GLB bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create model 3d GLB bucket: %w", err)
	}
	return nil
}

// Infra owns lifecycle configuration. Verify it on every temporary-model use without
// read-modify-writing the bucket's rules. Immediate cleanup cannot cover a late
// remote upload after HTTP cancellation or a Manager process exit.
func (e *ManagerModel3DGLBExecutor) verifyTemporaryModelExpiration(ctx context.Context, bucket string) error {
	versioning, err := e.objectStore.GetBucketVersioning(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check model temporary bucket versioning: %w", err)
	}
	if versioning.Status != "" {
		return errors.New("model temporary bucket must be unversioned for permanent object expiration")
	}
	config, err := e.objectStore.GetBucketLifecycle(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check model temporary object expiration: %w", err)
	}
	if config != nil {
		for _, rule := range config.Rules {
			filter := rule.RuleFilter
			if rule.Status == "Enabled" && rule.Prefix == "" && filter.Prefix == "temp/" &&
				filter.And.IsEmpty() && filter.Tag.IsEmpty() &&
				filter.ObjectSizeLessThan == 0 && filter.ObjectSizeGreaterThan == 0 &&
				rule.Expiration.Days == 7 && rule.Expiration.Date.IsZero() {
				return nil
			}
		}
	}
	return errors.New("model temporary objects require the Infra temp/ 7-day expiration rule on the target bucket")
}

func (e *ManagerModel3DGLBExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for model 3d GLB generation: %w", operatorName, err)
	}
	return engine, operator, nil
}
