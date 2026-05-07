package metapath

import (
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/models"
)

// SanitizeObjectPath 清理对象路径（去除前后空格和斜杠）。
func SanitizeObjectPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	return path
}

// SplitObjectPath 分割对象路径为 bucket 和相对路径。
// 例如："my-bucket/folder/file.txt" -> ("my-bucket", "folder/file.txt")。
func SplitObjectPath(path string) (string, string) {
	clean := SanitizeObjectPath(path)
	if clean == "" {
		return "", ""
	}
	parts := strings.SplitN(clean, "/", 2)
	bucket := parts[0]
	if bucket == "" {
		return "", ""
	}
	if len(parts) == 1 {
		return bucket, ""
	}
	return bucket, parts[1]
}

// PrepareObjectPaths 准备对象路径列表（去重、排序）。
// 优先级：paths > fallback > scanner.AllowedBuckets()。
func PrepareObjectPaths(paths, fallback []string, scanner format.ObjectStorageScanner) []string {
	pathSet := map[string]struct{}{}
	for _, p := range paths {
		clean := SanitizeObjectPath(p)
		if clean != "" {
			pathSet[clean] = struct{}{}
		}
	}

	if len(pathSet) == 0 {
		for _, p := range fallback {
			clean := SanitizeObjectPath(p)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	if len(pathSet) == 0 && scanner != nil {
		for _, bucket := range scanner.AllowedBuckets() {
			clean := SanitizeObjectPath(bucket)
			if clean != "" {
				pathSet[clean] = struct{}{}
			}
		}
	}

	var result []string
	for p := range pathSet {
		result = append(result, p)
	}
	sort.Strings(result)
	return result
}

// ComposeNodeFullName 基于父节点 full_name 拼接当前节点 full_name。
func ComposeNodeFullName(name string, parent *models.MetaNode, separator string) string {
	if parent == nil || parent.FullName == "" {
		return name
	}
	if separator == "" {
		separator = "."
	}
	return parent.FullName + separator + name
}

// FileCatalogPathFromFSPath 将文件系统路径转换为统一 CatalogPath。
func FileCatalogPathFromFSPath(engineID uint, rawPath string) plugin.CatalogPath {
	path := plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermRoot,
			Kind: plugin.CatalogKindRoot,
			Name: "/",
		}},
	}
	trimmed := strings.Trim(rawPath, "/")
	if trimmed == "" || trimmed == "." {
		return path
	}
	for _, part := range strings.Split(trimmed, "/") {
		if part == "" {
			continue
		}
		path.Segments = append(path.Segments, plugin.CatalogSegment{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindPrefix,
			Name: part,
		})
	}
	return path
}

// JoinFSPath 拼接文件系统 full_name。
func JoinFSPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

// RootFSIdentifier 返回文件系统根节点的 full_name 标识。
func RootFSIdentifier(engineType, rootPath string) string {
	switch strings.ToLower(engineType) {
	case "nfs", "nas":
		return ""
	default:
		return rootPath
	}
}
