package client

import (
	"context"
	"time"

	commonClient "github.com/addp/common/client"
)

// SystemClient 用于调用 System 模块的内部 API
type SystemClient struct {
	serviceClient *commonClient.SystemServiceClient
}

// APIKeyValidationResponse System 返回的 API Key 验证响应
type APIKeyValidationResponse = commonClient.APIKeyValidationResponse

// NewSystemClient 创建 System 客户端
func NewSystemClient(serviceClient *commonClient.SystemServiceClient) *SystemClient {
	return &SystemClient{serviceClient: serviceClient}
}

// ValidateAPIKey 验证 API Key
// keyHash: API Key 的 SHA256 hash（hex 编码）
func (c *SystemClient) ValidateAPIKey(keyHash string) (*APIKeyValidationResponse, error) {
	return c.serviceClient.ValidateAPIKey(context.Background(), keyHash)
}

// BulkGetAPIKeys 批量获取所有有效的 API Keys（用于预加载缓存）
// TODO: Phase 2 实现
func (c *SystemClient) BulkGetAPIKeys() ([]APIKeyValidationResponse, error) {
	return []APIKeyValidationResponse{}, nil
}

func (c *SystemClient) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*commonClient.ModuleRoutingSnapshot, error) {
	return c.serviceClient.WatchActiveModules(ctx, revision, wait)
}
