package scanner

import (
	"fmt"
	"strings"
)

// NewScanner 创建对应类型的扫描器
func NewScanner(dbType, connStr string) (Scanner, error) {
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
