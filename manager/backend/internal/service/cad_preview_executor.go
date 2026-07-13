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
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	rastercogref "github.com/addp/manager/internal/cog"
	"github.com/minio/minio-go/v7"
)

type ManagerCADPreviewExecutor struct {
	systemClient    *commonClient.SystemClient
	workflowEngines workflowEngineLister
	objectStore     *minio.Client
	minioEndpoint   string
	minioAccessKey  string
	minioSecretKey  string
	minioUseSSL     bool
	defaultBucket   string
	invokeTimeout   time.Duration
}

func NewManagerCADPreviewExecutor(systemClient *commonClient.SystemClient, workflowEngines workflowEngineLister, objectStore *minio.Client, endpoint, accessKey, secretKey string, useSSL bool, bucket string, timeout time.Duration) *ManagerCADPreviewExecutor {
	if timeout <= 0 {
		timeout = 2 * time.Hour
	}
	return &ManagerCADPreviewExecutor{systemClient: systemClient, workflowEngines: workflowEngines, objectStore: objectStore, minioEndpoint: strings.TrimSpace(endpoint), minioAccessKey: accessKey, minioSecretKey: secretKey, minioUseSSL: useSSL, defaultBucket: strings.TrimSpace(bucket), invokeTimeout: timeout}
}

func (e *ManagerCADPreviewExecutor) BuildCADPreview(ctx context.Context, req CADPreviewExecutionRequest) (*CADPreviewExecutionResult, error) {
	if e == nil || e.systemClient == nil || e.workflowEngines == nil || e.objectStore == nil {
		return nil, errors.New("CAD preview executor is not fully configured")
	}
	if req.Task == nil {
		return nil, errors.New("CAD preview task is required")
	}
	loc, err := resourcetree.ParseURI(req.Config.Source.ItemLocator)
	if err != nil {
		return nil, fmt.Errorf("parse CAD source locator: %w", err)
	}
	sourceEngine, err := e.systemClient.GetEngine(req.Config.Source.SourceEngineID)
	if err != nil {
		return nil, fmt.Errorf("get CAD source engine: %w", err)
	}
	if sourceEngine == nil || !sourceEngine.IsActive {
		return nil, errors.New("CAD source engine is not active")
	}
	if sourceEngine.TenantID != nil && *sourceEngine.TenantID != req.Task.TenantID {
		return nil, ErrEngineAccessDenied
	}
	source, err := workflowaccess.ResolveSource(workflowaccess.ResourceSpec{Engine: sourceEngine, Locator: loc, Kind: workflowaccess.KindFile, Format: "dwg"})
	if err != nil {
		return nil, fmt.Errorf("resolve CAD source access: %w", err)
	}

	bucket, prefix, err := rastercogref.ObjectLocation(req.Config.Result.StorageRef, e.defaultBucket)
	if err != nil {
		return nil, fmt.Errorf("resolve CAD preview storage: %w", err)
	}
	prefix = strings.Trim(prefix, "/")
	if prefix == "" {
		return nil, errors.New("CAD preview storage prefix is required")
	}
	if err := ensureMinIOBucket(ctx, e.objectStore, bucket); err != nil {
		return nil, err
	}
	if err := deleteMinIOPrefix(ctx, e.objectStore, bucket, prefix); err != nil {
		return nil, err
	}
	targetAccess, err := workflowaccess.ResolveObjectStoreTarget(plugin.ConnectionInfo{"endpoint": e.minioEndpoint, "access_key": e.minioAccessKey, "secret_key": e.minioSecretKey, "use_ssl": e.minioUseSSL}, bucket, prefix, workflowaccess.KindDirectory)
	if err != nil {
		return nil, fmt.Errorf("prepare CAD preview target access: %w", err)
	}
	plan, err := workflowaccess.New(source, workflowaccess.Target{Kind: workflowaccess.KindDirectory, Format: "cad_preview", Name: path.Base(prefix), WriteMode: workflowaccess.WriteModeReplace, Access: targetAccess})
	if err != nil {
		return nil, err
	}
	engines, err := e.workflowEngines.ListWorkflowEngines(req.Task.TenantID)
	if err != nil {
		return nil, fmt.Errorf("list workflow engines: %w", err)
	}
	workflowEngine, operator, err := dbbridge.ResolveDirectWorkflowOperator(ctx, engines, dbbridge.DirectWorkflowOperatorSelector{OperatorName: "cad.render_preview"})
	if err != nil {
		return nil, err
	}
	invoked, err := dbbridge.InvokeOperator(ctx, &workflowEngine, operator.Name, plugin.OperatorInvokeRequest{Params: map[string]interface{}{
		"access_plan": plan.JSONMap(), "tile_size": req.Config.Options.TileSize, "max_zoom": req.Config.Options.MaxZoom,
	}, Timeout: e.invokeTimeout})
	if err != nil {
		return nil, operatorInvokeError("invoke CAD preview renderer", invoked, err)
	}
	facts := operatorInvokeJSONFacts(invoked)
	manifestRef := firstNonEmptyConfig(stringFromConfig(facts["manifest_ref"]), "manifest.json")
	thumbnailRef := firstNonEmptyConfig(stringFromConfig(facts["thumbnail_ref"]), "thumbnail.webp")
	if err := validateCADPreviewArtifactRef(manifestRef); err != nil {
		return nil, err
	}
	if err := validateCADPreviewArtifactRef(thumbnailRef); err != nil {
		return nil, err
	}
	if _, err := e.objectStore.StatObject(ctx, bucket, path.Join(prefix, manifestRef), minio.StatObjectOptions{}); err != nil {
		return nil, fmt.Errorf("stat CAD preview manifest: %w", err)
	}
	bounds, _ := asJSONMap(facts["bounds_2d"])
	if bounds == nil {
		bounds = commonModels.JSONMap{}
	}
	return &CADPreviewExecutionResult{
		StorageRef: req.Config.Result.StorageRef, ManifestRef: manifestRef, ThumbnailRef: thumbnailRef,
		TileCount: jsonInt64(facts["tile_count"]), TileSize: req.Config.Options.TileSize, MinZoom: 0, MaxZoom: req.Config.Options.MaxZoom,
		Bounds: bounds, Metadata: commonModels.JSONMap{
			"access_plan":      plan.AuditJSONMap(),
			"workflow_runtime": commonModels.JSONMap{"engine_id": workflowEngine.ID, "engine_name": workflowEngine.Name, "operator": operator.Name, "mode": "direct", "execution_id": invoked.ExecutionID},
			"render_facts":     facts,
		},
	}, nil
}

