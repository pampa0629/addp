package service

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"path"
	"reflect"
	"strings"
	"time"

	commonapi "github.com/addp/common/api"
	"github.com/addp/common/client"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/objectstore"
	"github.com/addp/common/execution"
	"github.com/addp/common/inference"
	commonmodels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	protection "github.com/addp/manager/internal/protection"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
)

type ImageSegmentationTaskService struct {
	repo  *repository.ImageSegmentationTaskRepository
	runs  *execution.TaskExecutionRepository
	image *ImageSegmentationService
	meta  *client.MetaClient
}

func NewImageSegmentationTaskService(repo *repository.ImageSegmentationTaskRepository, runs *execution.TaskExecutionRepository, image *ImageSegmentationService, meta *client.MetaClient) *ImageSegmentationTaskService {
	return &ImageSegmentationTaskService{repo, runs, image, meta}
}

func (s *ImageSegmentationTaskService) GetByID(ctx context.Context, id, tenant uint) (*models.TaskDefinition, error) {
	return s.repo.Get(ctx, tenant, execution.TaskTypeImageSegmentation, id)
}

func (s *ImageSegmentationTaskService) List(ctx context.Context, tenant uint, page, size int) ([]*models.TaskDefinition, int64, error) {
	return s.repo.List(ctx, repository.TaskDefinitionFilter{TenantID: tenant, TaskType: execution.TaskTypeImageSegmentation, Page: page, PageSize: size})
}

func (s *ImageSegmentationTaskService) Delete(ctx context.Context, id, tenant uint) error {
	return s.repo.Delete(ctx, tenant, execution.TaskTypeImageSegmentation, id)
}

func (s *ImageSegmentationTaskService) Create(ctx context.Context, task *models.TaskDefinition) error {
	if err := s.normalize(ctx, task); err != nil {
		return err
	}
	return s.repo.Create(ctx, task)
}

func (s *ImageSegmentationTaskService) Update(ctx context.Context, task *models.TaskDefinition) error {
	if err := s.normalize(ctx, task); err != nil {
		return err
	}
	return s.repo.Update(ctx, task)
}

