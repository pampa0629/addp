package service

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	commonClient "github.com/addp/common/client"
)

// SQLRewriter 将 SQL 中的湖表引用改写为 read_parquet() 调用
// 输入：SELECT * FROM my_minio.warehouse.orders o JOIN my_pg.public.customers c ON o.id=c.id
// 输出：SELECT * FROM read_parquet('s3://bucket/warehouse/orders/*.parquet') o JOIN my_pg.public.customers c ON o.id=c.id
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

// lakeTableRef 湖表引用信息
type lakeTableRef struct {
	engineName   string
	schema       string
	table        string
	physicalPath string // s3://bucket/prefix/
}

// Rewrite 改写 SQL 中的湖表引用
func (r *SQLRewriter) Rewrite(ctx context.Context, sql string) (string, error) {
	if r.metaClient == nil {
		return sql, nil
	}

	// 找出所有三段式引用 engine.schema.table 或 engine.table
	refs := extractTableRefs(sql)
	if len(refs) == 0 {
		return sql, nil
	}

	// 查询 Meta，判断哪些是湖表
	lakeRefs, err := r.resolveLakeTables(ctx, refs)
	if err != nil {
		// 查询失败时降级，返回原始 SQL
		return sql, fmt.Errorf("resolve lake tables: %w", err)
	}

	if len(lakeRefs) == 0 {
		return sql, nil
	}

	// 替换 SQL 中的湖表引用
	result := sql
	for _, ref := range lakeRefs {
		result = replaceLakeTableRef(result, ref)
	}

	return result, nil
}

// tableRefPattern 匹配三段式 a.b.c
var tableRefPattern = regexp.MustCompile(
	`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\b`,
)

// twoPartRefPattern 匹配两段式 a.b（不含三段式中的 a.b 部分）
var twoPartRefPattern = regexp.MustCompile(
	`\b([a-zA-Z_][a-zA-Z0-9_]*)\.([a-zA-Z_][a-zA-Z0-9_]*)\b`,
)

// extractTableRefs 从 SQL 中提取所有三段式表引用
func extractTableRefs(sql string) []string {
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

// extractTwoPartTableRefs 从 SQL 中提取纯两段式引用（排除三段式中的前两段）
func extractTwoPartTableRefs(sql string) []string {
	// 先收集三段式的前两段，避免重复
	threePartPrefixes := make(map[string]bool)
	for _, ref := range extractTableRefs(sql) {
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

// resolveLakeTables 查询 Meta，返回确认为湖表的引用列表
func (r *SQLRewriter) resolveLakeTables(ctx context.Context, refs []string) ([]lakeTableRef, error) {
	// 按 engineName 分组，减少 Meta 请求次数
	// 先获取所有引擎的元数据树（缓存在本次请求内）
	engineTrees := make(map[string]map[string]string) // engineName -> tableName -> physicalPath

	for _, ref := range refs {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) != 3 {
			continue
		}
		engineName := parts[0]
		if _, loaded := engineTrees[engineName]; !loaded {
			engineTrees[engineName] = nil // 标记已尝试加载
		}
	}

	// 加载各引擎的湖表信息（通过 GetMetadataTree）
	// 注意：这里需要 engineID，但我们只有 engineName
	// 实际上 duckdb_service 已经有引擎列表，这里通过 metaClient 的 GetMetadataTree 需要 engineID
	// 为简化，我们在 Rewriter 中接受预构建的湖表映射
	// 此处返回空，由调用方（duckdb_service）传入预构建的映射
	return nil, nil
}

// replaceLakeTableRef 将 SQL 中的湖表引用替换为 read_parquet()
func replaceLakeTableRef(sql string, ref lakeTableRef) string {
	// 构建 read_parquet 路径
	parquetExpr := buildReadParquetExpr(ref.physicalPath)

	// 替换三段式引用 engine.schema.table
	threePartRef := fmt.Sprintf("%s.%s.%s", ref.engineName, ref.schema, ref.table)
	return strings.ReplaceAll(sql, threePartRef, parquetExpr)
}

// buildReadParquetExpr 根据物理路径构建 read_parquet() 表达式
func buildReadParquetExpr(physicalPath string) string {
	path := physicalPath
	// 相对路径自动加 s3:// 前缀（格式：bucket/key）
	if !strings.HasPrefix(path, "s3://") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
		path = "s3://" + path
	}
	if !strings.HasSuffix(path, ".parquet") {
		// 目录路径：读取目录下所有 parquet 文件
		path = strings.TrimRight(path, "/") + "/*.parquet"
	}
	return fmt.Sprintf("read_parquet('%s')", path)
}

// RewriteWithEngines 使用已知引擎列表改写 SQL（避免重复查询 Meta）
// engineLakeTables: engineName -> tableName -> physicalPath
func (r *SQLRewriter) RewriteWithEngines(ctx context.Context, sql string, engineLakeTables map[string]map[string]string) (string, error) {
	if len(engineLakeTables) == 0 {
		return sql, nil
	}

	result := sql

	// 处理三段式引用 engine.schema.table
	for _, ref := range extractTableRefs(sql) {
		parts := strings.SplitN(ref, ".", 3)
		if len(parts) != 3 {
			continue
		}
		engineName, schema, table := parts[0], parts[1], parts[2]
		tables, ok := engineLakeTables[engineName]
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
		lakeRef := lakeTableRef{engineName: engineName, schema: schema, table: table, physicalPath: physicalPath}
		result = replaceLakeTableRef(result, lakeRef)
	}

	// 处理两段式引用 engine.table（湖表无 schema 时）
	for engineName, tables := range engineLakeTables {
		for tableName, physicalPath := range tables {
			if strings.Contains(tableName, ".") {
				continue // 跳过 schema.table 形式的键
			}
			twoPartRef := engineName + "." + tableName
			if strings.Contains(result, twoPartRef) {
				parquetExpr := buildReadParquetExpr(physicalPath)
				result = strings.ReplaceAll(result, twoPartRef, parquetExpr)
			}
		}
	}

	return result, nil
}
