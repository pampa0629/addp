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

type APIConsumerCredentialValidationResponse = commonClient.APIConsumerCredentialValidationResponse

// NewSystemClient 创建 System 客户端
func NewSystemClient(serviceClient *commonClient.SystemServiceClient) *SystemClient {
	return &SystemClient{serviceClient: serviceClient}
}

func (c *SystemClient) ValidateAPIConsumerCredential(
	keyHash string,
) (*APIConsumerCredentialValidationResponse, error) {
	return c.serviceClient.ValidateAPIConsumerCredential(context.Background(), keyHash)
}

// BulkGetAPIConsumerCredentials 批量获取有效的 API 消费凭据（Phase 2 预留）。
// TODO: Phase 2 实现
func (c *SystemClient) BulkGetAPIConsumerCredentials() ([]APIConsumerCredentialValidationResponse, error) {
	return []APIConsumerCredentialValidationResponse{}, nil
}

func (c *SystemClient) WatchModules(ctx context.Context, revision int64, wait time.Duration) (*commonClient.ModuleRoutingSnapshot, error) {
	return c.serviceClient.WatchActiveModules(ctx, revision, wait)
}
