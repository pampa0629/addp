package service

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/addp/labs/vector/internal/client"
	"github.com/addp/labs/vector/internal/models"
)

// VectorService 向量化服务
type VectorService struct {
	embedding client.EmbeddingClient
	pgvector  *client.PgVectorClient
	logger    *slog.Logger
}

// NewVectorService 创建服务实例
func NewVectorService(
	embedding client.EmbeddingClient,
	pgvector *client.PgVectorClient,
	logger *slog.Logger,
) *VectorService {
	return &VectorService{
		embedding: embedding,
		pgvector:  pgvector,
		logger:    logger,
	}
}

// VectorizeImage 向量化单张图片
func (s *VectorService) VectorizeImage(ctx context.Context, imagePath string) error {
	s.logger.Info("========== 开始向量化图片 ==========", "path", imagePath)

	// 验证文件存在
	fileInfo, err := os.Stat(imagePath)
	if err != nil {
		return fmt.Errorf("文件不存在: %w", err)
	}

	if fileInfo.IsDir() {
		return fmt.Errorf("路径是目录，不是文件: %s", imagePath)
	}

	// 提取向量
	vector, err := s.embedding.ExtractVectorFromImage(ctx, imagePath)
	if err != nil {
		return fmt.Errorf("向量提取失败: %w", err)
	}

	// 构建向量记录
	record := models.VectorRecord{
		FilePath:  imagePath,
		FileName:  filepath.Base(imagePath),
		FileSize:  fileInfo.Size(),
		Modality:  models.ModalityImage,
		Model:     models.ModelTongyiEmbeddingVisionPlus,
		Embedding: vector,
		Dimension: len(vector),
		Metadata: map[string]interface{}{
			"mod_time": fileInfo.ModTime().Format("2006-01-02 15:04:05"),
		},
	}

	// 存储到数据库
	if err := s.pgvector.InsertVector(ctx, record); err != nil {
		return fmt.Errorf("向量存储失败: %w", err)
	}

	s.logger.Info("========== 向量化完成 ==========",
		"path", imagePath,
		"dimension", len(vector))

	return nil
}

// BatchVectorize 批量向量化目录下的所有图片
func (s *VectorService) BatchVectorize(ctx context.Context, dirPath string) error {
	s.logger.Info("========== 开始批量向量化 ==========", "directory", dirPath)

	// 支持的图片扩展名
	imageExts := map[string]bool{
		".jpg":  true,
		".jpeg": true,
		".png":  true,
		".gif":  true,
		".bmp":  true,
		".webp": true,
	}

	// 遍历目录，收集所有图片文件
	var imagePaths []string
	err := filepath.Walk(dirPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() {
			ext := strings.ToLower(filepath.Ext(path))
			if imageExts[ext] {
				imagePaths = append(imagePaths, path)
			}
		}

		return nil
	})

	if err != nil {
		return fmt.Errorf("遍历目录失败: %w", err)
	}

	if len(imagePaths) == 0 {
		s.logger.Warn("未找到任何图片文件", "directory", dirPath)
		return nil
	}

	s.logger.Info("找到图片文件", "count", len(imagePaths))

	// 逐个向量化
	successCount := 0
	failedCount := 0

	for i, imagePath := range imagePaths {
		s.logger.Info(fmt.Sprintf("========== 处理进度 [%d/%d] ==========", i+1, len(imagePaths)),
			"file", filepath.Base(imagePath))

		if err := s.VectorizeImage(ctx, imagePath); err != nil {
			s.logger.Error("❌ 向量化失败", "path", imagePath, "error", err)
			failedCount++
			continue
		}

		successCount++
	}

	s.logger.Info("========== 批量向量化完成 ==========",
		"total", len(imagePaths),
		"success", successCount,
		"failed", failedCount)

	return nil
}

