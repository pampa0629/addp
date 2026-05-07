package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"sync"
	"time"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/embedding"
	commonModels "github.com/addp/common/models"
	commonRepo "github.com/addp/common/repository"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
	"github.com/google/uuid"
	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"gorm.io/datatypes"
)

// EmbeddingService Manager 向量化服务
// 职责：
// 1. 按需向量化（单对象、目录、Bucket）
// 2. 指纹去重（避免重复向量化）
// 3. 数据更新检测（文件修改后重新向量化）
// 4. 向量化状态查询
type EmbeddingService struct {
	vectorRepo      *repository.EmbeddingRepository
	systemClient    *commonClient.SystemClient
	embeddingClient embedding.MultiModalEmbedder
	taskExecRepo    *commonRepo.TaskExecutionRepository
	cfg             *config.Config
	log             *slog.Logger
}

// NewEmbeddingService 创建向量化服务
func NewEmbeddingService(
	vectorRepo *repository.EmbeddingRepository,
	systemClient *commonClient.SystemClient,
	taskExecRepo *commonRepo.TaskExecutionRepository,
	cfg *config.Config,
	log *slog.Logger,
) (*EmbeddingService, error) {
	// 创建 Embedding 客户端
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
		embeddingClient: embeddingClient,
		taskExecRepo:    taskExecRepo,
		cfg:             cfg,
		log:             log,
	}, nil
}

// EmbedObjectRequest 单对象向量化请求
type EmbedObjectRequest struct {
	EngineID uint
	Bucket   string
	Path     string // 目录路径（以/结尾）
	Name     string // 文件名
	TenantID *uint
}

// EmbedObjectResult 单对象向量化结果
type EmbedObjectResult struct {
	Fingerprint string
	Modality    string
	Skipped     bool   // 是否因为未修改而跳过
	SkipReason  string // 跳过原因
	Vector      []float32
	Model       string
}

// EmbedObject 向量化单个对象（带去重检查）
func (s *EmbeddingService) EmbedObject(ctx context.Context, req EmbedObjectRequest) (*EmbedObjectResult, error) {
	// 拼接完整路径用于操作
	fullPath := req.Path + req.Name

	// 1. 生成指纹 - 两步计算方式
	fullName := commonModels.JoinObjectPath(req.Bucket, req.Path, req.Name)
	fingerprint := commonModels.GenerateItemFingerprint(req.EngineID, fullName)

	// 2. 获取对象元数据
	objectInfo, err := s.getObjectInfo(ctx, req.EngineID, req.Bucket, fullPath)
	if err != nil {
		return nil, fmt.Errorf("failed to get object info: %w", err)
	}

	// 3. 检查文件大小限制
	maxSizeMB := s.cfg.VectorConfig.MaxFileSizeMB
	if maxSizeMB > 0 && objectInfo.Size > int64(maxSizeMB)*1024*1024 {
		return nil, fmt.Errorf("file size %d exceeds limit %dMB", objectInfo.Size, maxSizeMB)
	}

	// 4. 确定模态类型
	modality := s.detectModality(objectInfo.ContentType, req.Name)

	// 5. 检查是否已向量化且未过期
	existing, err := s.vectorRepo.GetByFingerprint(ctx, fingerprint, string(modality))
	if err != nil {
		return nil, fmt.Errorf("failed to check existing vector: %w", err)
	}

	if existing != nil && existing.DataUpdatedAt != nil {
		// 对象未修改，跳过向量化
		if !objectInfo.LastModified.After(*existing.DataUpdatedAt) {
			s.log.Info("Object not modified, skipping vectorization",
				"fingerprint", fingerprint,
				"full_path", fullPath,
				"data_updated_at", existing.DataUpdatedAt,
				"last_modified", objectInfo.LastModified,
			)
			return &EmbedObjectResult{
				Fingerprint: fingerprint,
				Modality:    string(modality),
				Skipped:     true,
				SkipReason:  "object not modified",
			}, nil
		}
		s.log.Info("Object modified, re-vectorizing",
			"fingerprint", fingerprint,
			"full_path", fullPath,
			"old_data_updated_at", existing.DataUpdatedAt,
			"new_last_modified", objectInfo.LastModified,
		)
	}

	// 6. 获取对象内容并向量化
	vector, model, err := s.embedObjectContent(ctx, req.EngineID, req.Bucket, fullPath, modality, objectInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to vectorize content: %w", err)
	}

	// 7. 构建 metadata（用于搜索展示）
	metadata, err := s.buildMetadata(ctx, req.EngineID, req.Bucket, fullPath, objectInfo)
	if err != nil {
		s.log.Warn("Failed to build metadata, continuing without it", "error", err)
		metadata = nil
	}

	// 8. 存储向量（带去重）
	embeddingModel := &models.Embedding{
		EngineID:      req.EngineID,
		Bucket:        req.Bucket,
		Path:          req.Path,
		Name:          req.Name,
		Fingerprint:   fingerprint,
		DataUpdatedAt: &objectInfo.LastModified,
		Embedding:     vector,
		Modality:      string(modality),
		Model:         model,
		FileSize:      objectInfo.Size,
		ContentType:   objectInfo.ContentType,
		Metadata:      metadata,
		TenantID:      req.TenantID,
	}

	if err := s.vectorRepo.UpsertEmbedding(ctx, embeddingModel); err != nil {
		return nil, fmt.Errorf("failed to store embedding: %w", err)
	}

	s.log.Info("Object vectorized successfully",
		"fingerprint", fingerprint,
		"full_path", fullPath,
		"modality", modality,
		"model", model,
		"vector_dimension", len(vector),
	)

	return &EmbedObjectResult{
		Fingerprint: fingerprint,
		Modality:    string(modality),
		Skipped:     false,
		Vector:      vector,
		Model:       model,
	}, nil
}

