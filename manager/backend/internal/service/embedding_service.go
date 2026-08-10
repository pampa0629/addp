package service

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	enginePlugin "github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonInference "github.com/addp/common/inference"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
)

type EmbeddingService struct {
	vectorRepo            *repository.EmbeddingRepository
	systemClient          *commonClient.SystemClient
	metaClient            *commonClient.MetaClient
	inferenceClient       InferenceEmbeddingClient
	taskExecRepo          *commonExecution.TaskExecutionRepository
	configurationProvider *EmbeddingConfigurationProvider
	bindingService        *InferenceScenarioBindingService
	log                   *slog.Logger
}

func NewEmbeddingService(
	vectorRepo *repository.EmbeddingRepository,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	inferenceClient InferenceEmbeddingClient,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	configurationProvider *EmbeddingConfigurationProvider,
	bindingService *InferenceScenarioBindingService,
	log *slog.Logger,
) (*EmbeddingService, error) {
	if configurationProvider == nil || bindingService == nil || inferenceClient == nil {
		return nil, errors.New("embedding configuration, inference binding and inference client are required")
	}

	return &EmbeddingService{
		vectorRepo:            vectorRepo,
		systemClient:          systemClient,
		metaClient:            metaClient,
		inferenceClient:       inferenceClient,
		taskExecRepo:          taskExecRepo,
		configurationProvider: configurationProvider,
		bindingService:        bindingService,
		log:                   log,
	}, nil
}

type EmbeddingExecutionScope string

const (
	EmbeddingExecutionScopeItem EmbeddingExecutionScope = "item"
	EmbeddingExecutionScopeNode EmbeddingExecutionScope = "node"
)

type EmbeddingExecutionRequest struct {
	Scope   EmbeddingExecutionScope  `json:"scope"`
	Target  EmbeddingExecutionTarget `json:"target"`
	Filters map[string]interface{}   `json:"filters,omitempty"`
	Entry   string                   `json:"entry,omitempty"`
	Source  string                   `json:"source,omitempty"`
	Config  commonModels.JSONMap     `json:"-"`
}

type EmbeddingExecutionTarget struct {
	EngineID        uint   `json:"engine_id"`
	ItemID          uint   `json:"item_id,omitempty"`
	ItemFingerprint string `json:"item_fingerprint,omitempty"`
	NodeID          uint   `json:"node_id,omitempty"`
	Locator         string `json:"locator,omitempty"`
	Recursive       bool   `json:"recursive,omitempty"`
}

type EmbeddingExecutionResponse struct {
	ExecutionID string `json:"execution_id"`
	Status      string `json:"status"`
}

type EmbeddingExecutionContext struct {
	ExecutionID string
	TenantID    int
	StartedAt   time.Time
	Config      commonModels.JSONMap
	Runtime     EffectiveEmbeddingConfiguration
	Binding     ResolvedInferenceScenarioBinding
	Profile     commonInference.ResolveProfileResponse
	client      InferenceEmbeddingClient
}

type InferenceEmbeddingClient interface {
	ResolveProfile(context.Context, commonInference.ResolveProfileRequest) (*commonInference.ResolveProfileResponse, error)
	Embed(context.Context, commonInference.EmbeddingRequest) (*commonInference.EmbeddingResponse, error)
}

type EmbeddingExecutionStats struct {
	Total         int `json:"total"`
	ReadySkipped  int `json:"ready_skipped"`
	Generated     int `json:"generated"`
	Rebuilt       int `json:"rebuilt"`
	Unsupported   int `json:"unsupported"`
	Failed        int `json:"failed"`
	MissingSource int `json:"missing_source"`
}

