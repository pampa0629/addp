package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"os"
)

// ConnectionInfo 定义连接信息类型，支持 GORM JSONB 序列化
type ConnectionInfo map[string]interface{}

// Value 实现 driver.Valuer 接口，用于 GORM 写入数据库
func (c ConnectionInfo) Value() (driver.Value, error) {
	return json.Marshal(c)
}

// Scan 实现 sql.Scanner 接口，用于 GORM 从数据库读取
func (c *ConnectionInfo) Scan(value interface{}) error {
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, c)
}

// ScanConfig 元数据扫描配置
type ScanConfig struct {
	Enabled        bool     `json:"enabled"`                   // 是否启用扫描（总开关，兼容旧版）
	ImmediateScan  bool     `json:"immediate_scan"`            // 注册后立即扫描
	ImmediateDepth string   `json:"immediate_depth,omitempty"` // 立即扫描深度：basic（基础）或 deep（深度）
	ScheduledScan  bool     `json:"scheduled_scan"`            // 启用定时扫描（深度固定为 deep）
	ScheduleType   string   `json:"schedule_type"`             // daily, weekly, monthly, cron（仅当scheduled_scan=true时有效）
	CronExpression string   `json:"cron_expression,omitempty"` // Cron 表达式（schedule_type=cron 时使用）
	ScheduleTime   string   `json:"schedule_time,omitempty"`   // 执行时间 HH:mm（daily/weekly/monthly 时使用）
	ScheduleValue  []int    `json:"schedule_value,omitempty"`  // 周几（0-6）或月几（1-31）
	ScanDepth      string   `json:"scan_depth"`                // 兼容旧版字段：basic（或已废弃的 shallow）, deep
	SchemaNames    []string `json:"schema_names,omitempty"`    // 已废弃：PostgreSQL schemas（系统自动过滤）
	ObjectPaths    []string `json:"object_paths,omitempty"`    // 已废弃：MinIO prefixes（系统自动过滤）
}

// Value 实现 driver.Valuer 接口，用于 GORM 写入数据库
func (s ScanConfig) Value() (driver.Value, error) {
	return json.Marshal(s)
}

// Scan 实现 sql.Scanner 接口，用于 GORM 从数据库读取
func (s *ScanConfig) Scan(value interface{}) error {
	if value == nil {
		return nil
	}
	bytes, ok := value.([]byte)
	if !ok {
		return nil
	}
	return json.Unmarshal(bytes, s)
}

// Resource 资源信息（对应 system.resources 表）
type Resource struct {
	ID             uint           `gorm:"column:id" json:"id"`
	TenantID       uint           `gorm:"column:tenant_id" json:"tenant_id"`
	Name           string         `gorm:"column:name" json:"name"` // 数据库字段是 name（英文标识）
	DisplayName    string         `gorm:"column:display_name;not null;size:255" json:"display_name"` // 中文显示名称
	ResourceType   string         `gorm:"column:resource_type" json:"resource_type"`
	ConnectionInfo ConnectionInfo `gorm:"column:connection_info;type:json" json:"connection_info"`
	Description    string         `gorm:"column:description" json:"description"`
	ScanConfig     *ScanConfig    `gorm:"column:scan_config;type:json" json:"scan_config,omitempty"` // 元数据扫描配置（可选）
	IsActive       bool           `gorm:"column:is_active" json:"is_active"`
	CreatedBy      *uint          `gorm:"column:created_by" json:"created_by,omitempty"`

	// 能力注册字段（用于计算引擎）
	UniqueIdentifier  *string `gorm:"column:unique_identifier" json:"unique_identifier,omitempty"`
	IsBuiltin         bool    `gorm:"column:is_builtin" json:"is_builtin"`
	Capabilities      *string `gorm:"column:capabilities" json:"capabilities,omitempty"`           // JSONB 字符串
	TaskAPIConfig     *string `gorm:"column:task_api_config" json:"task_api_config,omitempty"`     // JSONB 字符串
	HealthCheckConfig *string `gorm:"column:health_check_config" json:"health_check_config,omitempty"` // JSONB 字符串

	// Status 字段不存在于 system.resources 表中，移除
}

// BuildConnectionString 根据资源信息构建连接字符串
func BuildConnectionString(resource *Resource) (string, error) {
	connInfo := resource.ConnectionInfo

	// 辅助函数:从interface{}转换为字符串
	getString := func(key string) string {
		if v, ok := connInfo[key]; ok {
			switch val := v.(type) {
			case string:
				return val
			case float64:
				// 对于端口字段，必须转为整数格式（避免 "9030.0"）
				if key == "port" {
					return fmt.Sprintf("%d", int(val))
				}
				return fmt.Sprintf("%.0f", val)
			case int:
				return fmt.Sprintf("%d", val)
			default:
				return fmt.Sprintf("%v", val)
			}
		}
		return ""
	}

	normalizeHost := func(host string) string {
		if host == "localhost" || host == "127.0.0.1" {
			if alias := os.Getenv("RESOURCE_LOCALHOST_ALIAS"); alias != "" {
				return alias
			}
		}
		return host
	}

	switch resource.ResourceType {
	case "postgresql", "PostgreSQL":
		host := normalizeHost(getString("host"))
		port := getString("port")
		// 兼容两种字段名：username 和 user
		user := getString("username")
		if user == "" {
			user = getString("user")
		}
		password := getString("password")
		dbname := getString("database")

		if host == "" || port == "" || user == "" || password == "" {
			return "", fmt.Errorf("missing required PostgreSQL connection info")
		}

		return fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
			host, port, user, password, dbname), nil

	case "mysql", "MySQL":
		host := normalizeHost(getString("host"))
		port := getString("port")
		// 兼容两种字段名：username 和 user
		user := getString("username")
		if user == "" {
			user = getString("user")
		}
		password := getString("password")
		dbname := getString("database")

		if host == "" || port == "" || user == "" || password == "" {
			return "", fmt.Errorf("missing required MySQL connection info")
		}

		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&timeout=10s",
			user, password, host, port, dbname), nil

	case "doris", "Doris":
		// Doris 兼容 MySQL 协议，完全复用 MySQL 连接逻辑
		host := normalizeHost(getString("host"))
		port := getString("port")
		// 兼容两种字段名：username 和 user
		user := getString("username")
		if user == "" {
			user = getString("user")
		}
		password := getString("password")
		dbname := getString("database")

		// Doris 默认 root 用户密码为空，所以不强制要求 password
		if host == "" || port == "" || user == "" {
			return "", fmt.Errorf("missing required Doris connection info (host, port, user)")
		}

		// 处理空密码的情况
		if password == "" {
			// 密码为空时，DSN 格式为: user@tcp(host:port)/database
			return fmt.Sprintf("%s@tcp(%s:%s)/%s?parseTime=true&timeout=10s",
				user, host, port, dbname), nil
		}

		// 密码不为空时，DSN 格式为: user:password@tcp(host:port)/database
		return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s?parseTime=true&timeout=10s",
			user, password, host, port, dbname), nil

	case "s3", "S3", "minio", "Minio", "oss", "OSS", "object_storage", "object-storage":
		bytes, err := json.Marshal(connInfo)
		if err != nil {
			return "", fmt.Errorf("failed to marshal object storage connection info: %w", err)
		}
		return string(bytes), nil
	default:
		return "", fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
	}
}