// EmbedDirectoryRequest 目录批量向量化请求
type EmbedDirectoryRequest struct {
	EngineID  uint
	Bucket    string
	Prefix    string
	Recursive bool
	TenantID  *uint
}

// EmbedDirectoryResult 目录批量向量化结果
type EmbedDirectoryResult struct {
	Total      int      `json:"total"`
	Vectorized int      `json:"vectorized"`
	Skipped    int      `json:"skipped"`
	Failed     int      `json:"failed"`
	Errors     []string `json:"errors,omitempty"`
}

// EmbedDirectory 批量向量化目录
// tenantID 用于写入 common.task_executions；executionID 为空时自动生成（ad-hoc 调用）
func (s *EmbeddingService) EmbedDirectory(ctx context.Context, req EmbedDirectoryRequest) (*EmbedDirectoryResult, error) {
	startTime := time.Now()

	// 写入 common.task_executions (status=running)
	executionID := uuid.New().String()
	var tenantIDInt int
	if req.TenantID != nil {
		tenantIDInt = int(*req.TenantID)
	}
	exec := &commonModels.TaskExecution{
		ExecutionID: executionID,
		TenantID:    tenantIDInt,
		Module:      commonModels.ModuleManager,
		TaskType:    "embedding",
		Status:      commonModels.ExecutionStatusRunning,
		TriggerType: commonModels.TriggerTypeManual,
		StartedAt:   &startTime,
	}
	if s.taskExecRepo != nil {
		if err := s.taskExecRepo.Create(ctx, exec); err != nil {
			s.log.Warn("Failed to create task execution record", "error", err)
		}
	}

	// 1. 列出目录下的所有对象
	objects, err := s.listObjects(ctx, req.EngineID, req.Bucket, req.Prefix, req.Recursive)
	if err != nil {
		s.finishExecution(ctx, executionID, tenantIDInt, commonModels.ExecutionStatusFailed, startTime,
			commonModels.JSONMap{"message": err.Error()}, nil)
		return nil, fmt.Errorf("failed to list objects: %w", err)
	}

	result := &EmbedDirectoryResult{
		Total:  len(objects),
		Errors: make([]string, 0),
	}

	// 2. 并发向量化
	concurrency := s.cfg.VectorConfig.BatchConcurrency
	if concurrency <= 0 {
		concurrency = 5
	}

	sem := make(chan struct{}, concurrency)
	var wg sync.WaitGroup
	var mu sync.Mutex

	for _, obj := range objects {
		wg.Add(1)
		go func(objectKey string) {
			defer wg.Done()

			sem <- struct{}{}
			defer func() { <-sem }()

			// 拆分完整路径为目录和文件名
			dir, name := commonModels.SplitObjectPath(objectKey)

			objReq := EmbedObjectRequest{
				EngineID: req.EngineID,
				Bucket:   req.Bucket,
				Path:     dir,
				Name:     name,
				TenantID: req.TenantID,
			}

			objResult, err := s.EmbedObject(ctx, objReq)
			mu.Lock()
			defer mu.Unlock()

			if err != nil {
				result.Failed++
				result.Errors = append(result.Errors, fmt.Sprintf("%s: %v", objectKey, err))
				s.log.Error("Failed to vectorize object", "object_key", objectKey, "error", err)
			} else if objResult.Skipped {
				result.Skipped++
			} else {
				result.Vectorized++
			}
		}(obj)
	}

	wg.Wait()

	// 完成执行记录
	status := commonModels.ExecutionStatusSuccess
	if result.Failed > 0 && result.Vectorized == 0 {
		status = commonModels.ExecutionStatusFailed
	}
	metadata := commonModels.JSONMap{
		"total":      result.Total,
		"vectorized": result.Vectorized,
		"skipped":    result.Skipped,
		"failed":     result.Failed,
	}
	if len(result.Errors) > 0 {
		metadata["error_samples"] = result.Errors
	}
	s.finishExecution(ctx, executionID, tenantIDInt, status, startTime, nil, metadata)

	s.log.Info("Directory vectorization completed",
		"bucket", req.Bucket,
		"prefix", req.Prefix,
		"total", result.Total,
		"vectorized", result.Vectorized,
		"skipped", result.Skipped,
		"failed", result.Failed,
	)

	return result, nil
}