func (s *EmbeddingService) CreateAdhocExecution(ctx context.Context, tenantID, userID uint, req EmbeddingExecutionRequest) (*EmbeddingExecutionResponse, error) {
	if s == nil {
		return nil, errors.New("embedding service is not available")
	}
	if s.taskExecRepo == nil {
		return nil, errors.New("task execution repository is not available")
	}

	req.Entry = strings.TrimSpace(req.Entry)
	if req.Entry == "" {
		req.Entry = "resource_tree"
	}
	req.Source = commonExecution.ModuleManager
	executionConfig, err := s.buildExecutionConfig(ctx, tenantID, req)
	if err != nil {
		return nil, err
	}
	runtime, binding, profile, err := s.runtimeSnapshot(ctx, tenantID)
	if err != nil {
		return nil, err
	}
	executionConfig["embedding"] = commonModels.JSONMap{
		"model_profile_id": binding.ModelProfileID, "profile_version": profile.ProfileVersion,
		"deployment_id": profile.DeploymentID, "dimension": profile.Dimension,
		"binding_version": binding.BindingVersion,
	}

	executionID := uuid.NewString()
	now := time.Now()
	exec := &commonExecution.TaskExecution{
		ExecutionID:     executionID,
		TenantID:        int(tenantID),
		Module:          commonExecution.ModuleManager,
		TaskType:        commonExecution.TaskTypeEmbedding,
		Source:          commonExecution.ModuleManager,
		Status:          commonExecution.ExecutionStatusRunning,
		TriggerType:     commonExecution.TriggerTypeManual,
		TriggeredBy:     intPtr(int(userID)),
		ExecutionConfig: executionConfig,
		StartedAt:       &now,
	}
	if err := s.taskExecRepo.Create(ctx, exec); err != nil {
		return nil, err
	}

	go func() {
		bgCtx := context.Background()
		stats, execErr := s.RunEmbeddingExecution(bgCtx, tenantID, req, &EmbeddingExecutionContext{
			ExecutionID: executionID,
			TenantID:    int(tenantID),
			StartedAt:   now,
			Config:      executionConfig,
			Runtime:     runtime,
			Binding:     binding,
			Profile:     *profile,
			client:      s.inferenceClient,
		})
		status := commonExecution.ExecutionStatusSuccess
		var errDetails commonModels.JSONMap
		if execErr != nil {
			status = commonExecution.ExecutionStatusFailed
			errDetails = commonModels.JSONMap{"message": execErr.Error()}
		}
		s.finishExecution(bgCtx, executionID, int(tenantID), status, now, errDetails, statsToJSONMap(stats))
	}()

	return &EmbeddingExecutionResponse{ExecutionID: executionID, Status: commonExecution.ExecutionStatusRunning}, nil
}

func (s *EmbeddingService) RunEmbeddingExecution(ctx context.Context, tenantID uint, req EmbeddingExecutionRequest, execCtx *EmbeddingExecutionContext) (*EmbeddingExecutionStats, error) {
	if s == nil {
		return nil, errors.New("embedding service is not available")
	}
	if s.metaClient == nil {
		return nil, errors.New("meta client is not available")
	}
	if execCtx == nil || execCtx.client == nil {
		runtime, binding, profile, err := s.runtimeSnapshot(ctx, tenantID)
		if err != nil {
			return nil, err
		}
		if execCtx == nil {
			execCtx = &EmbeddingExecutionContext{}
		}
		execCtx.Runtime = runtime
		execCtx.Binding = binding
		execCtx.Profile = *profile
		execCtx.client = s.inferenceClient
	}
	stats := &EmbeddingExecutionStats{}
	items, err := s.resolveExecutionItems(ctx, tenantID, req)
	if err != nil {
		return stats, err
	}
	stats.Total = len(items)

	concurrency := execCtx.Runtime.BatchConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}
	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, item := range items {
		item := item
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			outcome := s.processItem(ctx, tenantID, item, execCtx)
			mu.Lock()
			defer mu.Unlock()
			switch outcome {
			case "ready_skipped":
				stats.ReadySkipped++
			case "generated":
				stats.Generated++
			case "rebuilt":
				stats.Rebuilt++
			case "unsupported":
				stats.Unsupported++
			case "missing_source":
				stats.MissingSource++
			default:
				stats.Failed++
			}
		}()
	}
	wg.Wait()

	return stats, nil
}

