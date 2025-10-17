package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateItemFingerprint 生成meta_item的唯一指纹
// 用于去重和数据血缘追踪
func GenerateItemFingerprint(resID uint, identifier string) string {
	// identifier可以是:
	// - 对象存储: bucket/object_path (如 "addp/开会.jpg")
	// - 关系数据库: schema.table (如 "public.users")
	// - 文件系统: file_path (如 "/data/file.csv")

	data := fmt.Sprintf("%d:%s", resID, identifier)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GenerateObjectFingerprint 为对象存储生成指纹
func GenerateObjectFingerprint(resID uint, bucket, objectPath string) string {
	identifier := fmt.Sprintf("%s/%s", bucket, objectPath)
	return GenerateItemFingerprint(resID, identifier)
}

// GenerateTableFingerprint 为关系数据库表生成指纹
func GenerateTableFingerprint(resID uint, schema, tableName string) string {
	identifier := fmt.Sprintf("%s.%s", schema, tableName)
	return GenerateItemFingerprint(resID, identifier)
}

// GenerateFileFingerprint 为文件系统文件生成指纹
func GenerateFileFingerprint(resID uint, filePath string) string {
	return GenerateItemFingerprint(resID, filePath)
}
