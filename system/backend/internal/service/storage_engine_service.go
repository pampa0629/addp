package service

import (
	"context"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/system/internal/models"
)

type StorageEngineService struct{}

func NewStorageEngineService() *StorageEngineService {
	return &StorageEngineService{}
}

// TestConnection 测试引擎连接。
// 连接测试统一由插件 TestConnectionProvider 实现，且必须保持只读。
func (s *StorageEngineService) TestConnection(resource *models.Engine) error {
	return dbbridge.TestConnection(context.Background(), resource)
}

// GetConnectionInfo 获取存储引擎连接信息（用于前端展示，隐藏敏感信息）
func (s *StorageEngineService) GetConnectionInfo(resource *models.Engine) map[string]interface{} {
	result := make(map[string]interface{})
	result["type"] = resource.EngineType

	// 从插件系统获取敏感字段列表
	sensitiveFields, err := dbbridge.GetSensitiveFields(resource.EngineType)
	if err != nil {
		// 降级：使用默认敏感字段列表
		sensitiveFields = []string{"password", "secret_key", "access_key", "token"}
	}

	// 创建敏感字段的快速查找map
	sensitiveMap := make(map[string]bool)
	for _, field := range sensitiveFields {
		sensitiveMap[field] = true
	}

	// 遍历所有连接信息字段
	for key, value := range resource.ConnectionInfo {
		if sensitiveMap[key] {
			// 敏感字段：access_key部分隐藏，其他完全隐藏
			if key == "access_key" {
				result[key] = maskString(value)
			} else {
				result[key] = "******"
			}
		} else {
			// 非敏感字段：直接返回
			result[key] = value
		}
	}

	return result
}

// maskString 部分隐藏字符串（仅用于access_key等需要部分显示的字段）
func maskString(value interface{}) string {
	if str, ok := value.(string); ok {
		if len(str) <= 8 {
			return "****"
		}
		return str[:4] + "****" + str[len(str)-4:]
	}
	return "****"
}

// NamespaceInfo 表示 catalog 第一层命名空间。
type NamespaceInfo struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
}

// CatalogItemInfo 表示 catalog 叶子数据项。
type CatalogItemInfo struct {
	Name        string `json:"name"`
	Namespace   string `json:"namespace"`
	Type        string `json:"type,omitempty"`
	Description string `json:"description,omitempty"`
}

// ListNamespaces 列出指定资源的 catalog 命名空间。
func (s *StorageEngineService) ListNamespaces(resource *models.Engine) ([]NamespaceInfo, error) {
	nodes, err := dbbridge.ListNamespaces(context.Background(), resource)
	if err != nil {
		return nil, err
	}
	namespaces := make([]NamespaceInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsContainer {
			continue
		}
		namespaces = append(namespaces, NamespaceInfo{
			Name:        node.Name,
			Description: stringAttribute(node.Attributes, "description"),
		})
	}
	return namespaces, nil
}

// ListCatalogItems 列出指定命名空间下的 catalog 叶子数据项。
func (s *StorageEngineService) ListCatalogItems(resource *models.Engine, namespace string) ([]CatalogItemInfo, error) {
	nodes, err := dbbridge.ListItems(context.Background(), resource, namespace)
	if err != nil {
		return nil, err
	}
	items := make([]CatalogItemInfo, 0, len(nodes))
	for _, node := range nodes {
		if !node.IsItem {
			continue
		}
		items = append(items, CatalogItemInfo{
			Name:        node.Name,
			Namespace:   namespace,
			Type:        node.Kind,
			Description: stringAttribute(node.Attributes, "description"),
		})
	}
	return items, nil
}

// ListCatalogChildren 列出指定 catalog 路径下的实时子节点。
func (s *StorageEngineService) ListCatalogChildren(resource *models.Engine, req models.CatalogListChildrenRequest) ([]models.CatalogNode, error) {
	nodes, err := dbbridge.ListCatalogChildren(context.Background(), resource, toPluginCatalogPath(req.Path), plugin.ListOptions{
		Recursive: req.Options.Recursive,
		Limit:     req.Options.Limit,
		Offset:    req.Options.Offset,
		Filter:    req.Filter,
	})
	if err != nil {
		return nil, err
	}

	result := make([]models.CatalogNode, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, fromPluginCatalogNode(node))
	}
	return result, nil
}

func toPluginCatalogPath(path models.CatalogPath) plugin.CatalogPath {
	segments := make([]plugin.CatalogSegment, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, plugin.CatalogSegment{
			Term: segment.Term,
			Kind: segment.Kind,
			Name: segment.Name,
		})
	}
	return plugin.CatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: segments,
	}
}

func fromPluginCatalogNode(node plugin.CatalogNode) models.CatalogNode {
	return models.CatalogNode{
		Name:        node.Name,
		Path:        fromPluginCatalogPath(node.Path),
		Term:        node.Term,
		Kind:        node.Kind,
		IsContainer: node.IsContainer,
		IsItem:      node.IsItem,
		Stats:       node.Stats,
		Attributes:  node.Attributes,
		Actions:     node.Actions,
	}
}

func fromPluginCatalogPath(path plugin.CatalogPath) models.CatalogPath {
	segments := make([]models.CatalogSegment, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, models.CatalogSegment{
			Term: segment.Term,
			Kind: segment.Kind,
			Name: segment.Name,
		})
	}
	return models.CatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: segments,
	}
}

func stringAttribute(attributes map[string]interface{}, key string) string {
	if attributes == nil {
		return ""
	}
	if value, ok := attributes[key].(string); ok {
		return value
	}
	return ""
}
