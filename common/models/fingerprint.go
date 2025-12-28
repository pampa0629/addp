package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
)

// GenerateItemFingerprint 生成meta_item的唯一指纹
// 用于去重和数据血缘追踪
//
// 指纹计算规则:
//   - 基于 resID + identifier 的 SHA256 哈希
//   - identifier 是数据项的唯一标识，不同存储类型格式不同
//   - 同一数据项的指纹始终不变，无论数据内容如何变化
//
// 适用场景:
//   - 对象存储: identifier = "bucket/object_path" (如 "addp/开会.jpg")
//   - 关系数据库: identifier = "schema.table" (如 "public.users")
//   - 文件系统: identifier = "file_path" (如 "/data/file.csv")
func GenerateItemFingerprint(resID uint, identifier string) string {
	data := fmt.Sprintf("%d:%s", resID, identifier)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GenerateObjectFingerprint 为对象存储生成指纹
//
// 参数:
//   - resID: 资源ID（来自 system.engines 表）
//   - bucket: 存储桶名称
//   - objectPath: 对象路径（相对于bucket根目录）
//
// 示例:
//   fingerprint := GenerateObjectFingerprint(1, "addp", "documents/report.pdf")
//   // SHA256("1:addp/documents/report.pdf")
func GenerateObjectFingerprint(resID uint, bucket, objectPath string) string {
	identifier := fmt.Sprintf("%s/%s", bucket, objectPath)
	return GenerateItemFingerprint(resID, identifier)
}

// GenerateTableFingerprint 为关系数据库表生成指纹
//
// 参数:
//   - resID: 资源ID（来自 system.engines 表）
//   - schema: 模式名称（如 "public", "metadata"）
//   - tableName: 表名称
//
// 示例:
//   fingerprint := GenerateTableFingerprint(2, "public", "buildings")
//   // SHA256("2:public.buildings")
//
// 注意: 表数据变化不会改变指纹，指纹只依赖表的物理位置
func GenerateTableFingerprint(resID uint, schema, tableName string) string {
	identifier := fmt.Sprintf("%s.%s", schema, tableName)
	return GenerateItemFingerprint(resID, identifier)
}

// GenerateFileFingerprint 为文件系统文件生成指纹
//
// 参数:
//   - resID: 资源ID（来自 system.engines 表）
//   - filePath: 文件路径（绝对路径）
//
// 示例:
//   fingerprint := GenerateFileFingerprint(3, "/data/exports/users.csv")
//   // SHA256("3:/data/exports/users.csv")
func GenerateFileFingerprint(resID uint, filePath string) string {
	return GenerateItemFingerprint(resID, filePath)
}
