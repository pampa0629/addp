package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"

	commonClient "github.com/addp/common/client"
	commonModels "github.com/addp/common/models"
)

// SystemClientIface 最小接口，避免循环依赖
type SystemClientIface interface {
	ListEngines(engineType string, tenantID uint) ([]commonModels.Engine, error)
}

// FederatedSession 持有已就绪的 DuckDB 连接和改写后的 SQL
// 调用方负责 defer session.Close()
type FederatedSession struct {
	Conn         *sql.Conn
	RewrittenSQL string
	db           *sql.DB
}

// Close 释放连接和数据库资源
func (s *FederatedSession) Close() {
	if s.Conn != nil {
		s.Conn.Close()
	}
	if s.db != nil {
		s.db.Close()
	}
}

// PrepareFederatedQuery 准备联邦查询：打开 DuckDB、按需挂载引擎、改写 SQL
// 内部流程：提取引擎名 → 拉取并过滤引擎 → 打开 DuckDB → 挂载引擎 → 构建湖表映射 → 改写 SQL
func PrepareFederatedQuery(
	ctx context.Context,
	tenantID uint,
	sqlStr string,
	systemClient SystemClientIface,
	metaClient *commonClient.MetaClient,
) (*FederatedSession, error) {
	// 1. 解析 SQL 中引用的引擎名，按需拉取引擎列表
	referencedNames := ExtractReferencedEngineNames(sqlStr)
	var engines []commonModels.Engine
	if len(referencedNames) > 0 {
		var err error
		engines, err = systemClient.ListEngines("", tenantID)
		if err != nil {
			return nil, fmt.Errorf("获取引擎列表失败: %w", err)
		}
		engines = FilterEnginesByName(engines, referencedNames)
	}

	// 2. 打开 DuckDB 内存连接
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("获取 DuckDB 连接失败: %w", err)
	}

	// 3. 挂载引擎 + 构建湖表映射
	var engineLakeTables map[string]map[string]string
	if len(engines) > 0 {
		engineLakeTables = BuildLakeTableMap(ctx, tenantID, engines, metaClient)
		if err := MountEngines(ctx, conn, engines); err != nil {
			// 挂载失败只记录警告，不中断（部分引擎可能不可用）
			slog.Warn("部分引擎挂载失败", "error", err)
		}
	}

	// 4. 改写 SQL（湖表三段式引用 → read_parquet）
	rewriter := NewSQLRewriter(metaClient, tenantID)
	rewrittenSQL, err := rewriter.RewriteWithEngines(ctx, sqlStr, engineLakeTables)
	if err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("SQL 改写失败: %w", err)
	}

	return &FederatedSession{
		Conn:         conn,
		RewrittenSQL: rewrittenSQL,
		db:           db,
	}, nil
}