// finishExecution 更新 common.task_executions 执行完成状态
func (s *EmbeddingService) finishExecution(ctx context.Context, executionID string, tenantID int, status string, startTime time.Time, errDetails, metadata commonModels.JSONMap) {
	if s.taskExecRepo == nil {
		return
	}
	completedAt := time.Now()
	durationMs := completedAt.Sub(startTime).Milliseconds()
	fields := map[string]interface{}{
		"status":            status,
		"completed_at":      completedAt,
		"execution_time_ms": durationMs,
	}
	if errDetails != nil {
		fields["error_details"] = errDetails
	}
	if metadata != nil {
		fields["metadata"] = metadata
	}
	if err := s.taskExecRepo.UpdateFields(ctx, executionID, tenantID, fields); err != nil {
		s.log.Warn("Failed to update task execution", "execution_id", executionID, "error", err)
	}
}

// EmbedBucketRequest Bucket 全量向量化请求
type EmbedBucketRequest struct {
	EngineID uint
	Bucket   string
	TenantID *uint
}

// EmbedBucket 向量化整个 Bucket
func (s *EmbeddingService) EmbedBucket(ctx context.Context, req EmbedBucketRequest) (*EmbedDirectoryResult, error) {
	return s.EmbedDirectory(ctx, EmbedDirectoryRequest{
		EngineID:  req.EngineID,
		Bucket:    req.Bucket,
		Prefix:    "", // 空前缀表示全部对象
		Recursive: true,
		TenantID:  req.TenantID,
	})
}

// GetEmbeddingStatus 查询向量化状态
func (s *EmbeddingService) GetEmbeddingStatus(ctx context.Context, engineID uint, bucket string) (*models.EmbeddingStatus, error) {
	return s.vectorRepo.GetEmbeddingStatus(ctx, engineID, bucket)
}