func (s *EmbeddingService) GetItemEmbeddingState(ctx context.Context, tenantID, itemID uint) (*models.Embedding, string, error) {
	if s.metaClient == nil {
		return nil, "", errors.New("meta client is not available")
	}
	item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(itemID)
	if err != nil {
		return nil, "", err
	}
	itemFingerprint := commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	state, err := s.vectorRepo.GetByItemFingerprint(ctx, tenantID, itemFingerprint)
	if err != nil {
		return nil, itemFingerprint, err
	}
	if state == nil || state.Status != models.EmbeddingStatusReady {
		return state, itemFingerprint, nil
	}
	_, binding, profile, err := s.runtimeSnapshot(ctx, tenantID)
	if err != nil {
		return nil, itemFingerprint, err
	}
	return s.embeddingStateForCurrentItem(*item, state, binding.ModelProfileID, profile.ProfileVersion, profile.DeploymentID), itemFingerprint, nil
}

func (s *EmbeddingService) ListEmbeddings(ctx context.Context, filter repository.EmbeddingListFilter) ([]*models.Embedding, int64, error) {
	if filter.NodeID > 0 {
		if s.metaClient == nil {
			return nil, 0, errors.New("meta client is not available")
		}
		items, err := s.collectNodeItems(ctx, s.metaClient.WithTenantID(filter.TenantID), filter.NodeID, true)
		if err != nil {
			return nil, 0, err
		}
		if len(items) == 0 {
			return []*models.Embedding{}, 0, nil
		}
		itemIDs := make([]uint, 0, len(items))
		for _, item := range items {
			itemIDs = append(itemIDs, item.ID)
		}
		filter.ItemIDs = itemIDs
	}
	return s.vectorRepo.ListEmbeddings(ctx, filter)
}

func (s *EmbeddingService) DeleteEmbedding(ctx context.Context, tenantID, id uint) error {
	return s.vectorRepo.DeleteEmbedding(ctx, tenantID, id)
}

func (s *EmbeddingService) resolveExecutionItems(ctx context.Context, tenantID uint, req EmbeddingExecutionRequest) ([]commonModels.MetaItem, error) {
	client := s.metaClient.WithTenantID(tenantID)
	switch req.Scope {
	case EmbeddingExecutionScopeItem:
		if req.Target.ItemID == 0 {
			return nil, errors.New("scope=item requires target.item_id")
		}
		item, err := client.GetItemByID(req.Target.ItemID)
		if err != nil {
			return nil, err
		}
		if req.Target.EngineID != 0 && item.EngineID != req.Target.EngineID {
			return nil, fmt.Errorf("item %d does not belong to engine %d", item.ID, req.Target.EngineID)
		}
		return []commonModels.MetaItem{*item}, nil
	case EmbeddingExecutionScopeNode:
		if req.Target.NodeID == 0 {
			return nil, errors.New("scope=node requires target.node_id")
		}
		return s.collectNodeItems(ctx, client, req.Target.NodeID, req.Target.Recursive)
	default:
		return nil, fmt.Errorf("unsupported embedding execution scope: %s", req.Scope)
	}
}

func (s *EmbeddingService) collectNodeItems(ctx context.Context, client *commonClient.MetaClient, nodeID uint, recursive bool) ([]commonModels.MetaItem, error) {
	items, err := client.GetNodeItems(nodeID)
	if err != nil {
		return nil, err
	}
	if !recursive {
		return items, nil
	}
	children, err := client.GetNodeChildren(nodeID)
	if err != nil {
		return nil, err
	}
	for _, child := range children {
		childItems, err := s.collectNodeItems(ctx, client, child.ID, true)
		if err != nil {
			return nil, err
		}
		items = append(items, childItems...)
	}
	return items, nil
}

