package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	commonModels "github.com/addp/common/models"
)

// FederatedSession 持有已就绪的 DuckDB 连接和改写后的 SQL
// 调用方负责 defer session.Close()
type FederatedSession struct {
	Conn         *sql.Conn
	RewrittenSQL string
	db           *sql.DB
}

type FederatedSessionOptions struct {
	MemoryLimit string
	Threads     int
	LoadSpatial bool
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

// PrepareFederatedQueryWithEngines prepares a query from engine connections
// already resolved through the caller's execution authorization.
func PrepareFederatedQueryWithEngines(
	ctx context.Context,
	sqlStr string,
	engines []commonModels.Engine,
	engineObjectTables map[string]map[string]string,
	options FederatedSessionOptions,
) (*FederatedSession, error) {
	return prepareFederatedSession(ctx, sqlStr, engines, engineObjectTables, options)
}

func prepareFederatedSession(
	ctx context.Context,
	sqlStr string,
	engines []commonModels.Engine,
	engineObjectTables map[string]map[string]string,
	options FederatedSessionOptions,
) (*FederatedSession, error) {
	// 1. 打开 DuckDB 内存连接
	db, err := OpenDB()
	if err != nil {
		return nil, err
	}

	conn, err := db.Conn(ctx)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("获取 DuckDB 连接失败: %w", err)
	}
	if err := configureFederatedSession(ctx, conn, options); err != nil {
		conn.Close()
		db.Close()
		return nil, err
	}

	// 2. 挂载引擎。
	if len(engines) > 0 {
		if err := MountEngines(ctx, conn, engines); err != nil {
			conn.Close()
			db.Close()
			return nil, err
		}
	}

	// 3. 使用调用方提供的已冻结对象表映射改写 SQL。
	rewriter := NewSQLRewriter(nil, 0)
	rewrittenSQL, err := rewriter.RewriteWithEngines(ctx, sqlStr, engineObjectTables)
	if err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("SQL 改写失败: %w", err)
	}
	if err := lockFederatedExternalAccess(ctx, conn, engineObjectTables); err != nil {
		conn.Close()
		db.Close()
		return nil, fmt.Errorf("限制 DuckDB 外部访问失败: %w", err)
	}

	return &FederatedSession{
		Conn:         conn,
		RewrittenSQL: rewrittenSQL,
		db:           db,
	}, nil
}

func configureFederatedSession(ctx context.Context, conn *sql.Conn, options FederatedSessionOptions) error {
	memoryLimit := strings.TrimSpace(options.MemoryLimit)
	if memoryLimit == "" || options.Threads <= 0 {
		return fmt.Errorf("DuckDB 会话资源限制无效")
	}
	if _, err := conn.ExecContext(ctx, "SET memory_limit = '"+strings.ReplaceAll(memoryLimit, "'", "''")+"'"); err != nil {
		return fmt.Errorf("设置 DuckDB 内存限制失败: %w", err)
	}
	if _, err := conn.ExecContext(ctx, fmt.Sprintf("SET threads = %d", options.Threads)); err != nil {
		return fmt.Errorf("设置 DuckDB 线程数失败: %w", err)
	}
	if options.LoadSpatial {
		if err := ensureDuckDBExtension(ctx, conn, "spatial", "spatial"); err != nil {
			return err
		}
	}
	return nil
}

func lockFederatedExternalAccess(
	ctx context.Context,
	conn *sql.Conn,
	engineObjectTables map[string]map[string]string,
) error {
	allowedDirectories, allowedPaths := allowedObjectTableLocations(engineObjectTables)
	return lockExternalAccessWithAllowedLocations(ctx, conn, allowedDirectories, allowedPaths)
}

func lockExternalAccessWithAllowedLocations(
	ctx context.Context,
	conn *sql.Conn,
	allowedDirectories []string,
	allowedPaths []string,
) error {
	statements := make([]string, 0, 6)
	if len(allowedDirectories) > 0 {
		statements = append(statements, "SET allowed_directories = "+duckDBStringList(allowedDirectories))
	}
	if len(allowedPaths) > 0 {
		statements = append(statements, "SET allowed_paths = "+duckDBStringList(allowedPaths))
	}
	statements = append(statements,
		"SET autoinstall_known_extensions = false",
		"SET autoload_known_extensions = false",
		"SET enable_external_access = false",
		"SET lock_configuration = true",
	)
	for _, statement := range statements {
		if _, err := conn.ExecContext(ctx, statement); err != nil {
			return err
		}
	}
	return nil
}

func allowedObjectTableLocations(engineObjectTables map[string]map[string]string) ([]string, []string) {
	directorySet := make(map[string]struct{})
	pathSet := make(map[string]struct{})
	seen := make(map[string]struct{})
	for _, tables := range engineObjectTables {
		for _, physicalPath := range tables {
			path := strings.TrimSpace(physicalPath)
			if path == "" {
				continue
			}
			if !strings.HasPrefix(path, "s3://") && !strings.HasPrefix(path, "http://") && !strings.HasPrefix(path, "https://") {
				path = "s3://" + path
			}
			if _, exists := seen[path]; exists {
				continue
			}
			seen[path] = struct{}{}
			if strings.EqualFold(filepath.Ext(path), ".parquet") {
				pathSet[path] = struct{}{}
			} else {
				directorySet[strings.TrimRight(path, "/")] = struct{}{}
			}
		}
	}
	directories := make([]string, 0, len(directorySet))
	for directory := range directorySet {
		directories = append(directories, directory)
	}
	paths := make([]string, 0, len(pathSet))
	for path := range pathSet {
		paths = append(paths, path)
	}
	sort.Strings(directories)
	sort.Strings(paths)
	return directories, paths
}

func duckDBStringList(values []string) string {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "'"+strings.ReplaceAll(value, "'", "''")+"'")
	}
	return "[" + strings.Join(quoted, ", ") + "]"
}
