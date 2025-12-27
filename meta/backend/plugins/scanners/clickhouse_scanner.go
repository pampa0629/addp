package scanners

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// ClickHouseScanner 执行 ClickHouse 元数据扫描
// 通过 dbbridge 委托给 ClickHouse 插件的元数据查询能力
type ClickHouseScanner struct {
	resource *commonModels.Resource // 资源信息
	db       *gorm.DB               // 从插件获取的连接池
}

// NewClickHouseScanner 根据资源信息创建 ClickHouse 扫描器
func NewClickHouseScanner(resource *commonModels.Resource) (*ClickHouseScanner, error) {
	// 从插件系统获取连接池
	db, err := dbbridge.GetOrCreatePool(resource, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	return &ClickHouseScanner{
		resource: resource,
		db:       db,
	}, nil
}

// ListSchemas 列出所有数据库
// 委托给插件系统的 ListSchemas 方法
func (s *ClickHouseScanner) ListSchemas() ([]format.SchemaInfo, error) {
	// 调用插件系统的元数据查询
	pluginSchemas, err := dbbridge.ListSchemas(context.Background(), s.resource, s.db)
	if err != nil {
		return nil, fmt.Errorf("failed to list schemas: %w", err)
	}

	// 转换为 format.SchemaInfo 格式
	var schemas []format.SchemaInfo
	for _, ps := range pluginSchemas {
		schemas = append(schemas, format.SchemaInfo{
			Name:           ps.Name,
			TableCount:     ps.TableCount,
			TotalSizeBytes: 0, // ClickHouse plugin 不提供此字段，设为 0
		})
	}

	return schemas, nil
}

// ScanTables 扫描指定数据库的表
// 委托给插件系统的 ListTables 方法
func (s *ClickHouseScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
	// 调用插件系统的元数据查询
	pluginTables, err := dbbridge.ListTables(context.Background(), s.resource, s.db, schemaName)
	if err != nil {
		return nil, fmt.Errorf("failed to scan tables: %w", err)
	}

	// 转换为 format.TableInfo 格式
	var tables []format.TableInfo
	for _, pt := range pluginTables {
		tables = append(tables, format.TableInfo{
			Name:      pt.TableName,
			Type:      "BASE TABLE", // ClickHouse 插件不区分类型，默认为 BASE TABLE
			Comment:   "",           // ClickHouse 插件暂不提供表注释
			RowCount:  pt.RowCount,
			SizeBytes: pt.SizeBytes,
		})
	}

	return tables, nil
}

// ScanFields 扫描指定表的字段
// 委托给插件系统的 ListColumns 方法
func (s *ClickHouseScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
	// 调用插件系统的元数据查询
	pluginColumns, err := dbbridge.ListColumns(context.Background(), s.resource, s.db, schemaName, tableName)
	if err != nil {
		return nil, fmt.Errorf("failed to scan fields: %w", err)
	}

	// 转换为 format.FieldInfo 格式
	var fields []format.FieldInfo
	for i, pc := range pluginColumns {
		fields = append(fields, format.FieldInfo{
			Name:             pc.ColumnName,
			OrdinalPosition:  i + 1,
			DataType:         pc.DataType,
			ColumnType:       pc.DataType,
			IsNullable:       pc.IsNullable,
			DefaultValue:     "",     // ClickHouse 插件暂不提供默认值
			Comment:          pc.Comment,
			IsPrimaryKey:     pc.IsPrimaryKey,
			IsUniqueKey:      false,  // ClickHouse 插件暂不提供唯一键信息
			CharacterSet:     "",     // ClickHouse 不使用字符集概念
			Collation:        "",     // ClickHouse 不使用排序规则
			NumericPrecision: 0,      // ClickHouse 插件暂不提供精度
			NumericScale:     0,      // ClickHouse 插件暂不提供标度
		})
	}

	return fields, nil
}

// GetDB 返回底层的数据库连接
func (s *ClickHouseScanner) GetDB() *sql.DB {
	sqlDB, err := s.db.DB()
	if err != nil {
		return nil
	}
	return sqlDB
}

// Close 关闭数据库连接
// 连接池由 PoolManager 管理，Scanner 不负责关闭
func (s *ClickHouseScanner) Close() error {
	// 连接池由 PoolManager 管理，Scanner 不负责关闭
	return nil
}
