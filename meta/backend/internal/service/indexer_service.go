package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
	"github.com/addp/meta/internal/search"
)

// IndexerService 负责管理 Meilisearch 索引
// 提取自 ScanService，消除循环依赖
type IndexerService struct {
	indexer *search.Indexer
	log     *slog.Logger
}

// NewIndexerService 创建索引服务
func NewIndexerService(indexer *search.Indexer, log *slog.Logger) *IndexerService {
	return &IndexerService{
		indexer: indexer,
		log:     log,
	}
}

// DeleteTablesFromIndex 从索引中删除表
func (s *IndexerService) DeleteTablesFromIndex(tenantID, engineID uint, schemaName string) {
	if s.indexer == nil || !s.indexer.Enabled() || schemaName == "" {
		return
	}
	if err := s.indexer.DeleteTables(context.Background(), tenantID, engineID, schemaName); err != nil {
		s.log.Warn("删除表索引失败", "schema", schemaName, "engine_id", engineID, "error", err)
	}
}

// DeleteObjectsFromIndex 从索引中删除对象
func (s *IndexerService) DeleteObjectsFromIndex(tenantID, engineID uint, bucketName, path string) {
	if s.indexer == nil || !s.indexer.Enabled() || bucketName == "" {
		return
	}
	if err := s.indexer.DeleteObjects(context.Background(), tenantID, engineID, bucketName, path); err != nil {
		s.log.Warn("删除对象索引失败", "bucket", bucketName, "path", path, "error", err)
	}
}

// 辅助函数（从 scan_service.go 复制）

func copyJSONMap(m models.JSONMap) map[string]interface{} {
	if m == nil {
		return nil
	}
	result := make(map[string]interface{}, len(m))
	for k, v := range m {
		result[k] = v
	}
	return result
}

func extractStringSlice(value interface{}) []string {
	switch v := value.(type) {
	case nil:
		return nil
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if item != "" {
				out = append(out, item)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, raw := range v {
			if s, ok := raw.(string); ok && s != "" {
				out = append(out, s)
			}
		}
		if len(out) == 0 {
			return nil
		}
		return out
	case string:
		if v == "" {
			return nil
		}
		return []string{v}
	default:
		return nil
	}
}

func getStringFromMap(metadata map[string]interface{}, key string) string {
	if metadata == nil {
		return ""
	}
	if val, ok := metadata[key]; ok {
		switch v := val.(type) {
		case string:
			return v
		case fmt.Stringer:
			return v.String()
		case []byte:
			return string(v)
		default:
			return fmt.Sprintf("%v", v)
		}
	}
	return ""
}

func stringFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) string {
	return getStringFromMap(standardAttributes(metadata, sectionPath), key)
}

func stringSliceFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) []string {
	return extractStringSlice(valueFromStandardAttributes(metadata, sectionPath, key))
}

func intFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) int {
	return intFromInterface(valueFromStandardAttributes(metadata, sectionPath, key))
}

func timeFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) *time.Time {
	return extractTimePtr(valueFromStandardAttributes(metadata, sectionPath, key))
}

func valueFromStandardAttributes(metadata map[string]interface{}, sectionPath, key string) interface{} {
	if section := standardAttributes(metadata, sectionPath); section != nil {
		return section[key]
	}
	return nil
}

func standardAttributes(metadata map[string]interface{}, sectionPath string) map[string]interface{} {
	return commonJSON.Section(metadata, sectionPath)
}

func intFromInterface(value interface{}) int {
	switch v := value.(type) {
	case int:
		return v
	case int32:
		return int(v)
	case int64:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		if i64, err := v.Int64(); err == nil {
			return int(i64)
		}
	case string:
		if v == "" {
			return 0
		}
		if i64, err := strconv.ParseInt(v, 10, 64); err == nil {
			return int(i64)
		}
	}
	return 0
}

func extractTimePtr(value interface{}) *time.Time {
	switch v := value.(type) {
	case time.Time:
		if v.IsZero() {
			return nil
		}
		vv := v
		return &vv
	case *time.Time:
		if v == nil || v.IsZero() {
			return nil
		}
		vv := v.UTC()
		return &vv
	case string:
		if v == "" {
			return nil
		}
		if parsed, err := time.Parse(time.RFC3339, v); err == nil {
			return &parsed
		}
	}
	return nil
}
