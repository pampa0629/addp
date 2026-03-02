package models

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

// GenerateAssetFingerprint 生成资产的唯一指纹，用于去重和快速识别
//
// 输入: tenantID + sourceModule + sourceReference
// 公式: SHA256(fmt.Sprintf("%d:%s:%s", tenantID, sourceModule, sourceReference))
//
// 示例:
//   fingerprint := GenerateAssetFingerprint(42, "meta", "1:public.buildings")
//   fingerprint := GenerateAssetFingerprint(42, "service", "123")
func GenerateAssetFingerprint(tenantID int64, sourceModule, sourceReference string) string {
	data := fmt.Sprintf("%d:%s:%s", tenantID, sourceModule, sourceReference)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// GenerateItemFingerprint 生成meta_item的唯一指纹
//
// 这是ADDP平台唯一的指纹计算函数，所有存储类型统一使用此函数。
//
// 指纹计算规则（两步计算）:
//   步骤1: 计算 full_name (identifier)
//     - 对象存储: bucket/path+name (如 "addp/image/开会.jpg")
//     - 数据库表: schema.table (如 "public.buildings")
//     - 文件系统: path+name (如 "/data/image/users.csv")
//   步骤2: 基于 engineID + identifier 的 SHA256 哈希
//     - 公式: SHA256(fmt.Sprintf("%d:%s", engineID, identifier))
//
// 推荐用法:
//   // 对象存储
//   fullName := JoinObjectPath(bucket, path, name)
//   fingerprint := GenerateItemFingerprint(engineID, fullName)
//
//   // 数据库表
//   fullName := fmt.Sprintf("%s.%s", schema, table)
//   fingerprint := GenerateItemFingerprint(engineID, fullName)
//
//   // 文件系统
//   fullName := path + name
//   fingerprint := GenerateItemFingerprint(engineID, fullName)
//
// 特性:
//   - 同一数据项的指纹始终不变，无论数据内容如何变化
//   - 用于去重、变更检测、数据血缘追踪
func GenerateItemFingerprint(resID uint, identifier string) string {
	data := fmt.Sprintf("%d:%s", resID, identifier)
	hash := sha256.Sum256([]byte(data))
	return hex.EncodeToString(hash[:])
}

// ValidateBucketName 验证存储桶名称
// bucket 不能包含路径分隔符
func ValidateBucketName(bucket string) error {
	if strings.Contains(bucket, "/") {
		return fmt.Errorf("bucket不能包含路径分隔符: %s", bucket)
	}
	if bucket == "" {
		return fmt.Errorf("bucket不能为空")
	}
	return nil
}

// ValidateDirectoryPath 验证目录路径
// 对象存储路径规则:
//   - path以/结尾（空字符串表示根目录）
//   - path不以/开头（相对路径）
//   - path不包含bucket名称
//
// 文件系统路径规则:
//   - path以/结尾
//   - path可以以/开头（绝对路径）
func ValidateDirectoryPath(path string, isObjectStorage bool) error {
	if path != "" && !strings.HasSuffix(path, "/") {
		return fmt.Errorf("目录路径必须以/结尾: %s", path)
	}
	if isObjectStorage && strings.HasPrefix(path, "/") {
		return fmt.Errorf("对象存储的相对路径不能以/开头: %s", path)
	}
	return nil
}

// SplitObjectPath 拆分完整对象路径为目录和文件名
//
// 参数:
//   - fullPath: 完整路径（如 "image/开会.jpg" 或 "开会.jpg"）
//
// 返回:
//   - dir: 目录路径（以/结尾，如 "image/"）
//   - name: 文件名（如 "开会.jpg"）
//
// 示例:
//   dir, name := SplitObjectPath("image/sub/开会.jpg")  // "image/sub/", "开会.jpg"
//   dir, name := SplitObjectPath("开会.jpg")           // "", "开会.jpg"
func SplitObjectPath(fullPath string) (dir, name string) {
	idx := strings.LastIndex(fullPath, "/")
	if idx == -1 {
		// 没有目录分隔符，整个是文件名
		return "", fullPath
	}
	// 目录部分包含末尾的/
	return fullPath[:idx+1], fullPath[idx+1:]
}

// JoinObjectPath 拼接完整对象路径
//
// 参数:
//   - bucket: 存储桶名称
//   - path: 目录路径（以/结尾）
//   - name: 文件名
//
// 返回:
//   - fullName: 完整路径（如 "addp/image/开会.jpg"）
//
// 示例:
//   fullName := JoinObjectPath("addp", "image/", "开会.jpg")  // "addp/image/开会.jpg"
//   fullName := JoinObjectPath("addp", "", "开会.jpg")        // "addp/开会.jpg"
func JoinObjectPath(bucket, path, name string) string {
	return fmt.Sprintf("%s/%s%s", bucket, path, name)
}
