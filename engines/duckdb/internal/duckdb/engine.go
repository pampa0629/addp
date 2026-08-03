package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	commonModels "github.com/addp/common/models"
	_ "github.com/duckdb/duckdb-go/v2"
)

type MountKind string

const (
	MountKindUnsupported MountKind = ""
	MountKindObject      MountKind = "object"
	MountKindPostgres    MountKind = "postgres"
	MountKindMySQL       MountKind = "mysql"
)

// OpenDB 打开 DuckDB 内存连接
func OpenDB() (*sql.DB, error) {
	db, err := sql.Open("duckdb", "")
	if err != nil {
		return nil, fmt.Errorf("初始化 DuckDB 失败: %w", err)
	}
	return db, nil
}

// MountEngines 将多个引擎挂载到 DuckDB 连接
func MountEngines(ctx context.Context, conn *sql.Conn, engines []commonModels.Engine) error {
	var errs []string
	for _, engine := range engines {
		switch MountKindForEngine(engine.EngineType) {
		case MountKindObject:
			if err := MountObjectStorage(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(%s) 失败: %v", engine.Name, engine.EngineType, err))
			}
		case MountKindPostgres:
			if err := MountPostgres(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(postgresql) 失败: %v", engine.Name, err))
			}
		case MountKindMySQL:
			if err := MountMySQL(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(mysql) 失败: %v", engine.Name, err))
			}
		}
	}
	if len(errs) > 0 {
		return fmt.Errorf("部分引擎挂载失败: %s", strings.Join(errs, "; "))
	}
	return nil
}

func MountKindForEngine(engineType string) MountKind {
	switch strings.ToLower(strings.TrimSpace(engineType)) {
	case "minio", "s3":
		return MountKindObject
	case "postgresql":
		return MountKindPostgres
	case "mysql":
		return MountKindMySQL
	default:
		return MountKindUnsupported
	}
}

func SupportsMount(engineType string) bool {
	return MountKindForEngine(engineType) != MountKindUnsupported
}

func IsObjectTableEngine(engineType string) bool {
	return MountKindForEngine(engineType) == MountKindObject
}

func IsRelationalMountEngine(engineType string) bool {
	kind := MountKindForEngine(engineType)
	return kind == MountKindPostgres || kind == MountKindMySQL
}

// MountObjectStorage 挂载 MinIO/S3 到 DuckDB httpfs
func MountObjectStorage(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	endpoint := getString(connInfo, "endpoint")
	accessKey := getString(connInfo, "access_key")
	secretKey := getString(connInfo, "secret_key")
	region := getString(connInfo, "region")
	if region == "" {
		region = "us-east-1"
	}
	if endpoint == "" || accessKey == "" || secretKey == "" {
		return fmt.Errorf("对象存储连接缺少 endpoint、access_key 或 secret_key")
	}

	if err := ensureDuckDBExtension(ctx, conn, "httpfs", "httpfs"); err != nil {
		return err
	}
	secretName := fmt.Sprintf("addp_s3_%d", engine.ID)
	stmt := fmt.Sprintf(
		"CREATE TEMPORARY SECRET %s (TYPE s3, KEY_ID %s, SECRET %s, REGION %s, ENDPOINT %s, URL_STYLE 'path', USE_SSL false)",
		secretName, duckDBString(accessKey), duckDBString(secretKey), duckDBString(region), duckDBString(endpoint),
	)
	if _, err := conn.ExecContext(ctx, stmt); err != nil {
		return fmt.Errorf("配置对象存储临时访问凭据失败: %w", err)
	}
	return nil
}

// MountPostgres 挂载 PostgreSQL 到 DuckDB
func MountPostgres(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	host := getString(connInfo, "host")
	port := getInt(connInfo, "port", 5432)
	user := getString(connInfo, "username")
	if user == "" {
		user = getString(connInfo, "user")
	}
	password := getString(connInfo, "password")
	database := getString(connInfo, "database")

	dsn := fmt.Sprintf("host=%s port=%d user=%s password=%s dbname=%s",
		host, port, user, password, database)

	if err := ensureDuckDBExtension(ctx, conn, "postgres", "postgres_scanner"); err != nil {
		return err
	}
	stmts := []string{
		fmt.Sprintf("ATTACH '%s' AS %s (TYPE postgres, READ_ONLY)", dsn, SanitizeName(engine.Name)),
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") &&
				!strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("postgres attach failed: %w", err)
			}
		}
	}
	return nil
}

// MountMySQL 挂载 MySQL 到 DuckDB
func MountMySQL(ctx context.Context, conn *sql.Conn, engine commonModels.Engine) error {
	connInfo := engine.ConnectionInfo
	host := getString(connInfo, "host")
	port := getInt(connInfo, "port", 3306)
	user := getString(connInfo, "username")
	if user == "" {
		user = getString(connInfo, "user")
	}
	password := getString(connInfo, "password")
	database := getString(connInfo, "database")

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s", user, password, host, port, database)

	if err := ensureDuckDBExtension(ctx, conn, "mysql", "mysql_scanner"); err != nil {
		return err
	}
	stmts := []string{
		fmt.Sprintf("ATTACH '%s' AS %s (TYPE mysql, READ_ONLY)", dsn, SanitizeName(engine.Name)),
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") &&
				!strings.Contains(err.Error(), "already exists") {
				return fmt.Errorf("mysql attach failed: %w", err)
			}
		}
	}
	return nil
}

// SanitizeName 将引擎名称转换为合法的 DuckDB 标识符
func SanitizeName(name string) string {
	var b strings.Builder
	for _, r := range name {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	result := b.String()
	if len(result) > 0 && result[0] >= '0' && result[0] <= '9' {
		result = "_" + result
	}
	if result == "" {
		result = "engine"
	}
	return result
}

func getString(m map[string]interface{}, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
}

func getInt(m map[string]interface{}, key string, defaultVal int) int {
	if m == nil {
		return defaultVal
	}
	switch v := m[key].(type) {
	case float64:
		return int(v)
	case int:
		return v
	case int64:
		return int(v)
	}
	return defaultVal
}
