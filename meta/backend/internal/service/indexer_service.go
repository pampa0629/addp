package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"reflect"
	"strconv"
	"time"

	commonClient "github.com/addp/common/client"
	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/models"
)

// IndexerService 将扫描事实投影到 Manager owner 的内容索引。
type IndexerService struct {
	contentIndex *commonClient.ManagerContentClient
	log          *slog.Logger
}

func NewIndexerService(contentIndex *commonClient.ManagerContentClient, log *slog.Logger) *IndexerService {
	return &IndexerService{
		contentIndex: contentIndex,
		log:          log,
	}
}

// DeleteTablesFromIndex 从索引中删除表
func (s *IndexerService) DeleteTablesFromIndex(tenantID, engineID uint, schemaName string) {
	if s.contentIndex == nil || schemaName == "" {
		return
	}
	if err := s.contentIndex.WithTenantID(tenantID).DeleteDocuments(context.Background(), commonClient.ManagerContentDeleteScope{
		EngineID: engineID, DataItemType: "table", Schema: schemaName,
	}); err != nil {
		s.log.Warn("删除表索引失败", "schema", schemaName, "engine_id", engineID, "error", err)
	}
}

// DeleteObjectsFromIndex 从索引中删除对象
func (s *IndexerService) DeleteObjectsFromIndex(tenantID, engineID uint, bucketName, path string) {
	if s.contentIndex == nil || bucketName == "" {
		return
	}
	if err := s.contentIndex.WithTenantID(tenantID).DeleteDocuments(context.Background(), commonClient.ManagerContentDeleteScope{
		EngineID: engineID, DataItemType: "object", Bucket: bucketName, PathPrefix: path,
	}); err != nil {
		s.log.Warn("删除对象索引失败", "bucket", bucketName, "path", path, "error", err)
	}
}

func normalizeContentMap(input map[string]interface{}) map[string]interface{} {
	if input == nil {
		return nil
	}
	output := make(map[string]interface{}, len(input))
	for key, value := range input {
		output[key] = normalizeContentValue(value)
	}
	return output
}

func normalizeContentValue(value interface{}) interface{} {
	switch typed := value.(type) {
	case time.Time:
		return typed.UTC()
	case *time.Time:
		if typed == nil {
			return nil
		}
		return typed.UTC()
	case map[string]interface{}:
		return normalizeContentMap(typed)
	case []interface{}:
		result := make([]interface{}, len(typed))
		for index, item := range typed {
			result[index] = normalizeContentValue(item)
		}
		return result
	}
	reflected := reflect.ValueOf(value)
	if reflected.IsValid() && reflected.Kind() == reflect.Map && reflected.Type().Key().Kind() == reflect.String {
		result := make(map[string]interface{}, reflected.Len())
		iterator := reflected.MapRange()
		for iterator.Next() {
			result[iterator.Key().String()] = normalizeContentValue(iterator.Value().Interface())
		}
		return result
	}
	return value
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
