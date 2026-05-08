package plugin

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"gorm.io/gorm"
)

// DriverConnInfo 是构造 driver DSN 时需要的规范化连接参数。
type DriverConnInfo struct {
	Host     string
	Port     int
	User     string
	Password string
	Database string
}

// ParseDriverConnInfo 从 connection_info 解析 SQL/driver 连接参数。
// user 与 username 兼容读取；connection_info 仍是系统层唯一事实源。
func ParseDriverConnInfo(connInfo ConnectionInfo, defaultPort int, defaultDatabase string) DriverConnInfo {
	port := GetInt(connInfo, "port")
	if port == 0 {
		port = defaultPort
	}

	user := GetString(connInfo, "user")
	if user == "" {
		user = GetString(connInfo, "username")
	}

	database := GetString(connInfo, "database")
	if database == "" {
		database = defaultDatabase
	}

	return DriverConnInfo{
		Host:     NormalizeHost(GetString(connInfo, "host")),
		Port:     port,
		User:     user,
		Password: GetString(connInfo, "password"),
		Database: database,
	}
}

// Require 校验构造 DSN 必需的连接参数。
func (info DriverConnInfo) Require(engineLabel string, fields ...string) error {
	for _, field := range fields {
		switch field {
		case "host":
			if info.Host == "" {
				return fmt.Errorf("missing required %s connection info: host", engineLabel)
			}
		case "user":
			if info.User == "" {
				return fmt.Errorf("missing required %s connection info: user", engineLabel)
			}
		case "database":
			if info.Database == "" {
				return fmt.Errorf("missing required %s connection info: database", engineLabel)
			}
		default:
			return fmt.Errorf("unknown required connection field: %s", field)
		}
	}
	return nil
}

func BuildMySQLCompatibleDSN(connInfo ConnectionInfo, defaultPort int, engineLabel string, params map[string]string) (string, error) {
	parts := ParseDriverConnInfo(connInfo, defaultPort, "")
	if err := parts.Require(engineLabel, "host", "user"); err != nil {
		return "", err
	}
	return MySQLStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, parts.Database, params), nil
}

func BuildPostgreSQLDSN(connInfo ConnectionInfo, defaultPort int) (string, error) {
	parts := ParseDriverConnInfo(connInfo, defaultPort, "")
	if err := parts.Require("PostgreSQL", "host", "user"); err != nil {
		return "", err
	}
	sslMode := GetString(connInfo, "sslmode")
	if sslMode == "" {
		sslMode = "disable"
	}
	return PostgreSQLStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, parts.Database, sslMode), nil
}

func BuildClickHouseDSN(connInfo ConnectionInfo, defaultPort int, params map[string]string) (string, error) {
	parts := ParseDriverConnInfo(connInfo, defaultPort, "")
	if err := parts.Require("ClickHouse", "host", "user"); err != nil {
		return "", err
	}
	return ClickHouseStyleDSN(parts.User, parts.Password, parts.Host, parts.Port, parts.Database, params), nil
}

// TestSQLConnection 执行只读 SQL 语句验证连接和认证是否可用。
func TestSQLConnection(ctx context.Context, driverName, dsn, probeQuery string) error {
	db, err := sql.Open(driverName, dsn)
	if err != nil {
		return fmt.Errorf("failed to open connection: %w", err)
	}
	defer db.Close()

	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(10 * time.Second)

	testCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	if err := db.PingContext(testCtx); err != nil {
		return fmt.Errorf("failed to ping database: %w", err)
	}

	rows, err := db.QueryContext(testCtx, probeQuery)
	if err != nil {
		return fmt.Errorf("failed to execute test query: %w", err)
	}
	defer rows.Close()

	return nil
}

// OpenGORMPool 创建 GORM 连接池并统一应用连接池参数。
func OpenGORMPool(dialector gorm.Dialector, poolConfig *PoolConfig) (*gorm.DB, error) {
	if poolConfig == nil {
		poolConfig = DefaultPoolConfig()
	}

	db, err := gorm.Open(dialector, &gorm.Config{
		DisableAutomaticPing: false,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gorm connection: %w", err)
	}

	sqlDB, err := db.DB()
	if err != nil {
		return nil, fmt.Errorf("failed to get underlying sql.DB: %w", err)
	}

	sqlDB.SetMaxOpenConns(poolConfig.MaxOpenConns)
	sqlDB.SetMaxIdleConns(poolConfig.MaxIdleConns)
	sqlDB.SetConnMaxLifetime(poolConfig.ConnMaxLifetime)

	return db, nil
}
