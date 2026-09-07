package service

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	commonAPI "github.com/addp/common/api"
	commonClient "github.com/addp/common/client"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/logger"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type VectorTileSetExecutionConfig struct {
	Source                  tileCacheTaskTargetIdentity
	TargetEngineID          uint
	TargetLocator           string
	TargetName              string
	ProfileHash             string
	SourceVersion           string
	ReusableCacheStorageRef string
	Tile                    commonModels.JSONMap
	Options                 commonModels.JSONMap
	Optimization            commonModels.JSONMap
}

type VectorTileSetExecutionRequest struct {
	Task        *models.VectorTileSetTask
	ExecutionID string
	Config      VectorTileSetExecutionConfig
}

type VectorTileSetExecutionResult struct {
	EngineCatalogPath string
	Metadata          commonModels.JSONMap
}

type VectorTileSetExecutor interface {
	GenerateVectorTileSet(context.Context, VectorTileSetExecutionRequest) (*VectorTileSetExecutionResult, error)
}

type VectorTileSetTaskService struct {
	repo              *repository.VectorTileSetRepository
	taskExecRepo      *commonExecution.TaskExecutionRepository
	executor          VectorTileSetExecutor
	metaScanSubmitter RasterMosaicMetaScanSubmitter
	metaClient        *commonClient.MetaClient
	tileCacheRepo     *repository.TileCacheRepository
}

var (
	ErrVectorTileSetProgressTargetMismatch = errors.New("vector tile set progress event target mismatch")
	ErrVectorTileSetExecutionCompleted     = errors.New("vector tile set execution is already completed")
)

func NewVectorTileSetTaskService(repo *repository.VectorTileSetRepository, taskExecRepo *commonExecution.TaskExecutionRepository) *VectorTileSetTaskService {
	return &VectorTileSetTaskService{repo: repo, taskExecRepo: taskExecRepo}
}
func (s *VectorTileSetTaskService) SetExecutor(executor VectorTileSetExecutor) { s.executor = executor }
func (s *VectorTileSetTaskService) SetMetaScanSubmitter(submitter RasterMosaicMetaScanSubmitter) {
	s.metaScanSubmitter = submitter
}
func (s *VectorTileSetTaskService) SetMetaClient(client *commonClient.MetaClient) {
	s.metaClient = client
}
func (s *VectorTileSetTaskService) SetTileCacheRepository(repo *repository.TileCacheRepository) {
	s.tileCacheRepo = repo
}
func (s *VectorTileSetTaskService) Create(ctx context.Context, task *models.VectorTileSetTask) error {
	if err := normalizeVectorTileSetTask(task); err != nil {
		return err
	}
	semanticHash := stringFromConfig(task.Config["semantic_hash"])
	existing, err := s.repo.GetTaskBySemanticHash(ctx, task.TenantID, semanticHash, 0)
	if err != nil {
		return err
	}
	if existing != nil {
		return s.reuseExistingTask(ctx, task, existing)
	}
	if err := s.repo.CreateTask(ctx, task); err != nil {
		if strings.Contains(err.Error(), "idx_vector_tile_set_tasks_semantic_unique") {
			existing, lookupErr := s.repo.GetTaskBySemanticHash(ctx, task.TenantID, semanticHash, 0)
			if lookupErr != nil {
				return lookupErr
			}
			if existing != nil {
				return s.reuseExistingTask(ctx, task, existing)
			}
		}
		return err
	}
	return nil
}
func (s *VectorTileSetTaskService) Update(ctx context.Context, task *models.VectorTileSetTask) error {
	if err := normalizeVectorTileSetTask(task); err != nil {
		return err
	}
	existing, err := s.repo.GetTaskBySemanticHash(ctx, task.TenantID, stringFromConfig(task.Config["semantic_hash"]), task.ID)
	if err != nil {
		return err
	}
	if existing != nil {
		return fmt.Errorf("%w: vector tile set task %d already owns the same business target", commonAPI.ErrConflict, existing.ID)
	}
	return s.repo.UpdateTask(ctx, task)
}

func (s *VectorTileSetTaskService) reuseExistingTask(ctx context.Context, task, existing *models.VectorTileSetTask) error {
	existing.Name = task.Name
	existing.Description = task.Description
	existing.Enabled = task.Enabled
	existing.Schedule = ""
	existing.NextRunAt = nil
	existing.Config = task.Config.Clone()
	if existing.CreatedBy == nil {
		existing.CreatedBy = task.CreatedBy
	}
	if err := s.repo.UpdateTask(ctx, existing); err != nil {
		return err
	}
	*task = *existing
	return nil
}
func (s *VectorTileSetTaskService) Delete(ctx context.Context, id, tenantID uint) error {
	return s.repo.DeleteTask(ctx, id, tenantID)
}
func (s *VectorTileSetTaskService) GetByID(ctx context.Context, id, tenantID uint) (*models.VectorTileSetTask, error) {
	return s.repo.GetTask(ctx, id, tenantID)
}
func (s *VectorTileSetTaskService) List(ctx context.Context, tenantID uint, page, pageSize int) ([]*models.VectorTileSetTask, int64, error) {
	return s.repo.ListTasks(ctx, tenantID, page, pageSize)
}

