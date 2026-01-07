package mongodb

import (
	"context"
	"fmt"
	"time"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
	"go.mongodb.org/mongo-driver/mongo/readpref"
)

// MongoDBPlugin MongoDB 数据库插件
type MongoDBPlugin struct{}

// init 函数在包被导入时自动注册插件
func init() {
	plugin.Register(&MongoDBPlugin{})
}

// Type 返回数据库类型标识
func (p *MongoDBPlugin) Type() string {
	return "mongodb"
}

// DisplayName 返回显示名称
func (p *MongoDBPlugin) DisplayName() string {
	return "MongoDB"
}

// EngineCategory 返回引擎分类
func (p *MongoDBPlugin) EngineCategory() string {
	return "standard"
}

// DefaultPort 返回默认端口
func (p *MongoDBPlugin) DefaultPort() int {
	return 27017
}

// RequiredFields 返回必填字段列表
func (p *MongoDBPlugin) RequiredFields() []string {
	return []string{"host", "database"}
}

// SensitiveFields 返回敏感字段列表
func (p *MongoDBPlugin) SensitiveFields() []string {
	return []string{"password"}
}

// GenerateCapabilities 生成资源能力描述
func (p *MongoDBPlugin) GenerateCapabilities() string {
	return `{"storage":[{"type":"nosql_db","engine":"mongodb","supports_query":true}],"compute":[{"dev_modes":["query"],"description":"文档型数据库查询（MQL）","features":["flexible_schema","json_native"]}]}`
}

// ValidateConnectionInfo 验证连接信息
func (p *MongoDBPlugin) ValidateConnectionInfo(connInfo plugin.ConnectionInfo) error {
	return plugin.ValidateRequiredFields(connInfo, p.RequiredFields())
}

// BuildConnectionString 构建连接字符串
func (p *MongoDBPlugin) BuildConnectionString(connInfo plugin.ConnectionInfo) (string, error) {
	host := plugin.NormalizeHost(plugin.GetString(connInfo, "host"))
	port := plugin.GetInt(connInfo, "port")
	if port == 0 {
		port = p.DefaultPort()
	}

	// 兼容两种字段名：username 和 user
	user := plugin.GetString(connInfo, "user")
	if user == "" {
		user = plugin.GetString(connInfo, "username")
	}

	password := plugin.GetString(connInfo, "password")
	database := plugin.GetString(connInfo, "database")

	if host == "" {
		return "", fmt.Errorf("missing required MongoDB connection info: host")
	}

	// MongoDB DSN 格式：mongodb://[username:password@]host:port/database
	return plugin.MongoDBStyleDSN(user, password, host, port, database), nil
}

// TestConnection 测试数据库连接
func (p *MongoDBPlugin) TestConnection(ctx context.Context, connInfo plugin.ConnectionInfo) error {
	// 构建 DSN
	connStr, err := p.BuildConnectionString(connInfo)
	if err != nil {
		return fmt.Errorf("failed to build connection string: %w", err)
	}

	// 设置连接选项
	clientOptions := options.Client().ApplyURI(connStr)
	clientOptions.SetConnectTimeout(10 * time.Second)
	clientOptions.SetServerSelectionTimeout(10 * time.Second)

	// 连接数据库
	client, err := mongo.Connect(ctx, clientOptions)
	if err != nil {
		return fmt.Errorf("failed to connect to MongoDB: %w", err)
	}
	defer func() {
		if err := client.Disconnect(ctx); err != nil {
			// 忽略断开连接错误
		}
	}()

	// 测试连接（使用 Ping）
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := client.Ping(testCtx, readpref.Primary()); err != nil {
		return fmt.Errorf("failed to ping MongoDB: %w", err)
	}

	return nil
}

// SupportsTransactions 实现 SQLDatabasePlugin 接口
// MongoDB 4.0+ 支持多文档事务
func (p *MongoDBPlugin) SupportsTransactions() bool {
	return true
}

// DefaultDialect 实现 SQLDatabasePlugin 接口
func (p *MongoDBPlugin) DefaultDialect() string {
	return "mongodb"
}

// SupportsMetadataQuery 实现 StoragePlugin 接口
// MongoDB 支持元数据扫描
func (p *MongoDBPlugin) SupportsMetadataQuery() bool {
	return true
}
