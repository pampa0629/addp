package duckdb

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	commonModels "github.com/addp/common/models"
	_ "github.com/marcboeker/go-duckdb"
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
		switch engine.EngineType {
		case "minio", "s3":
			if err := MountObjectStorage(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(%s) 失败: %v", engine.Name, engine.EngineType, err))
			}
		case "postgresql":
			if err := MountPostgres(ctx, conn, engine); err != nil {
				errs = append(errs, fmt.Sprintf("挂载 %s(postgresql) 失败: %v", engine.Name, err))
			}
		case "mysql":
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

	stmts := []string{
		"INSTALL httpfs",
		"LOAD httpfs",
		fmt.Sprintf("SET s3_endpoint='%s'", endpoint),
		fmt.Sprintf("SET s3_access_key_id='%s'", accessKey),
		fmt.Sprintf("SET s3_secret_access_key='%s'", secretKey),
		fmt.Sprintf("SET s3_region='%s'", region),
		"SET s3_use_ssl=false",
		"SET s3_url_style='path'",
	}

	for _, stmt := range stmts {
		if _, err := conn.ExecContext(ctx, stmt); err != nil {
			if !strings.Contains(err.Error(), "already installed") &&
				!strings.Contains(err.Error(), "already loaded") {
				// 忽略警告，继续执行
			}
		}
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

	stmts := []string{
		"INSTALL postgres",
		"LOAD postgres",
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

	stmts := []string{
		"INSTALL mysql",
		"LOAD mysql",
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
