package service

import (
	"context"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/engine/plugin"
	engineselection "github.com/addp/common/engine/selection"
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

// ProbeWorkflowRuntimeContract checks the workflow runtime protocol surface for
// engines that declare compute.workflow support.
func (s *StorageEngineService) ProbeWorkflowRuntimeContract(resource *models.Engine) (int, error) {
	return dbbridge.ProbeWorkflowRuntimeContract(context.Background(), resource)
}

func (s *StorageEngineService) ShouldProbeWorkflowRuntime(resource *models.Engine) bool {
	return engineselection.SupportsComputeEntrypoint(resource, "workflow")
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

// ListEngineCatalogChildren 列出指定 catalog 路径下的实时子节点。
func (s *StorageEngineService) ListEngineCatalogChildren(ctx context.Context, resource *models.Engine, req models.EngineCatalogListChildrenRequest) ([]models.EngineCatalogEntry, error) {
	nodes, err := dbbridge.ListEngineCatalogChildren(ctx, resource, toPluginEngineCatalogPath(req.Path), plugin.ListOptions{
		Recursive: req.Options.Recursive,
		Limit:     req.Options.Limit,
		Offset:    req.Options.Offset,
	})
	if err != nil {
		return nil, err
	}

	result := make([]models.EngineCatalogEntry, 0, len(nodes))
	for _, node := range nodes {
		result = append(result, fromPluginEngineCatalogEntry(node))
	}
	return result, nil
}

// DescribeEngineCatalogFacts 返回指定 catalog 叶子的实时结构事实。
func (s *StorageEngineService) DescribeEngineCatalogFacts(ctx context.Context, resource *models.Engine, req models.EngineCatalogDescribeFactsRequest) (*plugin.EngineCatalogFacts, error) {
	return dbbridge.DescribeEngineCatalogFacts(ctx, resource, toPluginEngineCatalogPath(req.Path), plugin.EngineCatalogFactsOptions{})
}

func toPluginEngineCatalogPath(path models.EngineCatalogPath) plugin.EngineCatalogPath {
	segments := make([]plugin.EngineCatalogSegment, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, plugin.EngineCatalogSegment{
			Term: segment.Term,
			Kind: segment.Kind,
			Name: segment.Name,
		})
	}
	return plugin.EngineCatalogPath{
		Version:  path.Version,
		EngineID: path.EngineID,
		Segments: segments,
	}
}

func fromPluginEngineCatalogEntry(node plugin.EngineCatalogEntry) models.EngineCatalogEntry {
	return models.EngineCatalogEntry{
		Name:      node.Name,
		Path:      fromPluginEngineCatalogPath(node.Path),
		Term:      node.Term,
		Kind:      node.Kind,
		Role:      node.Role,
		Table:     node.Table,
		Storage:   node.Storage,
		LeafCount: node.LeafCount,
		UpdatedAt: node.UpdatedAt,
	}
}

func fromPluginEngineCatalogPath(path plugin.EngineCatalogPath) models.EngineCatalogPath {
	segments := make([]models.EngineCatalogSegment, 0, len(path.Segments))
	for _, segment := range path.Segments {
		segments = append(segments, models.EngineCatalogSegment{
			Term: segment.Term,
			Kind: segment.Kind,
			Name: segment.Name,
		})
	}
	return models.EngineCatalogPath{
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
