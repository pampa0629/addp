package service

// sql_rewriter.go - 已迁移到 common/duckdb/rewriter.go
// 此文件保留以兼容现有调用，直接委托给 common/duckdb 包

import (
	"context"

	"github.com/addp/common/duckdb"
	commonClient "github.com/addp/common/client"
)

// SQLRewriter 将 SQL 中的湖表引用改写为 read_parquet() 调用
// 委托给 common/duckdb.SQLRewriter
type SQLRewriter = duckdb.SQLRewriter

// NewSQLRewriter 创建 SQL 改写器
func NewSQLRewriter(metaClient *commonClient.MetaClient, tenantID uint) *SQLRewriter {
	return duckdb.NewSQLRewriter(metaClient, tenantID)
}

// extractTableRefs 从 SQL 中提取所有三段式表引用（保留供内部使用）
func extractTableRefs(sql string) []string {
	return duckdb.ExtractTableRefs(sql)
}

// extractTwoPartTableRefs 从 SQL 中提取纯两段式引用
func extractTwoPartTableRefs(sql string) []string {
	return duckdb.ExtractTwoPartTableRefs(sql)
}

// buildReadParquetExpr 根据物理路径构建 read_parquet() 表达式
func buildReadParquetExpr(physicalPath string) string {
	return duckdb.BuildReadParquetExpr(physicalPath)
}

// RewriteWithEngines 使用已知引擎列表改写 SQL
// 保留此函数签名以兼容现有调用
func rewriteWithEngines(ctx context.Context, rewriter *SQLRewriter, sql string, engineLakeTables map[string]map[string]string) (string, error) {
	return rewriter.RewriteWithEngines(ctx, sql, engineLakeTables)
}
