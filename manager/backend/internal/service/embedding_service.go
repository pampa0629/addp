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

	"github.com/addp/common/embedding"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/config"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/repository"
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
	engineRepo      *repository.EngineRepository
	embeddingClient embedding.MultiModalEmbedder
	cfg             *config.Config
	log             *slog.Logger
}

// NewEmbeddingService 创建向量化服务
func NewEmbeddingService(
	vectorRepo *repository.EmbeddingRepository,
	engineRepo *repository.EngineRepository,
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
		engineRepo:      engineRepo,
		embeddingClient: embeddingClient,
		cfg:             cfg,
		log:             log,
	}, nil
}

// EmbedObjectRequest 单对象向量化请求
type EmbedObjectRequest struct {
	EngineID  uint
	Bucket    string
	ObjectKey string
	TenantID  *uint
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
	// 1. 生成指纹
	fingerprint := commonModels.GenerateObjectFingerprint(req.EngineID, req.Bucket, req.ObjectKey)

	// 2. 获取对象元数据
	objectInfo, err := s.getObjectInfo(ctx, req.EngineID, req.Bucket, req.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("failed to get object info: %w", err)
	}

	// 3. 检查文件大小限制
	maxSizeMB := s.cfg.VectorConfig.MaxFileSizeMB
	if maxSizeMB > 0 && objectInfo.Size > int64(maxSizeMB)*1024*1024 {
		return nil, fmt.Errorf("file size %d exceeds limit %dMB", objectInfo.Size, maxSizeMB)
	}

	// 4. 确定模态类型
	modality := s.detectModality(objectInfo.ContentType, req.ObjectKey)

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
				"object_key", req.ObjectKey,
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
			"object_key", req.ObjectKey,
			"old_data_updated_at", existing.DataUpdatedAt,
			"new_last_modified", objectInfo.LastModified,
		)
	}

	// 6. 获取对象内容并向量化
	vector, model, err := s.embedObjectContent(ctx, req.EngineID, req.Bucket, req.ObjectKey, modality, objectInfo)
	if err != nil {
		return nil, fmt.Errorf("failed to vectorize content: %w", err)
	}

	// 7. 构建 metadata（用于搜索展示）
	metadata, err := s.buildMetadata(ctx, req.EngineID, req.Bucket, req.ObjectKey, objectInfo)
	if err != nil {
		s.log.Warn("Failed to build metadata, continuing without it", "error", err)
		metadata = nil
	}

	// 8. 存储向量（带去重）
	embeddingModel := &models.Embedding{
		EngineID:      req.EngineID,
		Bucket:        req.Bucket,
		ObjectKey:     req.ObjectKey,
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
		"object_key", req.ObjectKey,
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
func (s *EmbeddingService) EmbedDirectory(ctx context.Context, req EmbedDirectoryRequest) (*EmbedDirectoryResult, error) {
	startTime := time.Now()

	// 创建任务ID
	taskID := fmt.Sprintf("embedding-%d-directory-%d", req.EngineID, time.Now().Unix())

	// 创建任务记录
	task := &models.EmbeddingTask{
		TaskID:    taskID,
		TaskType:  "directory",
		EngineID:  req.EngineID,
		Bucket:    req.Bucket,
		Prefix:    req.Prefix,
		Recursive: req.Recursive,
		Status:    "running",
		StartedAt: &startTime,
		TenantID:  req.TenantID,
	}
	if err := s.vectorRepo.CreateTask(ctx, task); err != nil {
		s.log.Warn("Failed to create task record", "error", err)
		// 不因为任务记录创建失败而中断向量化流程
	}

	// 1. 列出目录下的所有对象
	objects, err := s.listObjects(ctx, req.EngineID, req.Bucket, req.Prefix, req.Recursive)
	if err != nil {
		// 更新任务为失败
		s.updateTaskFailed(ctx, taskID, startTime, fmt.Sprintf("failed to list objects: %v", err))
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

			objReq := EmbedObjectRequest{
				EngineID:  req.EngineID,
				Bucket:    req.Bucket,
				ObjectKey: objectKey,
				TenantID:  req.TenantID,
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

	// 更新任务记录
	completedAt := time.Now()
	duration := completedAt.Sub(startTime).Milliseconds()
	s.updateTaskCompleted(ctx, taskID, result.Total, result.Vectorized, result.Skipped, result.Failed, result.Errors, completedAt, duration)

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

// updateTaskCompleted 更新任务为完成状态
func (s *EmbeddingService) updateTaskCompleted(ctx context.Context, taskID string, total, vectorized, skipped, failed int, errors []string, completedAt time.Time, duration int64) {
	if err := s.vectorRepo.UpdateTaskResult(ctx, taskID, "completed", total, vectorized, skipped, failed, errors, completedAt, duration); err != nil {
		s.log.Warn("Failed to update task result", "task_id", taskID, "error", err)
	}
}

// updateTaskFailed 更新任务为失败状态
func (s *EmbeddingService) updateTaskFailed(ctx context.Context, taskID string, startTime time.Time, errorMsg string) {
	completedAt := time.Now()
	duration := completedAt.Sub(startTime).Milliseconds()
	errors := []string{errorMsg}
	if err := s.vectorRepo.UpdateTaskResult(ctx, taskID, "failed", 0, 0, 0, 0, errors, completedAt, duration); err != nil {
		s.log.Warn("Failed to update task as failed", "task_id", taskID, "error", err)
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
		fingerprints[i] = commonModels.GenerateObjectFingerprint(engineID, bucket, objectKey)
	}

	return s.vectorRepo.DeleteEmbeddingsByFingerprints(ctx, fingerprints)
}

// ListEmbeddingTasks 查询任务列表
func (s *EmbeddingService) ListEmbeddingTasks(ctx context.Context, engineID *uint, tenantID *uint, page, pageSize int) ([]*models.EmbeddingTask, int64, error) {
	return s.vectorRepo.ListTasks(ctx, engineID, tenantID, page, pageSize)
}

// GetEmbeddingTask 查询单个任务
func (s *EmbeddingService) GetEmbeddingTask(ctx context.Context, taskID string) (*models.EmbeddingTask, error) {
	return s.vectorRepo.GetTask(ctx, taskID)
}

// ===== 内部辅助方法 =====

// ObjectInfo 对象元数据
type ObjectInfo struct {
	Size         int64
	ContentType  string
	LastModified time.Time
}

// getObjectInfo 获取对象元数据
func (s *EmbeddingService) getObjectInfo(ctx context.Context, engineID uint, bucket, objectKey string) (*ObjectInfo, error) {
	// 获取引擎信息
	engine, err := s.engineRepo.GetByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
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

	return &ObjectInfo{
		Size:         objInfo.Size,
		ContentType:  objInfo.ContentType,
		LastModified: objInfo.LastModified,
	}, nil
}

// listObjects 列出目录下的所有对象
func (s *EmbeddingService) listObjects(ctx context.Context, engineID uint, bucket, prefix string, recursive bool) ([]string, error) {
	engine, err := s.engineRepo.GetByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
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
	objInfo *ObjectInfo,
) ([]float32, string, error) {
	// 获取引擎信息
	engine, err := s.engineRepo.GetByID(engineID)
	if err != nil {
		return nil, "", fmt.Errorf("failed to get engine: %w", err)
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
func (s *EmbeddingService) createMinioClient(engine *models.Engine) (*minio.Client, error) {
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
func (s *EmbeddingService) buildMetadata(ctx context.Context, engineID uint, bucket, objectKey string, objInfo *ObjectInfo) (datatypes.JSON, error) {
	// 获取引擎信息
	engine, err := s.engineRepo.GetByID(engineID)
	if err != nil {
		return nil, fmt.Errorf("failed to get engine: %w", err)
	}

	// 提取文件名（从 object_key 中获取最后一部分）
	fileName := filepath.Base(objectKey)

	// 计算相对路径（object_key 去掉文件名部分）
	relativePath := objectKey
	if dir := filepath.Dir(objectKey); dir != "." && dir != "/" {
		relativePath = dir
	} else {
		relativePath = ""
	}

	// 构建 metadata
	metadata := map[string]interface{}{
		"engine_id":      engineID,
		"engine_name":    engine.Name,
		"engine_type":    engine.EngineType,
		"bucket":         bucket,
		"file_name":      fileName,
		"relative_path":  relativePath,
		"content_type":   objInfo.ContentType,
		"last_modified":  objInfo.LastModified.Format(time.RFC3339),
	}

	// 转换为 JSON
	jsonData, err := json.Marshal(metadata)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return jsonData, nil
}
