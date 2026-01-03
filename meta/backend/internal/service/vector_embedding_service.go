package service

import (
	"context"
	"fmt"
	"log/slog"
	"mime"
	pathpkg "path"
	"strings"
	"time"

	"github.com/addp/common/embedding"
	"github.com/addp/common/vectorstore"
	"github.com/addp/meta/internal/search"
)

const (
	documentEmbeddingRuneLimit = 3500
	maxImageEmbeddingBytes     = 3 * 1024 * 1024  // 3MB
	maxVideoEmbeddingBytes     = 10 * 1024 * 1024 // 10MB
)

// VectorEmbeddingService 负责文档向量化
// 职责：为文档、图片、视频生成向量嵌入并存储到向量数据库
type VectorEmbeddingService struct {
	vectorStore           *vectorstore.PgVectorStore
	multiModalEmbedder    embedding.MultiModalEmbedder
	embeddingTimeout      time.Duration
	log                   *slog.Logger
	objectStorageService  *ObjectStorageScanService // 用于获取对象内容
}

// NewVectorEmbeddingService 创建向量嵌入服务
func NewVectorEmbeddingService(
	log *slog.Logger,
	objectStorageService *ObjectStorageScanService,
) *VectorEmbeddingService {
	return &VectorEmbeddingService{
		log:                  log,
		embeddingTimeout:     20 * time.Second,
		objectStorageService: objectStorageService,
	}
}

// EnableDocumentVectorization 启用文档向量化
func (s *VectorEmbeddingService) EnableDocumentVectorization(
	store *vectorstore.PgVectorStore,
	embedder embedding.MultiModalEmbedder,
	timeout time.Duration,
) {
	s.vectorStore = store
	s.multiModalEmbedder = embedder
	if timeout > 0 {
		s.embeddingTimeout = timeout
	}
}

// DisableDocumentVectorization 禁用文档向量化
func (s *VectorEmbeddingService) DisableDocumentVectorization() {
	s.vectorStore = nil
	s.multiModalEmbedder = nil
}

// IsEnabled 检查向量化是否启用
func (s *VectorEmbeddingService) IsEnabled() bool {
	return s.vectorStore != nil && s.multiModalEmbedder != nil
}

// UpsertDocumentEmbedding 生成文档向量并存储
func (s *VectorEmbeddingService) UpsertDocumentEmbedding(docRecord *search.DocumentRecord, content string) {
	if !s.IsEnabled() {
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), s.resolveEmbeddingTimeout())
	defer cancel()

	modality := detectDocumentModality(docRecord)

	var (
		embeddingResult *embedding.Embedding
		usage           *embedding.Usage
		err             error
	)

	switch modality {
	case embedding.ModalityImage:
		embeddingResult, usage, err = s.generateImageEmbedding(ctx, docRecord)
	case embedding.ModalityVideo:
		embeddingResult, usage, err = s.generateVideoEmbedding(ctx, docRecord)
	default:
		embeddingResult, usage, err = s.generateDocumentEmbedding(ctx, docRecord, content)
	}

	if err != nil {
		s.log.Warn("文档向量化失败", "document_id", docRecord.DocumentID, "modality", string(modality), "error", err)
		return
	}
	if embeddingResult == nil {
		return
	}

	vectorMeta := s.buildVectorMetadata(docRecord, modality, embeddingResult, usage)
	record := vectorstore.Record{
		ObjectID:  docRecord.DocumentID,
		AssetID:   docRecord.AssetID,
		Modality:  modality,
		Model:     embeddingResult.Model,
		Vector:    embeddingResult.Vector,
		Metadata:  vectorMeta,
		Dimension: embeddingResult.Dimension,
	}

	if docRecord.TenantID != 0 {
		tenantID := docRecord.TenantID
		record.TenantID = &tenantID
	}
	if docRecord.EngineID != 0 {
		resourceID := docRecord.EngineID
		record.EngineID = &resourceID
	}

	dbCtx, cancelDB := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancelDB()

	if _, err := s.vectorStore.Upsert(dbCtx, record); err != nil {
		s.log.Warn("向量存储写入失败", "document_id", docRecord.DocumentID, "error", err)
	}
}

