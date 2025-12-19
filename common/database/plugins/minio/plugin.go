package minio

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// MinIOPlugin MinIO 对象存储插件
type MinIOPlugin struct{}

func init() {
	plugin.Register(&MinIOPlugin{})
}

func (p *MinIOPlugin) Type() string {
	return "minio"
}

func (p *MinIOPlugin) DisplayName() string {
	return "MinIO"
}

func (p *MinIOPlugin) ConnectionCategory() string {
	return "object_storage"
}

func (p *MinIOPlugin) DefaultPort() int {
	return 9000
}

func (p *MinIOPlugin) RequiredFields() []string {
	return []string{"endpoint", "access_key", "secret_key"}
}

func (p *MinIOPlugin) SensitiveFields() []string {
	return []string{"access_key", "secret_key"}
}

func (p *MinIOPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"object_storage","engine":"minio"}]}`
}

func (p *MinIOPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *MinIOPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 对象存储返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal MinIO connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *MinIOPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 规范化 endpoint
	endpoint := p.normalizeEndpoint(plugin.GetString(connInfo, "endpoint"))
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")
	bucket := plugin.GetString(connInfo, "bucket")

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	// 初始化 MinIO 客户端
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create minio client: %w", err)
	}

	// 设置超时
	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	// 测试连接 - 列出存储桶
	buckets, err := client.ListBuckets(testCtx)
	if err != nil {
		return fmt.Errorf("failed to list buckets: %w", err)
	}

	// 如果指定了 bucket，检查是否存在
	if bucket != "" {
		found := false
		for _, b := range buckets {
			if b.Name == bucket {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("bucket '%s' not found", bucket)
		}
	}

	return nil
}

// normalizeEndpoint 规范化 endpoint 中的 localhost
func (p *MinIOPlugin) normalizeEndpoint(endpoint string) string {
	if endpoint == "" {
		return ""
	}

	// 提取主机名和端口部分
	hostPart := endpoint
	portPart := ""

	for i := len(endpoint) - 1; i >= 0; i-- {
		if endpoint[i] == ':' {
			hostPart = endpoint[:i]
			portPart = endpoint[i:] // 包含冒号
			break
		}
	}

	// 如果是 localhost 或 127.0.0.1，使用 NormalizeHost 处理
	if hostPart == "localhost" || hostPart == "127.0.0.1" {
		return plugin.NormalizeHost(hostPart) + portPart
	}

	return endpoint
}

func (p *MinIOPlugin) DefaultBucket() string {
	return ""
}

func (p *MinIOPlugin) SupportsSSL() bool {
	return true
}
