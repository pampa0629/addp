package resourcetree

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// ResourceType 资源类型
type ResourceType string

const (
	TypeTable      ResourceType = "table"
	TypeCollection ResourceType = "collection"
	TypeGraph      ResourceType = "graph"  // 图数据库整体
	TypeObject     ResourceType = "object" // 对象存储文件
	TypeFile       ResourceType = "file"   // 文件系统文件（NFS/本地FS）
	TypeDirectory  ResourceType = "directory"
	TypePrefix     ResourceType = "prefix"
	TypeDatabase   ResourceType = "database"
	TypeSchema     ResourceType = "schema"
	TypeBucket     ResourceType = "bucket"  // 对象存储桶
	TypeRoot       ResourceType = "root"    // 文件系统结构根
	TypeServer     ResourceType = "server"  // 数据库/namespace 引擎结构根
	TypeService    ResourceType = "service" // 对象存储服务结构根
	TypeDir        ResourceType = "dir"     // 文件系统子目录
	TypeUnknown    ResourceType = "unknown"
)

// ResourceLocator 资源定位符
// 使用 addp:// 协议的 URI 系统来唯一标识平台中的任何资源
//
// URI 格式: addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}&item_id={item_id}
//
// 示例:
//   - PostgreSQL 表: addp://engine/1/path/public/users?type=table
//   - MongoDB 集合: addp://engine/2/path/business/orders?type=collection
//   - MinIO 对象: addp://engine/3/path/uploads/2024/geo/data.shp?type=object
//   - MinIO 目录: addp://engine/3/path/uploads/2024?type=directory
type ResourceLocator struct {
	EngineID uint         `json:"engine_id"`
	Path     []string     `json:"path"`              // 资源路径，如 ["public", "users"]
	Type     ResourceType `json:"type"`              // catalog 术语，不表示内容语义
	NodeID   *uint        `json:"node_id,omitempty"` // 可选：MetaNode ID
	ItemID   *uint        `json:"item_id,omitempty"` // 可选：MetaItem ID
}

// LocatorFromFullName 根据 catalog 的 full_name 与资源类型构造标准 ResourceLocator。
func LocatorFromFullName(engineID uint, engineType, resourceType, fullName string, itemID *uint) *ResourceLocator {
	resourceType = strings.TrimSpace(resourceType)
	fullName = strings.TrimSpace(fullName)
	if engineID == 0 || resourceType == "" || fullName == "" {
		return nil
	}
	return &ResourceLocator{
		EngineID: engineID,
		Path:     ParseFullNamePath(engineType, resourceType, fullName),
		Type:     ResourceType(resourceType),
		ItemID:   itemID,
	}
}

// EngineRootLocator 构建引擎根节点的 ResourceLocator URI。
func EngineRootLocator(engineID uint) string {
	return EngineRootLocatorForType(engineID, TypeRoot)
}

// EngineRootLocatorForType 构建带显性 catalog root 术语的引擎根节点 ResourceLocator URI。
func EngineRootLocatorForType(engineID uint, rootType ResourceType) string {
	if !IsRootResourceType(rootType) {
		rootType = TypeRoot
	}
	return fmt.Sprintf("addp://engine/%d/path/?type=%s", engineID, rootType)
}

// IsRootResourceType 判断 ResourceLocator type 是否表达结构性 catalog root。
func IsRootResourceType(resourceType ResourceType) bool {
	switch resourceType {
	case TypeRoot, TypeServer, TypeService:
		return true
	default:
		return false
	}
}

// ParseFullNamePath 按 ResourceLocator 规范将 full_name 拆为 path segments。
func ParseFullNamePath(engineType, resourceType, fullName string) []string {
	fullName = strings.TrimSpace(fullName)
	if fullName == "" {
		return []string{}
	}
	if UsesSlashFullName(engineType, resourceType) {
		return splitLocatorSlashPath(fullName)
	}
	return splitLocatorDotPath(fullName)
}

// UsesSlashFullName 判断 catalog full_name 是否使用 slash 路径语义。
func UsesSlashFullName(engineType, resourceType string) bool {
	switch strings.ToLower(strings.TrimSpace(resourceType)) {
	case string(TypeBucket), "prefix", string(TypeDirectory), string(TypeObject), string(TypeRoot), string(TypeServer), string(TypeService), string(TypeDir), string(TypeFile):
		return true
	}
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3", "nfs", "nas":
		return true
	default:
		return false
	}
}

func splitLocatorSlashPath(value string) []string {
	trimmed := strings.Trim(value, "/")
	if trimmed == "" {
		return []string{}
	}
	parts := strings.Split(trimmed, "/")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitLocatorDotPath(value string) []string {
	parts := strings.Split(value, ".")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
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

	var nodeID *uint
	if nodeIDStr := query.Get("node_id"); nodeIDStr != "" {
		nid, err := strconv.ParseUint(nodeIDStr, 10, 32)
		if err != nil || nid == 0 {
			return nil, fmt.Errorf("invalid node_id: %s", nodeIDStr)
		}
		nodeIDUint := uint(nid)
		nodeID = &nodeIDUint
	}
	var itemID *uint
	if itemIDStr := query.Get("item_id"); itemIDStr != "" {
		iid, err := strconv.ParseUint(itemIDStr, 10, 32)
		if err != nil || iid == 0 {
			return nil, fmt.Errorf("invalid item_id: %s", itemIDStr)
		}
		itemIDUint := uint(iid)
		itemID = &itemIDUint
	}
	if nodeID != nil && itemID != nil {
		return nil, fmt.Errorf("node_id and item_id are mutually exclusive")
	}

	return &ResourceLocator{
		EngineID: uint(engineID),
		Path:     path,
		Type:     resType,
		NodeID:   nodeID,
		ItemID:   itemID,
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
	if r.NodeID != nil {
		uri += fmt.Sprintf("&node_id=%d", *r.NodeID)
	}
	if r.ItemID != nil {
		uri += fmt.Sprintf("&item_id=%d", *r.ItemID)
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
	}
}

// Clone 创建 ResourceLocator 的深拷贝
func (r *ResourceLocator) Clone() *ResourceLocator {
	path := make([]string, len(r.Path))
	copy(path, r.Path)

	var nodeID *uint
	if r.NodeID != nil {
		nid := *r.NodeID
		nodeID = &nid
	}
	var itemID *uint
	if r.ItemID != nil {
		iid := *r.ItemID
		itemID = &iid
	}

	return &ResourceLocator{
		EngineID: r.EngineID,
		Path:     path,
		Type:     r.Type,
		NodeID:   nodeID,
		ItemID:   itemID,
	}
}
