package metapath

import (
	"sort"
	"strings"

	"github.com/addp/meta/internal/metacatalog"
	"github.com/addp/meta/internal/models"
)

// SanitizeObjectPath 清理对象路径（去除前后空格和斜杠）。
func SanitizeObjectPath(path string) string {
	path = strings.TrimSpace(path)
	path = strings.Trim(path, "/")
	return path
}

func JoinObjectPathParts(parts ...string) string {
	cleaned := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.Trim(part, "/")
		if part == "" {
			continue
		}
		cleaned = append(cleaned, part)
	}
	return strings.Join(cleaned, "/")
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

// PrepareCatalogPaths 准备 catalog 路径列表（去重、排序）。
// 优先级：paths > fallback。
func PrepareCatalogPaths(paths, fallback []string) []string {
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

// JoinFSPath 拼接文件系统 full_name。
func JoinFSPath(parent, name string) string {
	if parent == "" {
		return name
	}
	return parent + "/" + name
}

func FilterObjectResourcesForDepth(resources []metacatalog.StorageResource, basePath string) []metacatalog.StorageResource {
	base := SanitizeObjectPath(basePath)
	if len(resources) == 0 {
		return resources
	}

	filtered := make([]metacatalog.StorageResource, 0, len(resources))
	for _, resource := range resources {
		if resource.NodeType == "bucket" {
			filtered = append(filtered, resource)
			continue
		}

		relative := SanitizeObjectPath(resource.Path)
		trimmed := relative
		if base != "" {
			switch {
			case trimmed == base:
				trimmed = ""
			case strings.HasPrefix(trimmed, base+"/"):
				trimmed = strings.TrimPrefix(trimmed, base+"/")
			}
		}

		switch strings.ToLower(resource.NodeType) {
		case "prefix":
			if trimmed == "" || !strings.Contains(trimmed, "/") {
				filtered = append(filtered, resource)
			}
		case "object":
			if trimmed != "" && strings.Contains(trimmed, "/") {
				continue
			}
			filtered = append(filtered, resource)
		default:
			filtered = append(filtered, resource)
		}
	}
	return filtered
}
