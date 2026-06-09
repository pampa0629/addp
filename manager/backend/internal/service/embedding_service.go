package service

import (
	"context"
	"crypto/sha256"
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
	"github.com/addp/common/embedding"
	commonExecution "github.com/addp/common/execution"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type EmbeddingService struct {
	vectorRepo      *repository.EmbeddingRepository
	systemClient    *commonClient.SystemClient
	metaClient      *commonClient.MetaClient
	embeddingClient embedding.MultiModalEmbedder
	taskExecRepo    *commonExecution.TaskExecutionRepository
	cfg             *config.Config
	log             *slog.Logger
}

func NewEmbeddingService(
	vectorRepo *repository.EmbeddingRepository,
	systemClient *commonClient.SystemClient,
	metaClient *commonClient.MetaClient,
	taskExecRepo *commonExecution.TaskExecutionRepository,
	cfg *config.Config,
	log *slog.Logger,
) (*EmbeddingService, error) {
	embeddingCfg := embedding.ServiceConfig{
		BaseURL: cfg.EmbeddingService.BaseURL,
		APIKey:  cfg.EmbeddingService.APIKey,
		Timeout: cfg.EmbeddingService.Timeout,
		Models: map[embedding.Modality]string{
			embedding.ModalityText:     cfg.EmbeddingService.Models["text"],
			embedding.ModalityDocument: cfg.EmbeddingService.Models["text"],
			embedding.ModalityImage:    cfg.EmbeddingService.Models["image"],
			embedding.ModalityVideo:    cfg.EmbeddingService.Models["video"],
		},
	}

	embeddingClient, err := embedding.NewHTTPEmbeddingClient(embeddingCfg, embedding.WithLogger(log))
	if err != nil {
		return nil, fmt.Errorf("failed to create embedding client: %w", err)
	}

	return &EmbeddingService{
		vectorRepo:      vectorRepo,
		systemClient:    systemClient,
		metaClient:      metaClient,
		embeddingClient: embeddingClient,
		taskExecRepo:    taskExecRepo,
		cfg:             cfg,
		log:             log,
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
	stats := &EmbeddingExecutionStats{}
	items, err := s.resolveExecutionItems(ctx, tenantID, req)
	if err != nil {
		return stats, err
	}
	stats.Total = len(items)

	concurrency := s.cfg.VectorConfig.BatchConcurrency
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
	return s.embeddingStateForCurrentItem(*item, state), itemFingerprint, nil
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
	modelName := s.currentEmbeddingModel()
	dimension := s.currentEmbeddingDimension()
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
		existing.SourceVersion == sourceVersion && existing.Model == modelName && existing.Dimension == dimension {
		return "ready_skipped"
	}

	resolved, err := s.resolveItemForEmbedding(ctx, item)
	if err != nil {
		if errors.Is(err, errUnsupportedItem) {
			s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelName, dimension, models.EmbeddingStatusUnsupported, models.EmbeddingReasonFormatUnsupported, err.Error(), lastExecutionID)
			return "unsupported"
		}
		if errors.Is(err, errMissingSource) {
			s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelName, dimension, models.EmbeddingStatusMissingSource, models.EmbeddingReasonSourceMissing, err.Error(), lastExecutionID)
			return "missing_source"
		}
		s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelName, dimension, models.EmbeddingStatusFailed, models.EmbeddingReasonReadFailed, err.Error(), lastExecutionID)
		return "failed"
	}

	vector, model, err := s.embedResolvedContent(ctx, resolved)
	if err != nil {
		s.upsertNonReadyState(ctx, tenantID, item, itemFingerprint, sourceVersion, modelName, dimension, models.EmbeddingStatusFailed, models.EmbeddingReasonEmbeddingFailed, err.Error(), lastExecutionID)
		return "failed"
	}
	if strings.TrimSpace(model) == "" {
		model = modelName
	}
	if len(vector) > 0 {
		dimension = len(vector)
	}

	now := time.Now()
	state := &models.Embedding{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		ItemID:          item.ID,
		EngineID:        item.EngineID,
		Locator:         s.itemLocator(ctx, item),
		SourceVersion:   sourceVersion,
		Embedding:       vector,
		Model:           model,
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

type resolvedEmbeddingInput struct {
	ID          string
	Data        []byte
	Text        string
	ContentType string
	Modality    embedding.Modality
}

var (
	errUnsupportedItem = errors.New("unsupported item")
	errMissingSource   = errors.New("missing source")
)

func (s *EmbeddingService) resolveItemForEmbedding(ctx context.Context, item commonModels.MetaItem) (*resolvedEmbeddingInput, error) {
	switch strings.ToLower(strings.TrimSpace(item.ItemType)) {
	case string(resourcetree.TypeObject):
		bucket, objectKey, err := splitObjectFullName(item.FullName)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errUnsupportedItem, err)
		}
		info, data, err := s.readObjectContent(ctx, item.EngineID, bucket, objectKey)
		if err != nil {
			return nil, fmt.Errorf("%w: %v", errMissingSource, err)
		}
		if s.cfg.VectorConfig.MaxFileSizeMB > 0 && info.Size > int64(s.cfg.VectorConfig.MaxFileSizeMB)*1024*1024 {
			return nil, fmt.Errorf("%w: file size %d exceeds limit %dMB", errUnsupportedItem, info.Size, s.cfg.VectorConfig.MaxFileSizeMB)
		}
		modality := s.detectModality(info.ContentType, item.Name)
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

func (s *EmbeddingService) embedResolvedContent(ctx context.Context, input *resolvedEmbeddingInput) ([]float32, string, error) {
	if input == nil {
		return nil, "", errors.New("embedding input is nil")
	}
	if s.embeddingClient == nil {
		return nil, "", errors.New(models.EmbeddingReasonEmbeddingServiceNil)
	}
	var result *embedding.BatchResult
	var err error
	switch input.Modality {
	case embedding.ModalityImage:
		result, err = s.embeddingClient.EmbedImage(ctx, []embedding.ImageInput{{
			ID:       input.ID,
			Data:     input.Data,
			MIMEType: input.ContentType,
		}})
	case embedding.ModalityVideo:
		result, err = s.embeddingClient.EmbedVideo(ctx, []embedding.VideoInput{{
			ID:       input.ID,
			Data:     input.Data,
			MIMEType: input.ContentType,
		}})
	case embedding.ModalityText, embedding.ModalityDocument:
		result, err = s.embeddingClient.EmbedText(ctx, []embedding.TextInput{{
			ID:   input.ID,
			Text: input.Text,
		}})
	default:
		return nil, "", fmt.Errorf("unsupported modality: %s", input.Modality)
	}
	if err != nil {
		return nil, "", err
	}
	if result == nil || len(result.Embeddings) == 0 || len(result.Embeddings[0].Vector) == 0 {
		return nil, "", errors.New("embedding result is empty")
	}
	return result.Embeddings[0].Vector, result.Embeddings[0].Model, nil
}

type ObjectStorageInfo struct {
	Size         int64
	ContentType  string
	LastModified time.Time
}

func (s *EmbeddingService) readObjectContent(ctx context.Context, engineID uint, bucket, objectKey string) (*ObjectStorageInfo, []byte, error) {
	if s.systemClient == nil {
		return nil, nil, errors.New("system client not available")
	}
	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, nil, err
	}
	minioClient, err := s.createMinioClient(engine)
	if err != nil {
		return nil, nil, err
	}
	objInfo, err := minioClient.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, nil, err
	}
	obj, err := minioClient.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, nil, err
	}
	defer obj.Close()
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, nil, err
	}
	return &ObjectStorageInfo{Size: objInfo.Size, ContentType: objInfo.ContentType, LastModified: objInfo.LastModified}, data, nil
}