func (s *EmbeddingService) processItem(ctx context.Context, tenantID uint, item commonModels.MetaItem, execCtx *EmbeddingExecutionContext) string {
	itemFingerprint := commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
	sourceVersion := sourceVersionForItem(itemFingerprint, item)
	modelProfileID := execCtx.Binding.ModelProfileID
	profileVersion := execCtx.Profile.ProfileVersion
	deploymentID := execCtx.Profile.DeploymentID
	dimension := execCtx.Runtime.Dimension
	if dimension <= 0 {
		dimension = s.currentEmbeddingDimension()
	}
	lastExecutionID := ""
	if execCtx != nil {
		lastExecutionID = execCtx.ExecutionID
	}

	existing, err := s.vectorRepo.GetByItemFingerprint(ctx, tenantID, itemFingerprint)
	if err != nil {
		s.log.Warn("查询现有向量化结果失败", "item_id", item.ID, "error", err)
		return "failed"
	}
	if existing != nil && existing.Status == models.EmbeddingStatusReady &&
		existing.SourceVersion == sourceVersion && existing.ModelProfileID == modelProfileID &&
		existing.ProfileVersion == profileVersion && existing.DeploymentID == deploymentID && existing.Dimension == dimension {
		return "ready_skipped"
	}

	resolved, err := s.resolveItemForEmbedding(ctx, tenantID, item, maxFileSizeMBFromExecutionContext(execCtx, execCtx.Runtime.MaxFileSizeMB))
	if err != nil {
		if errors.Is(err, errUnsupportedItem) {
			s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelProfileID, profileVersion, deploymentID, dimension, models.EmbeddingStatusUnsupported, models.EmbeddingReasonFormatUnsupported, err.Error(), lastExecutionID)
			return "unsupported"
		}
		if errors.Is(err, errMissingSource) {
			s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelProfileID, profileVersion, deploymentID, dimension, models.EmbeddingStatusMissingSource, models.EmbeddingReasonSourceMissing, err.Error(), lastExecutionID)
			return "missing_source"
		}
		s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelProfileID, profileVersion, deploymentID, dimension, models.EmbeddingStatusFailed, models.EmbeddingReasonReadFailed, err.Error(), lastExecutionID)
		return "failed"
	}

	response, err := s.embedResolvedContent(ctx, tenantID, modelProfileID, resolved, execCtx.client)
	if err != nil {
		s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelProfileID, profileVersion, deploymentID, dimension, models.EmbeddingStatusFailed, models.EmbeddingReasonEmbeddingFailed, err.Error(), lastExecutionID)
		return "failed"
	}
	vector := response.Vectors[0]
	profileVersion = response.ProfileVersion
	deploymentID = response.DeploymentID
	if len(vector) > 0 {
		dimension = len(vector)
	}

	now := time.Now()
	state := &models.Embedding{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		ItemID:          item.ID,
		EngineID:        item.EngineID,
		Locator:         s.itemLocator(ctx, tenantID, item),
		SourceVersion:   sourceVersion,
		Embedding:       vector,
		ModelProfileID:  modelProfileID,
		ProfileVersion:  profileVersion,
		DeploymentID:    deploymentID,
		Dimension:       dimension,
		Status:          models.EmbeddingStatusReady,
		StatusReason:    models.EmbeddingReasonReady,
		LastExecutionID: stringPtr(lastExecutionID),
		VectorizedAt:    &now,
	}
	if err := s.vectorRepo.UpsertEmbeddingState(ctx, state); err != nil {
		s.log.Warn("写入向量化结果失败", "item_id", item.ID, "error", err)
		return "failed"
	}
	if existing == nil {
		return "generated"
	}
	return "rebuilt"
}

type embeddingModality string

const (
	embeddingModalityText     embeddingModality = "text"
	embeddingModalityImage    embeddingModality = "image"
	embeddingModalityVideo    embeddingModality = "video"
	embeddingModalityDocument embeddingModality = "document"
)

type resolvedEmbeddingInput struct {
	ID          string
	Data        []byte
	Text        string
	ContentType string
	Modality    embeddingModality
}

var (
	errUnsupportedItem = errors.New("unsupported item")
	errMissingSource   = errors.New("missing source")
)