// DeleteEmbeddings 删除向量
func (s *EmbeddingService) DeleteEmbeddings(ctx context.Context, engineID uint, bucket string, objectKeys []string) error {
	if len(objectKeys) == 0 {
		// 删除整个 bucket 的向量
		return s.vectorRepo.DeleteEmbeddingsByEngine(ctx, engineID, bucket)
	}

	// 删除指定对象的向量（通过指纹）
	fingerprints := make([]string, len(objectKeys))
	for i, objectKey := range objectKeys {
		dir, name := commonModels.SplitObjectPath(objectKey)
		// 两步计算方式：先拼接 full_name，再计算指纹
		fullName := commonModels.JoinObjectPath(bucket, dir, name)
		fingerprints[i] = commonModels.GenerateItemFingerprint(engineID, fullName)
	}

	return s.vectorRepo.DeleteEmbeddingsByFingerprints(ctx, fingerprints)
}

// ===== 内部辅助方法 =====

// ObjectStorageInfo 对象存储元数据。
type ObjectStorageInfo struct {
	Size         int64
	ContentType  string
	LastModified time.Time
}

// getObjectInfo 获取对象元数据
func (s *EmbeddingService) getObjectInfo(ctx context.Context, engineID uint, bucket, objectKey string) (*ObjectStorageInfo, error) {
	// 通过 SystemClient 获取引擎信息
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 创建 MinIO 客户端
	minioClient, err := s.createMinioClient(engine)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	// 获取对象元数据
	objInfo, err := minioClient.StatObject(ctx, bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to stat object: %w", err)
	}

	return &ObjectStorageInfo{
		Size:         objInfo.Size,
		ContentType:  objInfo.ContentType,
		LastModified: objInfo.LastModified,
	}, nil
}

// listObjects 列出目录下的所有对象
func (s *EmbeddingService) listObjects(ctx context.Context, engineID uint, bucket, prefix string, recursive bool) ([]string, error) {
	// 通过 SystemClient 获取引擎信息
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	minioClient, err := s.createMinioClient(engine)
	if err != nil {
		return nil, fmt.Errorf("failed to create minio client: %w", err)
	}

	var objects []string
	opts := minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: recursive,
	}

	for obj := range minioClient.ListObjects(ctx, bucket, opts) {
		if obj.Err != nil {
			return nil, fmt.Errorf("failed to list objects: %w", obj.Err)
		}
		// 跳过目录（以 / 结尾）
		if strings.HasSuffix(obj.Key, "/") {
			continue
		}
		objects = append(objects, obj.Key)
	}

	return objects, nil
}

// embedObjectContent 获取对象内容并向量化
func (s *EmbeddingService) embedObjectContent(
	ctx context.Context,
	engineID uint,
	bucket, objectKey string,
	modality embedding.Modality,
	objInfo *ObjectStorageInfo,
) ([]float32, string, error) {
	// 通过 SystemClient 获取引擎信息
	if s.systemClient == nil {
		return nil, "", fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 创建 MinIO 客户端
	minioClient, err := s.createMinioClient(engine)
	if err != nil {
		return nil, "", fmt.Errorf("failed to create minio client: %w", err)
	}

	// 获取对象内容
	obj, err := minioClient.GetObject(ctx, bucket, objectKey, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("failed to get object: %w", err)
	}
	defer obj.Close()

	// 读取内容
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", fmt.Errorf("failed to read object: %w", err)
	}

	// 根据模态类型调用相应的 Embedding API
	var result *embedding.BatchResult
	var model string

	switch modality {
	case embedding.ModalityText, embedding.ModalityDocument:
		textInputs := []embedding.TextInput{
			{
				ID:   objectKey,
				Text: string(data),
			},
		}
		result, err = s.embeddingClient.EmbedText(ctx, textInputs)
		model = s.cfg.EmbeddingService.Models["text"]

	case embedding.ModalityImage:
		imageInputs := []embedding.ImageInput{
			{
				ID:       objectKey,
				Data:     data,
				MIMEType: objInfo.ContentType,
			},
		}
		result, err = s.embeddingClient.EmbedImage(ctx, imageInputs)
		model = s.cfg.EmbeddingService.Models["image"]

	case embedding.ModalityVideo:
		videoInputs := []embedding.VideoInput{
			{
				ID:       objectKey,
				Data:     data,
				MIMEType: objInfo.ContentType,
			},
		}
		result, err = s.embeddingClient.EmbedVideo(ctx, videoInputs)
		model = s.cfg.EmbeddingService.Models["video"]

	default:
		return nil, "", fmt.Errorf("unsupported modality: %s", modality)
	}

	if err != nil {
		return nil, "", fmt.Errorf("failed to embed content: %w", err)
	}

	if len(result.Embeddings) == 0 {
		return nil, "", fmt.Errorf("embedding result is empty")
	}

	return result.Embeddings[0].Vector, model, nil
}