// SearchSimilarImages 语义检索相似图片
func (s *VectorService) SearchSimilarImages(
	ctx context.Context,
	queryImagePath string,
	topK int,
) ([]models.SearchResult, error) {
	s.logger.Info("========== 开始语义检索 ==========",
		"query", queryImagePath,
		"top_k", topK)

	// 提取查询图片的向量
	queryVector, err := s.embedding.ExtractVectorFromImage(ctx, queryImagePath)
	if err != nil {
		return nil, fmt.Errorf("查询图片向量提取失败: %w", err)
	}

	// 执行检索（只搜索图片类型）
	results, err := s.pgvector.SearchSimilar(ctx, queryVector, topK, models.ModalityImage)
	if err != nil {
		return nil, fmt.Errorf("相似度检索失败: %w", err)
	}

	s.logger.Info("========== 检索完成 ==========", "result_count", len(results))

	// 打印结果
	for i, result := range results {
		s.logger.Info(fmt.Sprintf("结果 #%d", i+1),
			"file", result.FileName,
			"path", result.FilePath,
			"similarity", fmt.Sprintf("%.4f", result.Similarity),
			"distance", fmt.Sprintf("%.4f", result.Distance))
	}

	return results, nil
}

// VectorizeMultiModal 多模态向量化（支持文本+图片+视频）
func (s *VectorService) VectorizeMultiModal(
	ctx context.Context,
	text string,
	imagePaths []string,
	videoURL string,
) error {
	s.logger.Info("========== 开始多模态向量化 ==========",
		"text", text,
		"image_count", len(imagePaths),
		"video_url", videoURL)

	// 构建内容项
	var contents []models.ContentItem

	if text != "" {
		contents = append(contents, models.ContentItem{Text: text})
	}

	for _, imagePath := range imagePaths {
		imageData, err := os.ReadFile(imagePath)
		if err != nil {
			return fmt.Errorf("读取图片失败: %w", err)
		}

		// Base64 编码（这里简化处理，实际应该使用 base64 包）
		// TODO: 实现 base64 编码
		contents = append(contents, models.ContentItem{Image: string(imageData)})
	}

	if videoURL != "" {
		contents = append(contents, models.ContentItem{Video: videoURL})
	}

	// 注意: CLIP 模型不支持多模态混合输入，此功能暂时禁用
	_ = contents // 避免未使用变量警告
	return fmt.Errorf("CLIP 模型不支持多模态混合输入，请使用单独的文本或图片")
}

// GetStatus 获取数据库状态
func (s *VectorService) GetStatus(ctx context.Context) error {
	// 获取总数
	totalCount, err := s.pgvector.GetVectorCount(ctx)
	if err != nil {
		return fmt.Errorf("查询向量总数失败: %w", err)
	}

	// 按模态类型统计
	stats, err := s.pgvector.GetVectorsByModality(ctx)
	if err != nil {
		return fmt.Errorf("查询模态统计失败: %w", err)
	}

	s.logger.Info("========== 数据库状态 ==========")
	s.logger.Info("向量总数", "total", totalCount)

	for modality, count := range stats {
		s.logger.Info("模态统计", "modality", modality, "count", count)
	}

	s.logger.Info("=========================================")

	return nil
}

// SearchByText 通过文本检索相似图片
func (s *VectorService) SearchByText(ctx context.Context, queryText string, topK int) ([]models.SearchResult, error) {
	s.logger.Info("========== 开始文本语义检索 ==========", "query", queryText, "top_k", topK)

	// 提取查询文本的向量
	queryVector, err := s.embedding.ExtractVectorFromText(ctx, queryText)
	if err != nil {
		return nil, fmt.Errorf("提取查询向量失败: %w", err)
	}

	// 执行相似度检索（默认只检索图片）
	results, err := s.pgvector.SearchSimilar(ctx, queryVector, topK, "image")
	if err != nil {
		return nil, fmt.Errorf("检索失败: %w", err)
	}

	s.logger.Info("========== 检索完成 ==========", "result_count", len(results))

	// 输出结果
	for i, result := range results {
		s.logger.Info(fmt.Sprintf("结果 #%d", i+1),
			"file", result.FileName,
			"path", result.FilePath,
			"similarity", fmt.Sprintf("%.4f", result.Similarity),
			"distance", fmt.Sprintf("%.4f", result.Distance))
	}

	if len(results) == 0 {
		s.logger.Info("未找到相似图片")
	}

	return results, nil
}