func (s *VectorEmbeddingService) resolveEmbeddingTimeout() time.Duration {
	if s.embeddingTimeout <= 0 {
		return 20 * time.Second
	}
	return s.embeddingTimeout
}

// generateDocumentEmbedding 生成文档嵌入
func (s *VectorEmbeddingService) generateDocumentEmbedding(
	ctx context.Context,
	docRecord *search.DocumentRecord,
	content string,
) (*embedding.Embedding, *embedding.Usage, error) {
	text := strings.TrimSpace(content)
	if text == "" {
		text = strings.TrimSpace(docRecord.ContentPreview)
	}
	if text == "" {
		return nil, nil, nil
	}

	trimmedContent := truncateRunes(text, documentEmbeddingRuneLimit)
	embeddingText := strings.TrimSpace(strings.Join([]string{
		strings.TrimSpace(docRecord.Title),
		strings.TrimSpace(trimmedContent),
	}, "\n\n"))
	if embeddingText == "" {
		return nil, nil, nil
	}

	language := ""
	if docRecord.Metadata != nil {
		language = getStringFromMap(docRecord.Metadata, "language")
	}

	metaForEmbedding := map[string]string{
		"file_name": docRecord.FileName,
		"bucket":    docRecord.Bucket,
	}
	if docRecord.RelativePath != "" {
		metaForEmbedding["relative_path"] = docRecord.RelativePath
	}

	result, err := s.multiModalEmbedder.EmbedDocument(ctx, []embedding.DocumentInput{
		{
			ID:       docRecord.DocumentID,
			Content:  embeddingText,
			Title:    docRecord.Title,
			Language: language,
			Metadata: metaForEmbedding,
		},
	})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

// generateImageEmbedding 生成图片嵌入
func (s *VectorEmbeddingService) generateImageEmbedding(
	ctx context.Context,
	docRecord *search.DocumentRecord,
) (*embedding.Embedding, *embedding.Usage, error) {
	if strings.TrimSpace(docRecord.Bucket) == "" {
		return nil, nil, nil
	}

	objectPath := docRecord.RelativePath
	if objectPath == "" {
		objectPath = docRecord.FilePath
	}

	// 使用 ObjectStorageScanService 获取对象内容
	data, contentType, err := s.objectStorageService.FetchObjectContent(
		ctx,
		docRecord.EngineID,
		docRecord.TenantID,
		docRecord.Bucket,
		objectPath,
		maxImageEmbeddingBytes,
	)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, nil
	}

	mimeType := normalizeMimeType(contentType, docRecord.ContentType, docRecord.FileName, "image/png")

	meta := map[string]string{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
	}

	result, err := s.multiModalEmbedder.EmbedImage(ctx, []embedding.ImageInput{{
		ID:       docRecord.DocumentID,
		Data:     data,
		MIMEType: mimeType,
		Metadata: meta,
	}})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

// generateVideoEmbedding 生成视频嵌入
func (s *VectorEmbeddingService) generateVideoEmbedding(
	ctx context.Context,
	docRecord *search.DocumentRecord,
) (*embedding.Embedding, *embedding.Usage, error) {
	if strings.TrimSpace(docRecord.Bucket) == "" {
		return nil, nil, nil
	}

	objectPath := docRecord.RelativePath
	if objectPath == "" {
		objectPath = docRecord.FilePath
	}

	// 使用 ObjectStorageScanService 获取对象内容
	data, contentType, err := s.objectStorageService.FetchObjectContent(
		ctx,
		docRecord.EngineID,
		docRecord.TenantID,
		docRecord.Bucket,
		objectPath,
		maxVideoEmbeddingBytes,
	)
	if err != nil {
		return nil, nil, err
	}
	if len(data) == 0 {
		return nil, nil, nil
	}

	mimeType := normalizeMimeType(contentType, docRecord.ContentType, docRecord.FileName, "video/mp4")

	meta := map[string]string{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
	}

	result, err := s.multiModalEmbedder.EmbedVideo(ctx, []embedding.VideoInput{{
		ID:       docRecord.DocumentID,
		Data:     data,
		MIMEType: mimeType,
		Metadata: meta,
	}})
	if err != nil {
		return nil, nil, err
	}
	if result == nil || len(result.Embeddings) == 0 {
		return nil, nil, fmt.Errorf("embedding service returned empty result")
	}

	embeddingResult := result.Embeddings[0]
	return &embeddingResult, result.Usage, nil
}

// buildVectorMetadata 构建向量元数据
func (s *VectorEmbeddingService) buildVectorMetadata(
	docRecord *search.DocumentRecord,
	modality embedding.Modality,
	embed *embedding.Embedding,
	usage *embedding.Usage,
) map[string]any {
	meta := map[string]any{
		"file_name":     docRecord.FileName,
		"bucket":        docRecord.Bucket,
		"relative_path": docRecord.RelativePath,
		"engine_id":     docRecord.EngineID,
		"resource_name": docRecord.ResourceName,
		"resource_type": docRecord.ResourceType,
		"document_type": docRecord.DocumentType,
		"content_type":  docRecord.ContentType,
		"modality":      string(modality),
	}
	if docRecord.Title != "" {
		meta["title"] = docRecord.Title
	}
	if docRecord.ContentPreview != "" {
		meta["content_preview"] = docRecord.ContentPreview
	}
	if docRecord.Metadata != nil && len(docRecord.Metadata) > 0 {
		meta["source_metadata"] = docRecord.Metadata
	}
	if embed != nil && len(embed.Metadata) > 0 {
		meta["embedding_metadata"] = embed.Metadata
	}
	if usage != nil {
		meta["vector_usage"] = map[string]any{
			"prompt_tokens": usage.PromptTokens,
			"total_tokens":  usage.TotalTokens,
			"latency_ms":    usage.Latency.Milliseconds(),
		}
	}
	return meta
}

// detectDocumentModality 检测文档模态（静态辅助函数）
func detectDocumentModality(docRecord *search.DocumentRecord) embedding.Modality {
	contentType := strings.ToLower(strings.TrimSpace(docRecord.ContentType))
	if contentType == "" {
		if metaType := strings.ToLower(strings.TrimSpace(getStringFromMap(docRecord.Metadata, "content_type"))); metaType != "" {
			contentType = metaType
		}
	}
	if contentType == "" {
		if metaType := strings.ToLower(strings.TrimSpace(getStringFromMap(docRecord.Metadata, "mime_type"))); metaType != "" {
			contentType = metaType
		}
	}
	if contentType == "" {
		if ext := strings.ToLower(strings.TrimSpace(pathpkg.Ext(docRecord.FileName))); ext != "" {
			if inferred := strings.ToLower(strings.TrimSpace(mime.TypeByExtension(ext))); inferred != "" {
				contentType = inferred
			}
		}
	}
	switch {
	case strings.HasPrefix(contentType, "image/"):
		return embedding.ModalityImage
	case strings.HasPrefix(contentType, "video/"):
		return embedding.ModalityVideo
	case strings.HasPrefix(contentType, "audio/"):
		return embedding.ModalityAudio
	default:
		return embedding.ModalityDocument
	}
}

// normalizeMimeType 规范化 MIME 类型
func normalizeMimeType(detectedType, recordType, fileName, defaultType string) string {
	mimeType := strings.TrimSpace(detectedType)
	if mimeType == "" {
		mimeType = strings.TrimSpace(recordType)
	}
	if mimeType == "" {
		ext := pathpkg.Ext(fileName)
		if ext != "" {
			if inferred := mime.TypeByExtension(ext); inferred != "" {
				mimeType = inferred
			}
		}
	}
	if mimeType == "" {
		mimeType = defaultType
	}
	return mimeType
}

// 辅助函数 getStringFromMap 和 truncateRunes 在其他文件中已定义，这里直接复用