func (s *VectorTileSetTaskService) RecordProgressEvent(ctx context.Context, tenantID uint, executionID string, event TileCacheProgressEvent) error {
	if s.taskExecRepo == nil {
		return errors.New("task execution repository is required")
	}
	executionID = strings.TrimSpace(executionID)
	event.Phase = strings.TrimSpace(event.Phase)
	event.Event = strings.TrimSpace(event.Event)
	event.Message = strings.TrimSpace(event.Message)
	if executionID == "" || tenantID == 0 {
		return errors.New("execution_id and tenant_id are required")
	}
	if event.Phase == "" || event.Event == "" {
		return errors.New("phase and event are required")
	}
	if event.TilesProcessed < 0 || event.TilesTotalEstimate < 0 ||
		(event.TilesTotalEstimate > 0 && event.TilesProcessed > event.TilesTotalEstimate) {
		return errors.New("invalid tile progress counters")
	}
	exec, err := s.taskExecRepo.GetByExecutionID(ctx, executionID, int(tenantID))
	if err != nil {
		return err
	}
	if exec.Module != commonExecution.ModuleManager || exec.TaskType != commonExecution.TaskTypeVectorTileSetGeneration {
		return ErrVectorTileSetProgressTargetMismatch
	}
	if exec.IsCompleted() {
		return ErrVectorTileSetExecutionCompleted
	}
	if _, err := requireManagerExecutionLease(ctx, tenantID, executionID); err != nil {
		return err
	}
	nextProgress := tileCacheEventProgressPercent(event, exec.Progress)
	currentStep := event.Message
	if currentStep == "" || currentStep == "生成矢量瓦片缓存" {
		currentStep = "生成业务矢量瓦片集"
		if event.MaxZoom > 0 {
			currentStep = fmt.Sprintf("生成业务矢量瓦片集 z%d/%d", event.CurrentZoom, event.MaxZoom)
		}
		if event.TilesTotalEstimate > 0 {
			currentStep = fmt.Sprintf("生成业务矢量瓦片集 z%d/%d：%d/%d", event.CurrentZoom, event.MaxZoom, event.TilesProcessed, event.TilesTotalEstimate)
		}
	}
	now := time.Now()
	fields := map[string]interface{}{
		"progress": nextProgress, "current_step": currentStep,
		"metadata": tileCacheProgressEventMetadata(exec.Metadata, event, nextProgress), "updated_at": now,
	}
	if exec.StartedAt != nil {
		fields["execution_time_ms"] = int64(math.Max(0, float64(now.Sub(*exec.StartedAt).Milliseconds())))
	}
	return s.taskExecRepo.UpdateFields(ctx, executionID, int(tenantID), fields)
}

func (s *VectorTileSetTaskService) Execute(ctx context.Context, taskID, tenantID uint, triggerType, source string, parentExecutionID *string) (string, error) {
	triggerType, err := commonExecution.NormalizeTriggerType(triggerType)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(source) == "" {
		source = commonExecution.ModuleManager
	}
	executionID := uuid.NewString()
	now := time.Now()
	step := "生成业务矢量瓦片集"
	exec := &commonExecution.TaskExecution{ExecutionID: executionID, TenantID: int(tenantID), Module: commonExecution.ModuleManager,
		TaskType: commonExecution.TaskTypeVectorTileSetGeneration, Source: source, ParentExecutionID: parentExecutionID,
		Status: commonExecution.ExecutionStatusPending, Progress: 0, CurrentStep: &step, TriggerType: triggerType, CreatedAt: now, UpdatedAt: now}
	_, err = s.repo.ClaimExecution(ctx, taskID, tenantID, exec)
	if err != nil {
		if errors.Is(err, commonAPI.ErrNotFound) {
			return "", ErrTaskNotFound
		}
		if errors.Is(err, commonAPI.ErrConflict) {
			return "", ErrTaskExecutionBusy
		}
		return "", err
	}
	return executionID, nil
}

