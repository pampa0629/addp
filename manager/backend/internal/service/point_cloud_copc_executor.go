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
	"github.com/minio/minio-go/v7"
)

const pointCloudCOPCContentType = "application/vnd.laszip+copc"

type pointCloudCOPCObjectStore interface {
	BucketExists(ctx context.Context, bucketName string) (bool, error)
	MakeBucket(ctx context.Context, bucketName string, opts minio.MakeBucketOptions) error
	StatObject(ctx context.Context, bucketName string, objectName string, opts minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type ManagerPointCloudCOPCExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     pointCloudCOPCObjectStore
	managerBaseURL  string
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerPointCloudCOPCExecutor(
	systemClient *commonClient.SystemClient,
	workflowEngines workflowEngineLister,
	objectStore pointCloudCOPCObjectStore,
	managerBaseURL string,
	minioEndpoint string,
	minioAccessKey string,
	minioSecretKey string,
	minioUseSSL bool,
	defaultBucket string,
	invokeTimeout time.Duration,
) *ManagerPointCloudCOPCExecutor {
	if invokeTimeout <= 0 {
		invokeTimeout = 2 * time.Hour
	}
	return &ManagerPointCloudCOPCExecutor{
		systemClient:    systemClient,
		workflowEngines: workflowEngines,
		objectStore:     objectStore,
		managerBaseURL:  strings.TrimRight(strings.TrimSpace(managerBaseURL), "/"),
		minioEndpoint:   strings.TrimSpace(minioEndpoint),
		minioAccessKey:  minioAccessKey,
		minioSecretKey:  minioSecretKey,
		minioUseSSL:     minioUseSSL,
		defaultBucket:   strings.TrimSpace(defaultBucket),
		invokeTimeout:   invokeTimeout,
	}
}

func (e *ManagerPointCloudCOPCExecutor) BuildPointCloudCOPC(ctx context.Context, req PointCloudCOPCExecutionRequest) (*PointCloudCOPCExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("point cloud COPC generation executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("point cloud COPC generation task is required")
	}
	if strings.TrimSpace(req.ExecutionID) == "" {
		return nil, errors.New("point cloud COPC execution_id is required")
	}
	sourceAccess, sourceFacts, err := e.prepareSourceAccess(ctx, req.Task.TenantID, req.Config.Source)
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

	operatorName, sourceFormat, err := pointCloudCOPCOperatorForFormat(req.Config.Source.Format)
	if err != nil {
		return nil, err
	}
	targetAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{
		"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL,
	}, bucket, objectName, workflowaccess.KindFile)
	if err != nil {
		return nil, fmt.Errorf("prepare point cloud COPC target access: %w", err)
	}
	accessPlan, err := workflowaccess.New(
		workflowaccess.Source{
			Kind: workflowaccess.KindFile, Format: sourceFormat, Access: sourceAccess, Metadata: pointCloudSourceMetadata(sourceFacts, req.Config.Source),
		},
		workflowaccess.Target{
			Kind: workflowaccess.KindFile, Format: "copc", Name: safeCOPCFileName(req.Config.Result.FileName), WriteMode: workflowaccess.WriteModeReplace,
			ContentType: pointCloudCOPCContentType, Access: targetAccess, Metadata: commonModels.JSONMap{"storage_ref": req.Config.Result.StorageRef},
		},
	)
	if err != nil {
		return nil, fmt.Errorf("build point cloud COPC access plan: %w", err)
	}
	workflowEngine, workflowOperator, err := e.selectDirectWorkflowRuntime(ctx, req.Task.TenantID, operatorName)
	if err != nil {
		return nil, err
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &workflowEngine, workflowOperator.Name, plugin.OperatorInvokeRequest{
		Params: map[string]interface{}{
			"access_plan": accessPlan.JSONMap(),
			"progress_callback": commonModels.JSONMap{
				"endpoint":     e.managerBaseURL + "/api/v1/manager/executions/" + strings.TrimSpace(req.ExecutionID) + "/events",
				"tenant_id":    req.Task.TenantID,
				"execution_id": strings.TrimSpace(req.ExecutionID),
			},
			"options": req.Config.Options.Clone(),
		},
		Timeout: e.invokeTimeout,
	})
	if err != nil {
		return nil, operatorInvokeError("invoke point cloud to COPC operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("point cloud to COPC direct operator invocation failed", invokeResult, nil)
	}
	info, err := e.objectStore.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat point cloud COPC artifact: %w", err)
	}

	facts := operatorInvokeJSONFacts(invokeResult)
	result := &PointCloudCOPCExecutionResult{
		StorageRef: req.Config.Result.StorageRef,
		FileName:   safeCOPCFileName(req.Config.Result.FileName),
		SizeBytes:  firstPositiveInt64(info.Size, jsonInt64(facts["size_bytes"]), req.Config.Source.SourceSizeBytes),
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
			"copc_facts": facts,
			"artifact": commonModels.JSONMap{
				"bucket":       bucket,
				"object":       objectName,
				"storage_ref":  req.Config.Result.StorageRef,
				"content_type": pointCloudCOPCContentType,
				"access": commonModels.JSONMap{
					"engine_type":   "addp_object_storage",
					"access_method": "object_store_upload",
				},
			},
		},
	}
	if invokeResult.ExecutionTimeMs != nil {
		result.Metadata["workflow_runtime"].(commonModels.JSONMap)["execution_time_ms"] = *invokeResult.ExecutionTimeMs
	}
	return result, nil
}