var vectorizableObjectExtensions = map[string]embeddingModality{
	".txt":      embeddingModalityText,
	".md":       embeddingModalityText,
	".markdown": embeddingModalityText,
	".csv":      embeddingModalityText,
	".json":     embeddingModalityText,
	".jsonl":    embeddingModalityText,
	".jpg":      embeddingModalityImage,
	".jpeg":     embeddingModalityImage,
	".png":      embeddingModalityImage,
	".gif":      embeddingModalityImage,
	".bmp":      embeddingModalityImage,
	".webp":     embeddingModalityImage,
	".pdf":      embeddingModalityDocument,
	".doc":      embeddingModalityDocument,
	".docx":     embeddingModalityDocument,
	".ppt":      embeddingModalityDocument,
	".pptx":     embeddingModalityDocument,
	".xls":      embeddingModalityDocument,
	".xlsx":     embeddingModalityDocument,
}

func (s *EmbeddingService) resolveItemForEmbedding(ctx context.Context, tenantID uint, item commonModels.MetaItem, maxFileSizeMB int) (*resolvedEmbeddingInput, error) {
	switch strings.ToLower(strings.TrimSpace(item.ItemType)) {
	case string(resourcetree.TypeObject), string(resourcetree.TypeFile):
		info, data, err := s.readStorageLeafContent(ctx, tenantID, item)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errMissingSource, err)
		}
		if maxFileSizeMB > 0 && info.Size > int64(maxFileSizeMB)*1024*1024 {
			return nil, fmt.Errorf("%w: file size %d exceeds limit %dMB", errUnsupportedItem, info.Size, maxFileSizeMB)
		}
		modality, ok := s.detectSupportedModality(info.ContentType, info.Name)
		if !ok {
			return nil, fmt.Errorf("%w: format %s is not supported for embedding", errUnsupportedItem, filepath.Ext(info.Name))
		}
		return &resolvedEmbeddingInput{
			ID:          item.FullName,
			Data:        data,
			Text:        string(data),
			ContentType: info.ContentType,
			Modality:    modality,
		}, nil
	default:
		return nil, fmt.Errorf("%w: item_type %s is not supported for embedding", errUnsupportedItem, item.ItemType)
	}
}

func maxFileSizeMBFromExecutionContext(execCtx *EmbeddingExecutionContext, defaultValue int) int {
	if execCtx == nil || execCtx.Config == nil {
		return defaultValue
	}
	filters, ok := execCtx.Config["filters"].(commonModels.JSONMap)
	if !ok {
		if raw, rawOK := execCtx.Config["filters"].(map[string]interface{}); rawOK {
			filters = commonModels.JSONMap(raw)
			ok = true
		}
	}
	if !ok {
		return defaultValue
	}
	if configured := intFromConfig(filters["max_file_size_mb"]); configured > 0 {
		return configured
	}
	return defaultValue
}

func (s *EmbeddingService) embedResolvedContent(ctx context.Context, tenantID uint, modelProfileID string, input *resolvedEmbeddingInput, client InferenceEmbeddingClient) (*commonInference.EmbeddingResponse, error) {
	if input == nil {
		return nil, errors.New("embedding input is nil")
	}
	if client == nil {
		return nil, errors.New(models.EmbeddingReasonEmbeddingServiceNil)
	}
	requestInput := commonInference.EmbeddingInput{}
	switch input.Modality {
	case embeddingModalityImage:
		requestInput = commonInference.EmbeddingInput{Modality: commonInference.ModalityImage, Data: base64.StdEncoding.EncodeToString(input.Data), MIMEType: input.ContentType}
	case embeddingModalityVideo:
		return nil, fmt.Errorf("unsupported modality: %s", input.Modality)
	case embeddingModalityText, embeddingModalityDocument:
		requestInput = commonInference.EmbeddingInput{Modality: commonInference.ModalityText, Text: input.Text}
	default:
		return nil, fmt.Errorf("unsupported modality: %s", input.Modality)
	}
	result, err := client.Embed(ctx, commonInference.EmbeddingRequest{
		SchemaVersion: commonInference.SchemaVersion, TenantID: tenantID,
		ModelProfileID: modelProfileID, Inputs: []commonInference.EmbeddingInput{requestInput},
	})
	if err != nil {
		return nil, err
	}
	if result == nil || len(result.Vectors) != 1 || len(result.Vectors[0]) == 0 {
		return nil, errors.New("embedding result is empty")
	}
	return result, nil
}