func (s *VectorTileSetTaskService) run(ctx context.Context, task *models.VectorTileSetTask, executionID string) {
	startedAt := time.Now()
	if err := s.repo.StartExecution(ctx, task.ID, task.TenantID, executionID, startedAt); err != nil {
		return
	}
	status, progress := commonExecution.ExecutionStatusSuccess, 100
	metadata := commonModels.JSONMap{}
	var errDetails commonModels.JSONMap
	var metaScanExecutionID string
	cfg, err := readVectorTileSetConfig(task.Config)
	if err == nil {
		cfg.SourceVersion, err = s.resolveSourceVersion(ctx, task.TenantID, cfg.Source)
	}
	if err == nil && s.tileCacheRepo != nil {
		var reusable *models.TileCache
		reusable, err = s.tileCacheRepo.GetReusableTileCache(ctx, task.TenantID, cfg.Source.ItemFingerprint, cfg.SourceVersion, cfg.ProfileHash)
		if err == nil && reusable != nil {
			cfg.ReusableCacheStorageRef = reusable.StorageRef
		}
	}
	var result *VectorTileSetExecutionResult
	if err == nil {
		if s.executor == nil {
			err = errors.New("vector tile set executor is not configured")
		} else {
			result, err = s.executor.GenerateVectorTileSet(ctx, VectorTileSetExecutionRequest{Task: task, ExecutionID: executionID, Config: cfg})
		}
	}
	if err == nil && result != nil {
		metadata = result.Metadata.Clone()
		if metadata == nil {
			metadata = commonModels.JSONMap{}
		}
		metadata["catalog_path"] = result.EngineCatalogPath
		metadata["source_version"] = cfg.SourceVersion
		if s.metaScanSubmitter == nil {
			err = errors.New("vector tile set meta scan submitter is not configured")
		} else {
			var scan *commonExecution.TaskExecution
			scan, err = s.metaScanSubmitter.CreateManualScanRunForTenant(task.TenantID, vectorTileSetMetaScanOptions(cfg, result.EngineCatalogPath))
			if err == nil && scan != nil {
				metaScanExecutionID = scan.ExecutionID
				metadata["meta_scan_execution_id"] = scan.ExecutionID
			}
		}
		if err == nil {
			metadata = managerExecutionLineage(metadata, commonExecution.TaskTypeVectorTileSetGeneration,
				[]commonExecution.LineageResourceRef{managerItemLineageRef(cfg.Source.Locator, cfg.Source.ItemFingerprint, cfg.Source.ItemID)},
				[]commonExecution.LineageResourceRef{managerResourceLineageRef(managerLineageOutputPort, managerChildResourceLocator(cfg.TargetLocator, cfg.TargetName))},
				metaScanExecutionID,
			)
		}
	}
	if err != nil {
		status, progress, errDetails = commonExecution.ExecutionStatusFailed, 0, commonModels.JSONMap{"message": err.Error()}
	}
	completedAt := time.Now()
	if completeErr := s.repo.CompleteExecution(ctx, task.ID, task.TenantID, executionID, map[string]interface{}{
		"status": status, "progress": progress, "metadata": metadata, "error_details": errDetails,
		"completed_at": completedAt, "execution_time_ms": completedAt.Sub(startedAt).Milliseconds(), "updated_at": completedAt,
	}, completedAt); completeErr != nil {
		logger.L().Warn("提交业务矢量瓦片集 execution 终态失败", "error", completeErr)
	}
}

func vectorTileSetMetaScanOptions(cfg VectorTileSetExecutionConfig, catalogPath string) commonClient.MetaScanOptions {
	return commonClient.MetaScanOptions{
		EngineID:     cfg.TargetEngineID,
		CatalogPaths: []string{catalogPath},
		ScanDepth:    commonClient.MetaScanDepthDeep,
	}
}

func (s *VectorTileSetTaskService) resolveSourceVersion(ctx context.Context, tenantID uint, source tileCacheTaskTargetIdentity) (string, error) {
	if s.metaClient == nil || source.ItemID == 0 {
		return "", errors.New("Meta client and source item_id are required for vector tile set source version")
	}
	item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(source.ItemID)
	if err != nil {
		return "", err
	}
	if item == nil || item.EngineID != source.EngineID || item.Fingerprint != source.ItemFingerprint {
		return "", errors.New("vector tile set source item does not match task")
	}
	return sourceVersionForItem(source.ItemFingerprint, *item), nil
}

