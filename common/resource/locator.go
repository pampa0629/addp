package resource

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ResourceType 资源类型
type ResourceType string

const (
	TypeTable        ResourceType = "table"
	TypeCollection   ResourceType = "collection"
	TypeLabel        ResourceType = "label"        // 图数据库节点标签
	TypeRelationship ResourceType = "relationship" // 图数据库关系类型
	TypeObject       ResourceType = "object"       // 对象存储文件
	TypeFile         ResourceType = "file"         // 文件系统文件（NFS/本地FS）
	TypeDirectory    ResourceType = "directory"
	TypeDatabase     ResourceType = "database"
	TypeSchema       ResourceType = "schema"
	TypeBucket       ResourceType = "bucket" // 对象存储桶
	TypeRoot         ResourceType = "root"   // 文件系统根目录
	TypeDir          ResourceType = "dir"    // 文件系统子目录
	TypeUnknown      ResourceType = "unknown"
)

// ResourceLocator 资源定位符
// 使用 addp:// 协议的 URI 系统来唯一标识平台中的任何资源
//
// URI 格式: addp://engine/{engine_id}/path/{resource_path}?type={type}&meta_id={meta_id}
//
// 示例:
//   - PostgreSQL 表: addp://engine/1/path/public/users?type=table
//   - MongoDB 集合: addp://engine/2/path/business/orders?type=collection
//   - MinIO 对象: addp://engine/3/path/uploads/2024/geo/data.shp?type=object
//   - MinIO 目录: addp://engine/3/path/uploads/2024?type=directory
type ResourceLocator struct {
	EngineID uint         `json:"engine_id"`
	Path     []string     `json:"path"`              // 资源路径，如 ["public", "users"]
	Type     ResourceType `json:"type"`              // 资源类型
	MetaID   *uint        `json:"meta_id,omitempty"` // 可选：Meta 模块的节点 ID
}

// ParseURI 解析 ResourceLocator URI
// 参数:
//   - uri: ResourceLocator URI 字符串，如 "addp://engine/1/path/public/users?type=table"
//
// 返回:
//   - *ResourceLocator: 解析后的资源定位符
//   - error: 解析错误
func ParseURI(uri string) (*ResourceLocator, error) {
	// 解析 URL
	u, err := url.Parse(uri)
	if err != nil {
		return nil, fmt.Errorf("invalid URI: %w", err)
	}

	// 验证协议
	if u.Scheme != "addp" {
		return nil, fmt.Errorf("invalid scheme: expected 'addp', got '%s'", u.Scheme)
	}

	// 解析路径：//engine/{id}/path/{path}
	// 注意：url.Parse 会将 //engine 解析为 Host，我们需要从 Host 和 Path 组合解析
	pathStr := strings.Trim(u.Host+u.Path, "/")
	parts := strings.Split(pathStr, "/")

	if len(parts) < 3 || parts[0] != "engine" || parts[2] != "path" {
		return nil, fmt.Errorf("invalid path format: expected 'engine/{id}/path/{path}', got '%s'", pathStr)
	}

	// 解析 engine_id
	engineID, err := strconv.ParseUint(parts[1], 10, 32)
	if err != nil {
		return nil, fmt.Errorf("invalid engine_id: %w", err)
	}

	// 解析资源路径（URL 解码）
	path := []string{}
	if len(parts) > 3 {
		for _, p := range parts[3:] {
			decoded, err := url.PathUnescape(p)
			if err != nil {
				return nil, fmt.Errorf("failed to decode path segment '%s': %w", p, err)
			}
			if decoded != "" {
				path = append(path, decoded)
			}
		}
	}

	// 解析查询参数
	query := u.Query()
	resType := ResourceType(query.Get("type"))
	if resType == "" {
		return nil, fmt.Errorf("missing required parameter: type")
	}

	var metaID *uint
	if metaIDStr := query.Get("meta_id"); metaIDStr != "" {
		mid, err := strconv.ParseUint(metaIDStr, 10, 32)
		if err == nil {
			metaIDUint := uint(mid)
			metaID = &metaIDUint
		}
	}

	return &ResourceLocator{
		EngineID: uint(engineID),
		Path:     path,
		Type:     resType,
		MetaID:   metaID,
	}, nil
}

// ToURI 将 ResourceLocator 转换为 URI 字符串
// 返回: ResourceLocator URI，如 "addp://engine/1/path/public/users?type=table"
func (r *ResourceLocator) ToURI() string {
	// 编码路径
	encodedPath := make([]string, len(r.Path))
	for i, p := range r.Path {
		encodedPath[i] = url.PathEscape(p)
	}
	pathStr := strings.Join(encodedPath, "/")

	// 构建 URI
	uri := fmt.Sprintf("addp://engine/%d/path/%s?type=%s", r.EngineID, pathStr, r.Type)
	if r.MetaID != nil {
		uri += fmt.Sprintf("&meta_id=%d", *r.MetaID)
	}
	return uri
}

// PathString 返回路径字符串（不编码）
// 返回: 路径字符串，如 "public/users"
func (r *ResourceLocator) PathString() string {
	return strings.Join(r.Path, "/")
}

// FullName 返回完整名称（根据类型格式化）
// 返回:
//   - 表/集合: schema.table 格式（如 "public.users"）
//   - 对象/目录: bucket/path 格式（如 "uploads/2024/data.shp"）
//   - 其他: 直接返回路径字符串
func (r *ResourceLocator) FullName() string {
	switch r.Type {
	case TypeTable, TypeCollection:
		// PostgreSQL/MongoDB: schema.table 或 database.collection
		if len(r.Path) >= 2 {
			return r.Path[len(r.Path)-2] + "." + r.Path[len(r.Path)-1]
		}
		return r.PathString()
	case TypeObject, TypeDirectory:
		// 对象存储: bucket/path/to/file
		return r.PathString()
	default:
		return r.PathString()
	}
}

// LastSegment 返回路径的最后一段
// 返回: 最后一段路径，如 "users"
func (r *ResourceLocator) LastSegment() string {
	if len(r.Path) == 0 {
		return ""
	}
	return r.Path[len(r.Path)-1]
}

// ParentPath 返回父路径
// 返回: 父路径的 ResourceLocator（类型为 directory 或 schema）
func (r *ResourceLocator) ParentPath() *ResourceLocator {
	if len(r.Path) == 0 {
		return nil
	}

	parentPath := make([]string, len(r.Path)-1)
	copy(parentPath, r.Path[:len(r.Path)-1])

	// 推断父节点类型
	var parentType ResourceType
	switch r.Type {
	case TypeTable:
		parentType = TypeSchema
	case TypeCollection:
		parentType = TypeDatabase
	case TypeObject:
		parentType = TypeDirectory
	default:
		parentType = TypeDirectory
	}

	return &ResourceLocator{
		EngineID: r.EngineID,
		Path:     parentPath,
		Type:     parentType,
		MetaID:   nil, // 父节点的 MetaID 需要单独查询
	}
}

// Clone 创建 ResourceLocator 的深拷贝
func (r *ResourceLocator) Clone() *ResourceLocator {
	path := make([]string, len(r.Path))
	copy(path, r.Path)

	var metaID *uint
	if r.MetaID != nil {
		mid := *r.MetaID
		metaID = &mid
	}

	return &ResourceLocator{
		EngineID: r.EngineID,
		Path:     path,
		Type:     r.Type,
		MetaID:   metaID,
	}
}