type ObjectStorageInfo struct {
	Name         string
	Size         int64
	ContentType  string
	LastModified time.Time
}

func (s *EmbeddingService) readStorageLeafContent(ctx context.Context, tenantID uint, item commonModels.MetaItem) (*ObjectStorageInfo, []byte, error) {
	if s.systemClient == nil {
		return nil, nil, errors.New("system client not available")
	}
	engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, item.EngineID)
	if err != nil {
		return nil, nil, err
	}
	if engine == nil {
		return nil, nil, errors.New("engine not found")
	}
	plugin, err := enginePlugin.Get(engine.EngineType)
	if err != nil {
		return nil, nil, err
	}
	contentReader, ok := plugin.(enginePlugin.ContentReadableProvider)
	if !ok {
		return nil, nil, fmt.Errorf("engine %s does not implement content reading", engine.EngineType)
	}
	factsProvider, ok := plugin.(enginePlugin.CatalogFactsProvider)
	if !ok {
		return nil, nil, fmt.Errorf("engine %s does not implement catalog facts", engine.EngineType)
	}
	loc := resourcetree.LocatorFromFullName(item.EngineID, engine.EngineType, item.ItemType, item.FullName, &item.ID)
	if loc == nil {
		return nil, nil, fmt.Errorf("cannot build locator for item %d", item.ID)
	}
	catalogPath, err := resourcetree.ProviderCatalogPathFromLocator(catalogModelForEmbeddingItem(item), loc)
	if err != nil {
		return nil, nil, err
	}
	connInfo := enginePlugin.ConnectionInfo(engine.ConnectionInfo)
	facts, err := factsProvider.DescribeCatalogFacts(ctx, connInfo, catalogPath, enginePlugin.CatalogFactsOptions{})
	if err != nil {
		return nil, nil, err
	}
	reader, err := contentReader.OpenContent(ctx, connInfo, catalogPath, enginePlugin.ReadOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer reader.Close()
	data, err := io.ReadAll(reader)
	if err != nil {
		return nil, nil, err
	}
	info := storageInfoFromCatalogFacts(item, facts)
	return info, data, nil
}

func catalogModelForEmbeddingItem(item commonModels.MetaItem) enginePlugin.CatalogModelSpec {
	switch strings.ToLower(strings.TrimSpace(item.ItemType)) {
	case string(resourcetree.TypeFile):
		return enginePlugin.FileCatalogModel()
	default:
		return enginePlugin.ObjectCatalogModel()
	}
}

func storageInfoFromCatalogFacts(item commonModels.MetaItem, facts *enginePlugin.CatalogFacts) *ObjectStorageInfo {
	info := &ObjectStorageInfo{
		Name: item.Name,
	}
	if info.Name == "" {
		info.Name = filepath.Base(strings.Trim(strings.TrimSpace(item.FullName), "/"))
	}
	if facts == nil {
		if item.SizeBytes != nil {
			info.Size = *item.SizeBytes
		}
		return info
	}
	if facts.Storage != nil {
		if strings.TrimSpace(facts.Storage.Name) != "" {
			info.Name = facts.Storage.Name
		}
		info.ContentType = facts.Storage.ContentType
		if facts.Storage.SizeBytes != nil {
			info.Size = *facts.Storage.SizeBytes
		}
	}
	if facts.UpdatedAt != nil {
		info.LastModified = *facts.UpdatedAt
	}
	if info.Size == 0 && item.SizeBytes != nil {
		info.Size = *item.SizeBytes
	}
	return info
}

func (s *EmbeddingService) upsertNonReadyState(ctx context.Context, tenantID uint, item commonModels.MetaItem, itemFingerprint, sourceVersion, modelProfileID string, profileVersion int64, deploymentID string, dimension int, status, reason, message, lastExecutionID string) {
	state := &models.Embedding{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		ItemID:          item.ID,
		EngineID:        item.EngineID,
		Locator:         s.itemLocator(ctx, tenantID, item),
		SourceVersion:   sourceVersion,
		ModelProfileID:  modelProfileID,
		ProfileVersion:  profileVersion,
		DeploymentID:    deploymentID,
		Dimension:       dimension,
		Status:          status,
		StatusReason:    reason,
		ErrorMessage:    message,
		LastExecutionID: stringPtr(lastExecutionID),
	}
	if err := s.vectorRepo.UpsertEmbeddingState(ctx, state); err != nil {
		s.log.Warn("写入向量化非 ready 状态失败", "item_id", item.ID, "status", status, "error", err)
	}
}