func (s *ImageSegmentationTaskService) normalize(ctx context.Context, task *models.TaskDefinition) error {
	sourceURI, _ := task.Config["source_locator"].(string)
	parentURI, _ := task.Config["target_parent_locator"].(string)
	name, _ := task.Config["target_name"].(string)
	prompt, _ := task.Config["text"].(string)
	name, prompt = strings.TrimSpace(name), strings.TrimSpace(prompt)
	if len(task.Config) != 4 || task.Name == "" || prompt == "" || len(prompt) > 256 || path.Base(name) != name ||
		strings.Contains(name, "\\") || strings.ToLower(path.Ext(name)) != ".geojson" || name == ".geojson" {
		return fmt.Errorf("%w: source_locator, target_parent_locator, target_name and text are required", commonapi.ErrBadRequest)
	}
	source, item, err := s.image.metadata.resolveStorageDataItem(ctx, sourceURI, &task.TenantID)
	if err != nil {
		return err
	}
	if source.EngineType != "minio" || source.Descriptor.Format != "tiff" || source.Descriptor.DataType != "media" ||
		(source.Descriptor.Layout != "single" && !(len(source.Descriptor.Refs) == 1 && source.Descriptor.Refs[0].Primary && source.Descriptor.Refs[0].Required && source.Descriptor.Refs[0].Path == source.StorageRef)) {
		return fmt.Errorf("%w: expected a single MinIO GeoTIFF", commonapi.ErrBadRequest)
	}
	sourcePath, _, err := streamStorageRefPath(source.EngineType, source.EngineID, source.StorageRef)
	if err != nil {
		return err
	}
	composition, err := authorizationfacts.NewScannedFileCompositionBoundary("tiff", string(source.Descriptor.Layout),
		[]authorizationfacts.FileCompositionRef{{Target: sourcePath, Role: "image", Primary: true, Required: true}})
	if err != nil {
		return err
	}
	parent, err := resourcetree.ParseURI(parentURI)
	if err != nil || (parent.Type != resourcetree.TypeBucket && parent.Type != resourcetree.TypePrefix) || parent.ItemID != nil || len(parent.Path) == 0 {
		return fmt.Errorf("%w: target must select a MinIO bucket or prefix", commonapi.ErrBadRequest)
	}
	for _, part := range parent.Path {
		if part == "" || part == "." || part == ".." || strings.ContainsAny(part, "/\\") {
			return fmt.Errorf("%w: invalid MinIO target path", commonapi.ErrBadRequest)
		}
	}
	target, err := s.image.metadata.getResourceForTenant(ctx, parent.EngineID, &task.TenantID)
	if err != nil {
		return err
	}
	if target.EngineType != "minio" || target.IsBuiltin || target.TenantID == nil || *target.TenantID != task.TenantID || !storageCanWrite(target) {
		return commonapi.ErrForbidden
	}
	loc := resourcetree.ResourceLocator{EngineID: parent.EngineID, Path: append(append([]string{}, parent.Path...), name), Type: resourcetree.TypeObject}
	targetPath := plugin.ObjectItemPath(loc.EngineID, loc.Path[0], strings.Join(loc.Path[1:], "/"))
	binding, err := s.image.binding.ResolveScenario(ctx, task.TenantID, ImageSegmentationScenario)
	if err != nil {
		return ErrSegmentBinding
	}
	cfg := execution.ImageSegmentationConfig{
		Source: execution.ImageSegmentationSource{
			Locator: source.locator, EngineID: source.EngineID, EngineVersion: source.engine.Version, ItemID: item.ID, ItemFingerprint: source.ItemFingerprint, Composition: *composition,
		},
		Target: execution.ImageSegmentationTarget{
			Locator: loc.ToURI(), EngineID: target.ID, EngineVersion: target.Version, Path: targetPath,
		},
		Text: prompt, ModelProfileID: binding.ModelProfileID,
	}
	if err := cfg.Validate(); err != nil {
		return fmt.Errorf("%w: %v", commonapi.ErrBadRequest, err)
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	task.Config = nil
	return json.Unmarshal(raw, &task.Config)
}

func (s *ImageSegmentationTaskService) Execute(ctx context.Context, taskID, tenant uint, trigger, source string, parent *string) (string, error) {
	if trigger != execution.TriggerTypeManual {
		return "", fmt.Errorf("%w: image segmentation only supports manual execution", commonapi.ErrBadRequest)
	}
	now := time.Now().UTC()
	run := &execution.TaskExecution{ExecutionID: uuid.NewString(), TenantID: int(tenant), Module: execution.ModuleManager,
		TaskType: execution.TaskTypeImageSegmentation, TriggerType: trigger, Source: source, ParentExecutionID: parent,
		Status: execution.ExecutionStatusPending, CreatedAt: now, UpdatedAt: now}
	if parent == nil {
		if err := execution.ApplyUserOrigin(ctx, run); err != nil {
			return "", err
		}
	}
	if err := s.repo.Enqueue(ctx, taskID, tenant, run); err != nil {
		switch {
		case errors.Is(err, commonapi.ErrNotFound):
			return "", ErrTaskNotFound
		case errors.Is(err, commonapi.ErrConflict):
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}
	return run.ExecutionID, nil
}

func (s *ImageSegmentationTaskService) Run(ctx context.Context, taskID uint, run *execution.TaskExecution, lease execution.Lease) error {
	metadata, err := s.generate(ctx, run, lease)
	fields := map[string]any{"status": execution.ExecutionStatusSuccess, "progress": 100, "metadata": metadata}
	if err != nil {
		fields["status"], fields["progress"] = execution.ExecutionStatusFailed, 0
		fields["error_details"] = commonmodels.JSONMap{"message": err.Error()}
	}
	return s.repo.Complete(ctx, taskID, uint(run.TenantID), run.ExecutionID, fields)
}

func (s *ImageSegmentationTaskService) generate(ctx context.Context, run *execution.TaskExecution, lease execution.Lease) (commonmodels.JSONMap, error) {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Minute)
	defer cancel()
	var cfg execution.ImageSegmentationConfig
	raw, err := json.Marshal(run.ExecutionConfig)
	if err != nil || json.Unmarshal(raw, &cfg) != nil || cfg.Validate() != nil {
		return nil, errors.New("invalid frozen image segmentation inputs")
	}
	tenant := uint(run.TenantID)
	access := func() (*client.ImageSegmentationAccessObservation, error) {
		reply, err := s.image.source.WithTenantID(tenant).CheckImageSegmentationAccess(ctx, client.ImageSegmentationAccessRequest{
			ExecutionID: run.ExecutionID, Attempt: lease.Attempt, LeaseToken: lease.Token,
		})
		if err != nil {
			return nil, errors.New("System rejected image segmentation resource access")
		}
		source, _, err := s.image.metadata.resolveStorageDataItem(ctx, cfg.Source.Locator, &tenant)
		if err != nil || source.ItemFingerprint != cfg.Source.ItemFingerprint ||
			protection.RequireUnmanagedDataItem(ctx, s.image.protection, tenant, cfg.Source.ItemFingerprint, time.Now().UTC()) != nil {
			return nil, errors.New("image segmentation source identity or protection changed")
		}
		return reply, nil
	}
	observed, err := access()
	if err != nil {
		return nil, err
	}
	primary, _ := cfg.Source.Composition.PrimaryTarget()
	storageRef, err := plugin.RequireObjectLeafPath(primary)
	if err != nil {
		return nil, err
	}
	minioPlugin, err := plugin.Get("minio")
	if err != nil {
		return nil, err
	}
	factsProvider := minioPlugin.(plugin.EngineCatalogFactsProvider)
	facts := func(engine *commonmodels.Engine) (*plugin.StorageObjectFacts, error) {
		return streamEngineCatalogFacts(ctx, factsProvider, plugin.ConnectionInfo(engine.ConnectionInfo), primary, storageRef)
	}
	version, err := facts(observed.SourceEngine)
	if err != nil || version == nil || (version.ETag == "" && version.ModifiedAt.IsZero()) || version.Size <= 0 || version.Size > maxSegmentSourceBytes {
		return nil, errors.New("image segmentation source is missing or exceeds its size limit")
	}
	reader, _, _, _, err := s.image.metadata.streamStorageContent(ctx, observed.SourceEngine, storageRef, "", maxSegmentSourceBytes, func() error {
		_, err := access()
		return err
	})
	if err != nil {
		return nil, errors.New("could not open image segmentation source")
	}
	sourceBytes, readErr := io.ReadAll(io.LimitReader(reader, maxSegmentSourceBytes+1))
	_ = reader.Close()
	if readErr != nil || int64(len(sourceBytes)) != version.Size {
		return nil, errors.New("could not read a complete image segmentation source")
	}
	ref, err := readSegmentGeoReference(sourceBytes)
	if err != nil {
		return nil, err
	}
	pngData, window, err := renderSegmentWindow(bytes.NewReader(sourceBytes), "tiff", nil)
	if err != nil {
		return nil, err
	}
	checkRevision := func() (*client.ImageSegmentationAccessObservation, error) {
		current, err := access()
		if err != nil {
			return nil, err
		}
		got, err := facts(current.SourceEngine)
		if err != nil || !reflect.DeepEqual(got, version) {
			return nil, errors.New("image segmentation source changed during execution")
		}
		return current, nil
	}
	if _, err := checkRevision(); err != nil {
		return nil, err
	}
	select {
	case s.image.prepareSlots <- struct{}{}:
	default:
		return nil, ErrSegmentBusy
	}
	scope := fmt.Sprintf("%x", sha256.Sum256([]byte(run.ExecutionID)))
	req := inference.SegmentRequest{SchemaVersion: inference.SchemaVersion, TenantID: tenant, ModelProfileID: cfg.ModelProfileID,
		Action: "prepare", Scope: scope, Image: base64.StdEncoding.EncodeToString(pngData)}
	prepared, err := s.image.inference.Segment(ctx, req)
	<-s.image.prepareSlots
	if err != nil {
		return nil, ErrSegmentUpstream
	}
	req.Image, req.SessionID = "", prepared.SessionID
	defer func() {
		releaseCtx, stop := context.WithTimeout(context.WithoutCancel(ctx), 3*time.Second)
		defer stop()
		req.Action, req.Prompt = "release", nil
		_, _ = s.image.inference.Segment(releaseCtx, req)
	}()
	req.Action, req.Prompt = "prompt", &inference.SegmentPrompt{Text: cfg.Text}
	result, err := s.image.inference.Segment(ctx, req)
	if err != nil || prepared.ProfileVersion != result.ProfileVersion || prepared.DeploymentID != result.DeploymentID ||
		prepared.Width != result.Width || prepared.Height != result.Height {
		return nil, ErrSegmentUpstream
	}
	vectors, count, err := segmentVectors(result.Masks, ref, window, result.Width, result.Height)
	if err != nil {
		return nil, err
	}
	current, err := checkRevision()
	if err != nil {
		return nil, err
	}
	catalogPath, err := plugin.RequireObjectLeafPath(cfg.Target.Path)
	if err != nil {
		return nil, err
	}
	bucket, key := objectstore.SplitBucketPrefix(catalogPath)
	dst, err := objectstore.NewClient(plugin.ConnectionInfo(current.TargetEngine.ConnectionInfo), false, true)
	if err != nil {
		return nil, errors.New("could not connect to image segmentation target")
	}
	opts := minio.PutObjectOptions{ContentType: "application/geo+json", DisableMultipart: true}
	opts.SetMatchETagExcept("*")
	if _, err := dst.PutObject(ctx, bucket, key, bytes.NewReader(vectors), int64(len(vectors)), opts); err != nil {
		if minio.ToErrorResponse(err).StatusCode == 412 {
			return nil, errors.New("image segmentation target already exists")
		}
		return nil, errors.New("could not confirm image segmentation target creation")
	}
	metadata := commonmodels.JSONMap{"catalog_path": catalogPath, "produced_target": client.MetaScanProducedTarget{
		EngineID: cfg.Target.EngineID, CatalogPath: catalogPath, Attempt: lease.Attempt,
	}, "instance_count": count, "model_profile_id": cfg.ModelProfileID, "model_profile_version": result.ProfileVersion,
		"model_deployment_id": result.DeploymentID, "source_sha256": fmt.Sprintf("%x", sha256.Sum256(sourceBytes)),
		"source_affine": ref.affine, "source_epsg": ref.epsg, "source_window": window,
		"model_image_width": result.Width, "model_image_height": result.Height}
	if err := s.runs.UpdateWithLease(ctx, lease, map[string]any{"metadata": metadata}); err != nil {
		return metadata, err
	}
	scan, err := s.meta.CreateManualScanRunForTenant(ctx, tenant, client.MetaScanOptions{
		EngineID: cfg.Target.EngineID, RefGroups: []client.MetaScanRefGroup{{Primary: catalogPath}},
		ParentExecutionID: run.ExecutionID, Source: execution.ModuleManager, Force: true, ScanDepth: client.MetaScanDepthDeep,
	})
	if err != nil || scan == nil || scan.ExecutionID == "" {
		return metadata, errors.New("image segmentation result created but Meta scan was not accepted")
	}
	metadata = managerExecutionLineage(metadata, execution.TaskTypeImageSegmentation,
		[]execution.LineageResourceRef{managerItemLineageRef(cfg.Source.Locator, cfg.Source.ItemFingerprint, cfg.Source.ItemID)},
		[]execution.LineageResourceRef{managerResourceLineageRef("resource", cfg.Target.Locator)}, scan.ExecutionID)
	ticker := time.NewTicker(500 * time.Millisecond)
	defer ticker.Stop()
	for {
		status, err := s.runs.GetByExecutionID(ctx, scan.ExecutionID, run.TenantID)
		if err != nil || status == nil || status.Module != execution.ModuleMeta {
			return metadata, errors.New("could not observe image segmentation result scan")
		}
		switch status.Status {
		case execution.ExecutionStatusSuccess:
			metadata["outputs"] = commonmodels.JSONMap{"resource": commonmodels.JSONMap{"locator": cfg.Target.Locator, "type": "object", "write_mode": "create"}}
			return metadata, nil
		case execution.ExecutionStatusPending, execution.ExecutionStatusRunning:
		default:
			return metadata, errors.New("image segmentation result scan failed")
		}
		select {
		case <-ctx.Done():
			return metadata, ctx.Err()
		case <-ticker.C:
		}
	}
}
