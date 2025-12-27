package models

import "time"

// ContentItem DashScope API 内容项（支持多模态）
type ContentItem struct {
	Text        string   `json:"text,omitempty"`         // 文本内容
	Image       string   `json:"image,omitempty"`        // 图片（Base64 或 URL）
	Video       string   `json:"video,omitempty"`        // 视频 URL
	MultiImages []string `json:"multi_images,omitempty"` // 多张图片（URL 列表）
}

// VectorRecord 向量记录
type VectorRecord struct {
	ID        string                 // UUID
	FilePath  string                 // 文件完整路径
	FileName  string                 // 文件名
	FileSize  int64                  // 文件大小（字节）
	Modality  string                 // 模态类型：image/text/video/multi_images
	Model     string                 // 模型名称
	Embedding []float32              // 向量数据
	Dimension int                    // 向量维度
	Metadata  map[string]interface{} // 扩展元数据
	CreatedAt time.Time              // 创建时间
	UpdatedAt time.Time              // 更新时间
}

// SearchResult 检索结果
type SearchResult struct {
	VectorRecord
	Distance   float64 // 余弦距离（0-2，越小越相似）
	Similarity float64 // 余弦相似度（0-1，越大越相似）
}

// Modality 常量
const (
	ModalityImage       = "image"
	ModalityText        = "text"
	ModalityVideo       = "video"
	ModalityMultiImages = "multi_images"
)

// Model 常量
const (
	ModelTongyiEmbeddingVisionPlus = "tongyi-embedding-vision-plus"
)