func normalizeVectorTileSetTask(task *models.VectorTileSetTask) error {
	if task == nil {
		return errors.New("vector tile set task is nil")
	}
	task.Name, task.Description, task.Schedule = strings.TrimSpace(task.Name), strings.TrimSpace(task.Description), strings.TrimSpace(task.Schedule)
	if task.Name == "" || task.Config == nil {
		return errors.New("vector tile set task name and config are required")
	}
	if task.Schedule != "" || task.NextRunAt != nil {
		return errors.New("vector tile set generation task does not support schedule")
	}
	cfg, err := normalizeVectorTileSetConfig(task.Config)
	if err != nil {
		return err
	}
	semanticHash, err := vectorTileSetSemanticHash(cfg)
	if err != nil {
		return err
	}
	task.Config["semantic_hash"] = semanticHash
	return nil
}

func readVectorTileSetConfig(config commonModels.JSONMap) (VectorTileSetExecutionConfig, error) {
	return normalizeVectorTileSetConfig(config.Clone())
}

func normalizeVectorTileSetConfig(config commonModels.JSONMap) (VectorTileSetExecutionConfig, error) {
	source, ok := asJSONMap(config["source"])
	if !ok {
		return VectorTileSetExecutionConfig{}, errors.New("vector tile set config.source is required")
	}
	working := commonModels.JSONMap{"target": source.Clone(), "tile": config["tile"], "options": config["options"], "optimization": config["optimization"]}
	identity, err := normalizeTileCacheTaskTarget(working)
	if err != nil {
		return VectorTileSetExecutionConfig{}, err
	}
	profileHash, err := normalizeVectorTileProfile(working, identity)
	if err != nil {
		return VectorTileSetExecutionConfig{}, err
	}
	target, ok := asJSONMap(config["target"])
	if !ok {
		return VectorTileSetExecutionConfig{}, errors.New("vector tile set config.target is required")
	}
	targetEngineID := uintFromConfig(target["engine_id"])
	targetLocator := stringFromConfig(target["storage_locator"])
	targetName := strings.TrimSpace(stringFromConfig(target["name"]))
	if targetEngineID == 0 || targetLocator == "" || targetName == "" {
		return VectorTileSetExecutionConfig{}, errors.New("vector tile set target requires engine_id, storage_locator, and name")
	}
	if !strings.HasSuffix(strings.ToLower(targetName), ".pmtiles") {
		targetName += ".pmtiles"
	}
	loc, err := resourcetree.ParseURI(targetLocator)
	if err != nil || loc.EngineID != targetEngineID {
		return VectorTileSetExecutionConfig{}, errors.New("vector tile set target storage_locator is invalid")
	}
	config["source"] = working["target"]
	config["tile"] = working["tile"]
	config["options"] = working["options"]
	config["profile_hash"] = profileHash
	config["target"] = commonModels.JSONMap{"engine_id": targetEngineID, "storage_locator": targetLocator, "name": targetName}
	tile, _ := asJSONMap(config["tile"])
	options, _ := asJSONMap(config["options"])
	optimization, _ := asJSONMap(config["optimization"])
	return VectorTileSetExecutionConfig{Source: identity, TargetEngineID: targetEngineID, TargetLocator: targetLocator, TargetName: targetName, ProfileHash: profileHash, Tile: tile, Options: options, Optimization: optimization}, nil
}

func vectorTileSetCatalogPath(cfg VectorTileSetExecutionConfig) string {
	loc, _ := resourcetree.ParseURI(cfg.TargetLocator)
	if loc == nil {
		return ""
	}
	return strings.Trim(joinFilePath(loc.FullName(), cfg.TargetName), "/")
}

func validateVectorTileSetConfig(cfg VectorTileSetExecutionConfig) error {
	if vectorTileSetCatalogPath(cfg) == "" {
		return fmt.Errorf("vector tile set catalog path is empty")
	}
	return nil
}

func vectorTileSetSemanticHash(cfg VectorTileSetExecutionConfig) (string, error) {
	identity := struct {
		SourceFingerprint string `json:"source_fingerprint"`
		TargetEngineID    uint   `json:"target_engine_id"`
		TargetCatalogPath string `json:"target_catalog_path"`
		ProfileHash       string `json:"profile_hash"`
	}{
		SourceFingerprint: strings.TrimSpace(cfg.Source.ItemFingerprint),
		TargetEngineID:    cfg.TargetEngineID,
		TargetCatalogPath: vectorTileSetCatalogPath(cfg),
		ProfileHash:       strings.TrimSpace(cfg.ProfileHash),
	}
	if identity.SourceFingerprint == "" || identity.TargetEngineID == 0 || identity.TargetCatalogPath == "" || identity.ProfileHash == "" {
		return "", errors.New("vector tile set semantic identity is incomplete")
	}
	data, err := json.Marshal(identity)
	if err != nil {
		return "", fmt.Errorf("marshal vector tile set semantic identity: %w", err)
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