func validateCADPreviewArtifactRef(ref string) error {
	ref = strings.TrimSpace(strings.ReplaceAll(ref, "\\", "/"))
	if ref == "" || strings.HasPrefix(ref, "/") || path.Clean(ref) != ref || ref == "." || ref == ".." || strings.HasPrefix(ref, "../") {
		return fmt.Errorf("invalid CAD preview artifact ref %q", ref)
	}
	return nil
}

type MinIOCADPreviewCleaner struct {
	client *minio.Client
	bucket string
}

func NewMinIOCADPreviewCleaner(client *minio.Client, bucket string) *MinIOCADPreviewCleaner {
	return &MinIOCADPreviewCleaner{client: client, bucket: bucket}
}
func (c *MinIOCADPreviewCleaner) DeleteByStorageRef(ctx context.Context, storageRef string) error {
	if c == nil || c.client == nil {
		return errors.New("CAD preview cleaner is not configured")
	}
	bucket, prefix, err := rastercogref.ObjectLocation(storageRef, c.bucket)
	if err != nil {
		return err
	}
	return deleteMinIOPrefix(ctx, c.client, bucket, prefix)
}

func ensureMinIOBucket(ctx context.Context, client *minio.Client, bucket string) error {
	exists, err := client.BucketExists(ctx, bucket)
	if err != nil {
		return fmt.Errorf("check CAD preview bucket: %w", err)
	}
	if exists {
		return nil
	}
	if err := client.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		return fmt.Errorf("create CAD preview bucket: %w", err)
	}
	return nil
}

func deleteMinIOPrefix(ctx context.Context, client *minio.Client, bucket, prefix string) error {
	prefix = strings.Trim(prefix, "/")
	for object := range client.ListObjects(ctx, bucket, minio.ListObjectsOptions{Prefix: prefix + "/", Recursive: true}) {
		if object.Err != nil {
			return object.Err
		}
		if err := client.RemoveObject(ctx, bucket, object.Key, minio.RemoveObjectOptions{}); err != nil {
			return err
		}
	}
	return nil
}