// detectModality 根据内容类型和文件扩展名检测模态类型
func (s *EmbeddingService) detectModality(contentType, objectKey string) embedding.Modality {
	// 优先使用 content type
	contentType = strings.ToLower(strings.TrimSpace(contentType))
	if strings.HasPrefix(contentType, "image/") {
		return embedding.ModalityImage
	}
	if strings.HasPrefix(contentType, "video/") {
		return embedding.ModalityVideo
	}
	if strings.HasPrefix(contentType, "audio/") {
		return embedding.ModalityAudio
	}

	// 使用文件扩展名
	ext := strings.ToLower(filepath.Ext(objectKey))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".gif", ".bmp", ".webp":
		return embedding.ModalityImage
	case ".mp4", ".avi", ".mov", ".mkv", ".flv", ".wmv":
		return embedding.ModalityVideo
	case ".mp3", ".wav", ".flac", ".aac", ".ogg":
		return embedding.ModalityAudio
	case ".pdf", ".doc", ".docx", ".ppt", ".pptx", ".xls", ".xlsx":
		return embedding.ModalityDocument
	default:
		return embedding.ModalityText
	}
}

// createMinioClient 创建 MinIO 客户端
func (s *EmbeddingService) createMinioClient(engine *commonModels.Engine) (*minio.Client, error) {
	connInfo := engine.ConnectionInfo

	endpoint, _ := connInfo["endpoint"].(string)
	accessKey, _ := connInfo["access_key"].(string)
	secretKey, _ := connInfo["secret_key"].(string)
	useSSL, _ := connInfo["use_ssl"].(bool)

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return nil, fmt.Errorf("invalid minio connection info")
	}

	return minio.New(endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
}

// buildMetadata 构建向量的 metadata（用于搜索展示和定位）
func (s *EmbeddingService) buildMetadata(ctx context.Context, engineID uint, bucket, objectKey string, objInfo *ObjectStorageInfo) (datatypes.JSON, error) {
	// 通过 SystemClient 获取引擎信息
	if s.systemClient == nil {
		return nil, fmt.Errorf("system client not available")
	}

	engine, err := s.systemClient.GetEngine(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine from system: %w", err)
	}

	// 按照路径统一规范拆分：path（目录，以/结尾）、name（文件名）
	path, name := commonModels.SplitObjectPath(objectKey)

	// 构建 metadata（符合路径统一规范）
	metadata := map[string]interface{}{
		"engine_id":   engineID,
		"engine_name": engine.Name,
		"engine_type": engine.EngineType,
		"storage": map[string]interface{}{
			"bucket":           bucket,
			"path":             path,
			"name":             name,
			"content_type":     objInfo.ContentType,
			"last_modified_at": objInfo.LastModified.Format(time.RFC3339),
		},
	}

	// 转换为 JSON
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return jsonData, nil
}
