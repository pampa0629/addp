package scanners

import (
	"fmt"
	"strings"

	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
)

// NewScanner 创建对应类型的扫描器
// 这是统一的工厂方法，根据数据库类型返回相应的 Scanner 实现
// 重构后：不再接受连接字符串，而是接受Resource对象，委托给插件系统
func NewScanner(resource *commonModels.Resource) (format.Scanner, error) {
	dbType := strings.ToLower(resource.ResourceType)

	switch dbType {
	case "postgresql", "postgres":
		return NewPostgresScanner(resource)
	case "mysql":
		return NewMySQLScanner(resource)
	case "doris":
		return NewDorisScanner(resource)
	case "clickhouse":
		return NewClickHouseScanner(resource)
	case "mongodb":
		return NewMongoDBScanner(resource)
	case "s3", "minio", "oss", "object_storage", "object-storage":
		// S3Scanner 仍然需要连接字符串，暂时保持原样
		// TODO: 未来也可以迁移到插件系统
		connStr, err := commonModels.BuildConnectionString(resource)
		if err != nil {
			return nil, fmt.Errorf("failed to build connection string: %w", err)
		}
		return NewS3Scanner(connStr)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}