func (s *EmbeddingService) buildExecutionConfig(ctx context.Context, tenantID uint, req EmbeddingExecutionRequest) (commonModels.JSONMap, error) {
	if req.Config != nil {
		return req.Config, nil
	}
	if req.Scope != EmbeddingExecutionScopeItem && req.Scope != EmbeddingExecutionScopeNode {
		return nil, fmt.Errorf("unsupported embedding execution scope: %s", req.Scope)
	}
	target := commonModels.JSONMap{}
	if req.Target.EngineID > 0 {
		target["engine_id"] = req.Target.EngineID
	}
	if req.Scope == EmbeddingExecutionScopeItem {
		if req.Target.ItemID == 0 {
			return nil, errors.New("scope=item requires target.item_id")
		}
		item, err := s.metaClient.WithTenantID(tenantID).GetItemByID(req.Target.ItemID)
		if err != nil {
			return nil, err
		}
		itemFingerprint := commonModels.GenerateItemFingerprint(item.EngineID, item.FullName)
		target["engine_id"] = item.EngineID
		target["item_id"] = item.ID
		target["item_fingerprint"] = itemFingerprint
		target["locator"] = s.itemLocator(ctx, tenantID, *item)
	} else {
		if req.Target.NodeID == 0 {
			return nil, errors.New("scope=node requires target.node_id")
		}
		target["node_id"] = req.Target.NodeID
		target["recursive"] = req.Target.Recursive
		if strings.TrimSpace(req.Target.Locator) != "" {
			target["locator"] = req.Target.Locator
		}
	}
	return commonModels.JSONMap{
		"entry":  req.Entry,
		"scope":  string(req.Scope),
		"target": target,
		"filters": commonModels.JSONMap{
			"max_file_size_mb": s.configurationProvider.Current().MaxFileSizeMB,
		},
	}, nil
}

func (s *EmbeddingService) itemLocator(ctx context.Context, tenantID uint, item commonModels.MetaItem) string {
	engineType := ""
	if s.systemClient != nil {
		if engine, err := s.systemClient.GetEngineForTenant(ctx, tenantID, item.EngineID); err == nil && engine != nil {
			engineType = engine.EngineType
		}
	}
	loc := resourcetree.LocatorFromFullName(item.EngineID, engineType, item.ItemType, item.FullName, &item.ID)
	if loc == nil {
		return ""
	}
	return loc.ToURI()
}

func (s *EmbeddingService) currentEmbeddingDimension() int {
	if s == nil || s.configurationProvider == nil || s.configurationProvider.Current().Dimension <= 0 {
		return 2560
	}
	return s.configurationProvider.Current().Dimension
}

func (s *EmbeddingService) runtimeSnapshot(ctx context.Context, tenantID uint) (EffectiveEmbeddingConfiguration, ResolvedInferenceScenarioBinding, *commonInference.ResolveProfileResponse, error) {
	if s == nil || s.configurationProvider == nil || s.bindingService == nil || s.inferenceClient == nil {
		return EffectiveEmbeddingConfiguration{}, ResolvedInferenceScenarioBinding{}, nil, errors.New("embedding inference runtime is not available")
	}
	runtime := s.configurationProvider.Current()
	binding, err := s.bindingService.Resolve(ctx, tenantID)
	if err != nil {
		return runtime, binding, nil, err
	}
	profile, err := s.inferenceClient.ResolveProfile(ctx, commonInference.ResolveProfileRequest{
		SchemaVersion: commonInference.SchemaVersion, TenantID: tenantID,
		ModelProfileID: binding.ModelProfileID, Operation: commonInference.OperationEmbedding, Modality: commonInference.ModalityText,
	})
	if err != nil {
		return runtime, binding, nil, err
	}
	if profile.Dimension != runtime.Dimension {
		return runtime, binding, nil, fmt.Errorf("model profile dimension %d does not match manager vector dimension %d", profile.Dimension, runtime.Dimension)
	}
	return runtime, binding, profile, nil
}

