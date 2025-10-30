package scanners

import (
	"fmt"
	"strings"

	"github.com/addp/common/format"
)

// NewScanner 创建对应类型的扫描器
// 这是统一的工厂方法，根据数据库类型返回相应的 Scanner 实现
func NewScanner(dbType, connStr string) (format.Scanner, error) {
	dbType = strings.ToLower(dbType)

	switch dbType {
	case "postgresql", "postgres":
		return NewPostgresScanner(connStr)
	case "mysql":
		return NewMySQLScanner(connStr)
	case "s3", "minio", "oss", "object_storage", "object-storage":
		return NewS3Scanner(connStr)
	default:
		return nil, fmt.Errorf("unsupported database type: %s", dbType)
	}
}
