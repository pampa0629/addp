package duckdb

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	commonClient "github.com/addp/common/client"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	commonModels "github.com/addp/common/models"
)

// SQLRewriter 将 SQL 中的对象存储表引用改写为 read_parquet() 调用
type SQLRewriter struct {
	metaClient *commonClient.MetaClient
	tenantID   uint
}

// NewSQLRewriter 创建 SQL 改写器
func NewSQLRewriter(metaClient *commonClient.MetaClient, tenantID uint) *SQLRewriter {
	return &SQLRewriter{
		metaClient: metaClient,
		tenantID:   tenantID,
	}
}

// tableRefPattern 匹配三段式 a.b.c
var tableRefPattern = regexp.MustCompile(
	`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\b`,
)

// twoPartRefPattern 匹配两段式 a.b
var twoPartRefPattern = regexp.MustCompile(
	`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\b`,
)

// ExtractTableRefs 从 SQL 中提取所有三段式表引用
func ExtractTableRefs(sql string) []string {
	matches := tableRefPattern.FindAllString(sql, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

// ExtractTwoPartTableRefs 从 SQL 中提取纯两段式引用（排除三段式中的前两段）
func ExtractTwoPartTableRefs(sql string) []string {
	threePartPrefixes := make(map[string]bool)
	for _, ref := range ExtractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) == 3 {
			threePartPrefixes[parts[0]+"."+parts[1]] = true
		}
	}
	matches := twoPartRefPattern.FindAllString(sql, -1)
	seen := make(map[string]bool)
	var result []string
	for _, m := range matches {
		if !seen[m] && !threePartPrefixes[m] {
			seen[m] = true
			result = append(result, m)
		}
	}
	return result
}

// BuildReadParquetExpr 根据物理路径构建 read_parquet() 表达式
func BuildReadParquetExpr(physicalPath string) string {
	path := physicalPath
	if !strings.HasPrefix(path, "s3://") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "s3://" + path
	}
	if !strings.HasSuffix(path, ".parquet") {
		path = strings.TrimRight(path, "/") + "/*.parquet"
	}
	return fmt.Sprintf("read_parquet('%s')", path)
}

// BuildObjectTableMap 构建对象存储表映射：engineName -> (schema.table 或 table) -> physicalPath
func BuildObjectTableMap(ctx context.Context, tenantID uint, engines []commonModels.Engine, metaClient *commonClient.MetaClient) map[string]map[string]string {
	result := make(map[string]map[string]string)
	if metaClient == nil {
		return result
	}
	metaClient = metaClient.WithTenantID(tenantID)
	for _, engine := range engines {
		if !IsObjectTableEngine(engine.EngineType) {
			continue
		}
		items, err := metaClient.ListEngineItems(engine.ID, "")
		if err != nil {
			continue
		}
		tables := make(map[string]string)
		for _, item := range items {
			descriptor, ok := ObjectTableDescriptorFromMetaItem(item)
			if !ok || descriptor.PhysicalPath == "" {
				continue
			}
			tables[item.Name] = descriptor.PhysicalPath
			if item.FullName != "" && item.FullName != item.Name {
				tables[item.FullName] = descriptor.PhysicalPath
			}
		}
		if len(tables) > 0 {
			result[engine.Name] = tables
			if sn := SanitizeName(engine.Name); sn != engine.Name {
				result[sn] = tables
			}
		}
	}
	return result
}

func IsObjectTableItem(item commonModels.MetaItem) bool {
	_, ok := ObjectTableDescriptorFromMetaItem(item)
	return ok
}

// ObjectTableDescriptorFromMetaItem returns the table descriptor for file/object catalog leaves DuckDB can read directly.
func ObjectTableDescriptorFromMetaItem(item commonModels.MetaItem) (dataitem.ItemDescriptor, bool) {
	switch item.ItemType {
	case "object", "file":
	default:
		return dataitem.ItemDescriptor{}, false
	}
	descriptor := dataitem.DescriptorFromAttributes(item.Attributes)
	if descriptor.DataType != datatype.Table {
		return dataitem.ItemDescriptor{}, false
	}
	switch descriptor.Format {
	case "parquet", "orc", "avro":
		return descriptor, true
	default:
		return dataitem.ItemDescriptor{}, false
	}
}

// RewriteWithEngines 使用已知引擎列表改写 SQL（避免重复查询 Meta）
// engineObjectTables: engineName -> tableName -> physicalPath
func (r *SQLRewriter) RewriteWithEngines(ctx context.Context, sql string, engineObjectTables map[string]map[string]string) (string, error) {
	if len(engineObjectTables) == 0 {
		return sql, nil
	}

	result := sql

	// 处理三段式引用 engine.schema.table
	for _, ref := range ExtractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) != 3 {
			continue
		}
		engineName, schema, table := parts[0], parts[1], parts[2]
		tables, ok := engineObjectTables[engineName]
		if !ok {
			continue
		}
		physicalPath, found := tables[schema+"."+table]
		if !found {
			physicalPath, found = tables[table]
		}
		if !found {
			continue
		}
		parquetExpr := BuildReadParquetExpr(physicalPath)
		result = strings.ReplaceAll(result, ref, parquetExpr)
	}

	// 处理两段式引用 engine.table（对象存储表无 schema 时）
	for engineName, tables := range engineObjectTables {
		for tableName, physicalPath := range tables {
			if strings.Contains(tableName, ".") {
				continue
			}
			twoPartRef := engineName + "." + tableName
			if strings.Contains(result, twoPartRef) {
				parquetExpr := BuildReadParquetExpr(physicalPath)
				result = strings.ReplaceAll(result, twoPartRef, parquetExpr)
			}
		}
	}

	return result, nil
}

// ExtractReferencedEngineNames 从 SQL 中提取可能的引擎名
func ExtractReferencedEngineNames(sql string) map[string]bool {
	names := make(map[string]bool)
	for _, ref := range ExtractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) == 3 {
			names[parts[0]] = true
		}
	}
	for _, ref := range ExtractTwoPartTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 2)
		if len(parts) == 2 {
			names[parts[0]] = true
		}
	}
	return names
}

// FilterEnginesByName 只保留名称在 referenced 集合中的引擎
func FilterEnginesByName(engines []commonModels.Engine, referenced map[string]bool) []commonModels.Engine {
	if len(referenced) == 0 {
		return nil
	}
	var result []commonModels.Engine
	for _, e := range engines {
		if referenced[e.Name] || referenced[SanitizeName(e.Name)] {
			result = append(result, e)
		}
	}
	return result
}