func (s *EmbeddingService) embeddingStateForCurrentItem(item commonModels.MetaItem, state *models.Embedding, modelProfileID string, profileVersion int64, deploymentID string) *models.Embedding {
	if state == nil {
		return nil
	}
	if state.Status != models.EmbeddingStatusReady {
		return state
	}

	currentSourceVersion := sourceVersionForItem(commonModels.GenerateItemFingerprint(item.EngineID, item.FullName), item)
	statusReason := ""
	switch {
	case state.SourceVersion != currentSourceVersion:
		statusReason = models.EmbeddingReasonSourceChanged
	case state.ModelProfileID != modelProfileID || state.ProfileVersion != profileVersion || state.DeploymentID != deploymentID:
		statusReason = models.EmbeddingReasonModelChanged
	case s.currentEmbeddingDimension() > 0 && state.Dimension != s.currentEmbeddingDimension():
		statusReason = models.EmbeddingReasonDimensionChanged
	}
	if statusReason == "" {
		return state
	}

	outdated := *state
	outdated.Status = models.EmbeddingStatusOutdated
	outdated.StatusReason = statusReason
	outdated.Embedding = nil
	return &outdated
}

func (s *EmbeddingService) finishExecution(ctx context.Context, executionID string, tenantID int, status string, startTime time.Time, errDetails, metadata commonModels.JSONMap) {
	if s.taskExecRepo == nil {
		return
	}
	completedAt := time.Now()
	fields := map[string]interface{}{
		"status":            status,
		"completed_at":      completedAt,
		"execution_time_ms": completedAt.Sub(startTime).Milliseconds(),
	}
	if errDetails != nil {
		fields["error_details"] = errDetails
	}
	if metadata != nil {
		fields["metadata"] = metadata
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, tenantID, fields); err != nil {
		s.log.Warn("更新向量化 execution 失败", "execution_id", executionID, "error", err)
	}
}

func (s *EmbeddingService) detectSupportedModality(contentType, objectKey string) (embeddingModality, bool) {
	ext := strings.ToLower(filepath.Ext(objectKey))
	if modality, ok := vectorizableObjectExtensions[ext]; ok {
		return modality, true
	}
	if ext != "" {
		return "", false
	}

	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return embeddingModalityImage, true
	}
	if strings.HasPrefix(contentType, "video/") {
		return "", false
	}
	if strings.HasPrefix(contentType, "text/") {
		return embeddingModalityText, true
	}
	if strings.Contains(contentType, "json") {
		return embeddingModalityText, true
	}
	return "", false
}

func sourceVersionForItem(itemFingerprint string, item commonModels.MetaItem) string {
	parts := []string{itemFingerprint}
	if item.DataUpdatedAt != nil {
		parts = append(parts, item.DataUpdatedAt.UTC().Format(time.RFC3339Nano))
	}
	if item.SizeBytes != nil {
		parts = append(parts, fmt.Sprintf("size:%d", *item.SizeBytes))
	}
	if item.ObjectSizeBytes != nil {
		parts = append(parts, fmt.Sprintf("object_size:%d", *item.ObjectSizeBytes))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "|")))
	return hex.EncodeToString(sum[:])
}

func statsToJSONMap(stats *EmbeddingExecutionStats) commonModels.JSONMap {
	if stats == nil {
		return commonModels.JSONMap{}
	}
	return commonModels.JSONMap{
		"total":          stats.Total,
		"ready_skipped":  stats.ReadySkipped,
		"generated":      stats.Generated,
		"rebuilt":        stats.Rebuilt,
		"unsupported":    stats.Unsupported,
		"failed":         stats.Failed,
		"missing_source": stats.MissingSource,
	}
}

func intPtr(v int) *int { return &v }

func stringPtr(v string) *string {
	if strings.TrimSpace(v) == "" {
		return nil
	}
	return &v
}