func (s *EmbeddingService) upsertNonReadyState(ctx context.Context, tenantID uint, item commonModels.MetaItem, itemFingerprint, sourceVersion, model string, dimension int, status, reason, message, lastExecutionID string) {
	state := &models.Embedding{
		TenantID:        tenantID,
		ItemFingerprint: itemFingerprint,
		ItemID:          item.ID,
		EngineID:        item.EngineID,
		Locator:         s.itemLocator(ctx, item),
		SourceVersion:   sourceVersion,
		Model:           model,
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
		target["locator"] = s.itemLocator(ctx, *item)
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
			"max_file_size_mb": s.cfg.VectorConfig.MaxFileSizeMB,
		},
		"embedding": commonModels.JSONMap{
			"model":     s.currentEmbeddingModel(),
			"dimension": s.currentEmbeddingDimension(),
		},
	}, nil
}

func (s *EmbeddingService) itemLocator(ctx context.Context, item commonModels.MetaItem) string {
	engineType := ""
	if s.systemClient != nil {
		if engine, err := s.systemClient.GetEngine(item.EngineID); err == nil && engine != nil {
			engineType = engine.EngineType
		}
	}
	loc := resourcetree.LocatorFromFullName(item.EngineID, engineType, item.ItemType, item.FullName, &item.ID)
	if loc == nil {
		return ""
	}
	return loc.ToURI()
}

func (s *EmbeddingService) currentEmbeddingModel() string {
	if s == nil || s.cfg == nil {
		return ""
	}
	model := strings.TrimSpace(s.cfg.EmbeddingService.Models["text"])
	if model == "" {
		model = strings.TrimSpace(s.cfg.EmbeddingService.Models["image"])
	}
	return model
}

func (s *EmbeddingService) currentEmbeddingDimension() int {
	if s == nil || s.cfg == nil || s.cfg.VectorConfig.Dimension <= 0 {
		return 1024
	}
	return s.cfg.VectorConfig.Dimension
}

func (s *EmbeddingService) embeddingStateForCurrentItem(item commonModels.MetaItem, state *models.Embedding) *models.Embedding {
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
	case strings.TrimSpace(s.currentEmbeddingModel()) != "" && state.Model != s.currentEmbeddingModel():
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

func (s *EmbeddingService) detectModality(contentType, objectKey string) embedding.Modality {
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return embedding.ModalityImage
	}
	if strings.HasPrefix(contentType, "video/") {
		return embedding.ModalityVideo
	}
	ext := strings.ToLower(filepath.Ext(objectKey))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return embedding.ModalityImage
	case ".mp4", ".avi", ".mov", ".mkv", ".flv", ".wmv":
		return embedding.ModalityVideo
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return embedding.ModalityDocument
	default:
		return embedding.ModalityText
	}
}

func (s *EmbeddingService) createMinioClient(engine *commonModels.Engine) (*minio.Client, error) {
	connInfo := engine.ConnectionInfo
	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL, _ := connInfo["use_ssl"].(bool)
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, errors.New("invalid minio connection info")
	}
	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
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

func splitObjectFullName(fullName string) (string, string, error) {
	trimmed := strings.Trim(strings.TrimSpace(fullName), "/")
	if trimmed == "" {
		return "", "", errors.New("empty object full_name")
	}
	parts := strings.SplitN(trimmed, "/", 2)
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", "", fmt.Errorf("object full_name must be bucket/object_key, got %q", fullName)
	}
	return parts[0], parts[1], nil
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
