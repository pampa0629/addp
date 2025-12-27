package client

import "context"

// EmbeddingClient 向量化客户端接口
type EmbeddingClient interface {
	// ExtractVectorFromText 从文本提取向量
	ExtractVectorFromText(ctx context.Context, text string) ([]float32, error)

	// ExtractVectorFromImage 从图片提取向量
	ExtractVectorFromImage(ctx context.Context, imagePath string) ([]float32, error)
}