func pointCloudSourceMetadata(sourceFacts commonModels.JSONMap, source PointCloudCOPCSourceConfig) commonModels.JSONMap {
	metadata := commonModels.JSONMap{}
	if sourceFacts != nil {
		metadata = sourceFacts.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
	}
	metadata["source_size_bytes"] = source.SourceSizeBytes
	metadata["item_locator"] = source.ItemLocator
	metadata["item_id"] = source.ItemID
	metadata["item_fingerprint"] = source.ItemFingerprint
	return metadata
}

func (e *ManagerPointCloudCOPCExecutor) prepareSourceAccess(ctx context.Context, tenantID uint, source PointCloudCOPCSourceConfig) (workflowaccess.Access, commonModels.JSONMap, error) {
	loc, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil {
		return workflowaccess.Access{}, nil, fmt.Errorf("parse point cloud COPC source locator: %w", err)
	}
	engine, err := e.systemClient.GetEngineForTenant(ctx, tenantID, source.SourceEngineID)
	if err != nil {
		return workflowaccess.Access{}, nil, fmt.Errorf("get source engine: %w", err)
	}
	if !engine.IsUsable() {
		return workflowaccess.Access{}, nil, errors.New("source engine is not active")
	}
	if engine.TenantID != nil && *engine.TenantID != tenantID {
		return workflowaccess.Access{}, nil, ErrEngineAccessDenied
	}
	resolved, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: engine, Locator: loc, Kind: workflowaccess.KindFile, Format: string(format.NormalizeFormat(source.Format)),
	})
	if err != nil {
		return workflowaccess.Access{}, nil, fmt.Errorf("resolve point cloud source access: %w", err)
	}
	return resolved.Access, commonModels.JSONMap{
		"engine_type":   engine.EngineType,
		"access_method": resolved.Access.Method,
	}, nil
}

func pointCloudCOPCOperatorForFormat(sourceFormat string) (operatorName string, normalizedFormat string, err error) {
	switch format.NormalizeFormat(sourceFormat) {
	case format.FormatLAS:
		return "las_to_copc", string(format.FormatLAS), nil
	case format.FormatLAZ:
		return "laz_to_copc", string(format.FormatLAZ), nil
	case format.FormatE57:
		return "e57_to_copc", string(format.FormatE57), nil
	case format.FormatPCD:
		return "pcd_to_copc", string(format.FormatPCD), nil
	case format.FormatXYZ:
		return "xyz_to_copc", string(format.FormatXYZ), nil
	default:
		return "", "", fmt.Errorf("point cloud COPC source format %q is not supported", strings.TrimSpace(sourceFormat))
	}
}

func (e *ManagerPointCloudCOPCExecutor) ensureTargetBucket(ctx context.Context, bucket string) error {
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check point cloud COPC bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create point cloud COPC bucket: %w", err)
	}
	return nil
}

func (e *ManagerPointCloudCOPCExecutor) selectDirectWorkflowRuntime(ctx context.Context, tenantID uint, operatorName string) (commonModels.Engine, commonModels.OperatorDescriptor, error) {
	engines, err := e.workflowEngines.ListWorkflowEngines(tenantID)
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("list workflow engines: %w", err)
	}
	engine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{
		OperatorName: operatorName,
	})
	if err != nil {
		return commonModels.Engine{}, commonModels.OperatorDescriptor{}, fmt.Errorf("direct workflow operator %s is unavailable for point cloud COPC generation: %w", operatorName, err)
	}
	return engine, operator, nil
}
