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
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/addp/manager/internal/engineaccess"
	"github.com/minio/minio-go/v7"
)

type pptxPDFObjectStore interface {
	BucketExists(context.Context, string) (bool, error)
	MakeBucket(context.Context, string, minio.MakeBucketOptions) error
	StatObject(context.Context, string, string, minio.StatObjectOptions) (minio.ObjectInfo, error)
}

type PPTXPDFExecutionRequest struct {
	TenantID        uint
	SourceEngineID  uint
	ItemID          uint
	ItemFingerprint string
	Locator         string
	SourceVersion   string
	SourceSizeBytes int64
	StorageRef      string
	FileName        string
}

type PPTXPDFExecutionResult struct {
	StorageRef string
	FileName   string
	SizeBytes  int64
	PageCount  int
	Metadata   commonModels.JSONMap
}

type PPTXPDFExecutor interface {
	BuildPPTXPDF(context.Context, PPTXPDFExecutionRequest) (*PPTXPDFExecutionResult, error)
}

type ManagerPPTXPDFExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     pptxPDFObjectStore
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerPPTXPDFExecutor(systemClient *commonClient.SystemClient, workflowEngines workflowEngineLister, objectStore pptxPDFObjectStore, endpoint, accessKey, secretKey string, useSSL bool, bucket string, timeout time.Duration) *ManagerPPTXPDFExecutor {
	if timeout <= 0 {
		timeout = 30 * time.Minute
	}
	return &ManagerPPTXPDFExecutor{systemClient: systemClient, workflowEngines: workflowEngines, objectStore: objectStore, minioEndpoint: strings.TrimSpace(endpoint), minioAccessKey: accessKey, minioSecretKey: secretKey, minioUseSSL: useSSL, defaultBucket: strings.TrimSpace(bucket), invokeTimeout: timeout}
}

func (e *ManagerPPTXPDFExecutor) BuildPPTXPDF(ctx context.Context, req PPTXPDFExecutionRequest) (*PPTXPDFExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("PPTX PDF executor is not fully configured")
	}
	loc, err := resourcetree.ParseURI(req.Locator)
	if err != nil {
		return nil, fmt.Errorf("parse PPTX source locator: %w", err)
	}
	engine, err := e.systemClient.GetEngineForTenant(ctx, req.TenantID, req.SourceEngineID)
	if err != nil {
		return nil, fmt.Errorf("get PPTX source engine: %w", err)
	}
	if err := engineaccess.EnsureAvailable(engine); err != nil {
		return nil, err
	}
	if engine.TenantID != nil && *engine.TenantID != req.TenantID {
		return nil, ErrEngineAccessDenied
	}
	source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{
		Engine: engine, Locator: loc, Kind: workflowaccess.KindFile, Format: "pptx",
		Metadata: commonModels.JSONMap{"item_id": req.ItemID, "item_fingerprint": req.ItemFingerprint, "source_version": req.SourceVersion, "source_size_bytes": req.SourceSizeBytes},
	})
	if err != nil {
		return nil, fmt.Errorf("prepare PPTX source access: %w", err)
	}
	bucket, objectName, err := rastercogref.ObjectLocation(req.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, err
	}
	exists, err := e.objectStore.BucketExists(ctx, bucket)
	if err != nil {
		return nil, fmt.Errorf("check PPTX PDF bucket: %w", err)
	}
	if !exists {
		if err := e.objectStore.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
			return nil, fmt.Errorf("create PPTX PDF bucket: %w", err)
		}
	}
	targetAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL}, bucket, objectName, workflowaccess.KindFile)
	if err != nil {
		return nil, fmt.Errorf("prepare PPTX PDF target access: %w", err)
	}
	accessPlan, err := workflowaccess.New(source, workflowaccess.Target{Kind: workflowaccess.KindFile, Format: "pdf", Name: req.FileName, WriteMode: workflowaccess.WriteModeReplace, ContentType: "application/pdf", Access: targetAccess, Metadata: commonModels.JSONMap{"storage_ref": req.StorageRef}})
	if err != nil {
		return nil, fmt.Errorf("build PPTX PDF access plan: %w", err)
	}
	engines, err := e.workflowEngines.ListWorkflowEngines(req.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list document workflow engines: %w", err)
	}
	runtime, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{OperatorName: "document_to_pdf", EngineType: "document_workflow"})
	if err != nil {
		return nil, fmt.Errorf("document_to_pdf runtime is unavailable: %w", err)
	}
	invokeResult, err := dbbridge.InvokeOperator(ctx, &runtime, operator.Name, plugin.OperatorInvokeRequest{Params: map[string]interface{}{"access_plan": accessPlan.JSONMap(), "options": map[string]interface{}{"strip_embedded_media": true}}, Timeout: e.invokeTimeout})
	if err != nil {
		return nil, operatorInvokeError("invoke document_to_pdf operator", invokeResult, err)
	}
	if invokeResult.Status != "" && invokeResult.Status != "success" {
		return nil, operatorInvokeError("document_to_pdf direct invocation failed", invokeResult, nil)
	}
	info, err := e.objectStore.StatObject(ctx, bucket, objectName, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("stat PPTX PDF output: %w", err)
	}
	facts := operatorInvokeJSONFacts(invokeResult)
	return &PPTXPDFExecutionResult{
		StorageRef: req.StorageRef, FileName: req.FileName, SizeBytes: firstPositiveInt64(info.Size, jsonInt64(facts["size_bytes"])), PageCount: int(jsonInt64(facts["page_count"])),
		Metadata: commonModels.JSONMap{
			"access_plan":      accessPlan.AuditJSONMap(),
			"workflow_runtime": commonModels.JSONMap{"engine_id": runtime.ID, "engine_name": runtime.Name, "engine_type": runtime.EngineType, "execution_id": invokeResult.ExecutionID, "operator": operator.Name, "mode": "direct"},
			"pdf_facts":        facts,
			"artifact":         commonModels.JSONMap{"bucket": bucket, "object": objectName, "storage_ref": req.StorageRef, "content_type": "application/pdf"},
		},
	}, nil
}
