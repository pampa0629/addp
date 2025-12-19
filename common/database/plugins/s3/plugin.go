package s3

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/addp/common/database/plugin"
	miniogo "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Plugin Amazon S3 对象存储插件
// 使用 MinIO SDK (S3 兼容)
type S3Plugin struct{}

func init() {
	plugin.Register(&S3Plugin{})
}

func (p *S3Plugin) Type() string {
	return "s3"
}

func (p *S3Plugin) DisplayName() string {
	return "Amazon S3"
}

func (p *S3Plugin) ConnectionCategory() string {
	return "object_storage"
}

func (p *S3Plugin) DefaultPort() int {
	return 443 // S3 默认使用 HTTPS
}

func (p *S3Plugin) RequiredFields() []string {
	return []string{"endpoint", "access_key", "secret_key"}
}

func (p *S3Plugin) SensitiveFields() []string {
	return []string{"access_key", "secret_key"}
}

func (p *S3Plugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"object_storage","engine":"s3"}]}`
}

func (p *S3Plugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

func (p *S3Plugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	// 对象存储返回 JSON 格式的连接信息
	bytes, err := json.Marshal(connInfo)
	if err != nil {
		return "", fmt.Errorf("failed to marshal S3 connection info: %w", err)
	}
	return string(bytes), nil
}

func (p *S3Plugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	endpoint := plugin.GetString(connInfo, "endpoint")
	accessKey := plugin.GetString(connInfo, "access_key")
	secretKey := plugin.GetString(connInfo, "secret_key")
	useSSL := plugin.GetBool(connInfo, "use_ssl")
	bucket := plugin.GetString(connInfo, "bucket")

	// S3 默认使用 SSL
	if !plugin.Contains([]string{"use_ssl"}, "use_ssl") {
		useSSL = true
	}

	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("missing required fields: endpoint, access_key, secret_key")
	}

	// 初始化 S3 客户端（使用 MinIO SDK，S3 兼容）
	client, err := miniogo.New(endpoint, &miniogo.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, ""),
		Secure: useSSL,
	})
	if err != nil {
		return fmt.Errorf("failed to create s3 client: %w", err)
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

func (p *S3Plugin) DefaultBucket() string {
	return ""
}

func (p *S3Plugin) SupportsSSL() bool {
	return true
}
