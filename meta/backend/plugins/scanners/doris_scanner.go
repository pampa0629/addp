package scanners

import (
	"context"
	"fmt"

	"github.com/addp/common/dbbridge"
	"github.com/addp/common/format"
	commonModels "github.com/addp/common/models"
	"gorm.io/gorm"
)

// DorisScanner 执行 Doris 元数据扫描
// 重构后：作为插件系统的薄适配层，委托给插件的元数据查询能力
type DorisScanner struct {
	resource *commonModels.Resource // 资源信息
	db       *gorm.DB               // 从插件获取的连接池
}

// NewDorisScanner 根据资源信息创建 Doris 扫描器
// 重构后：不再接受连接字符串，而是接受Resource对象
func NewDorisScanner(resource *commonModels.Resource) (*DorisScanner, error) {
	// 从插件系统获取连接池
	db, err := dbbridge.GetOrCreatePool(resource, dbbridge.DefaultPoolConfig())
	if err != nil {
		return nil, fmt.Errorf("failed to get connection pool: %w", err)
	}

	return &DorisScanner{
		resource: resource,
		db:       db,
	}, nil
}

// ListSchemas 列出可用数据库
// 重构后：委托给插件系统的 ListSchemas 方法
func (s *DorisScanner) ListSchemas() ([]format.SchemaInfo, error) {
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
			TotalSizeBytes: 0, // plugin 层不提供此字段，设为 0
		})
	}

	return schemas, nil
}

// ScanTables 扫描指定数据库的表
// 重构后：委托给插件系统的 ListTables 方法
func (s *DorisScanner) ScanTables(schemaName string) ([]format.TableInfo, error) {
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
			Type:      "BASE TABLE", // 插件层不区分类型，默认为 BASE TABLE
			Comment:   "",           // 插件层暂不提供表注释
			RowCount:  pt.RowCount,
			SizeBytes: pt.SizeBytes,
		})
	}

	return tables, nil
}

// ScanFields 扫描指定表的字段
// 重构后：委托给插件系统的 ListColumns 方法
func (s *DorisScanner) ScanFields(schemaName, tableName string) ([]format.FieldInfo, error) {
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
			OrdinalPosition:  i + 1, // 插件层暂不提供序号，使用索引+1
			DataType:         pc.DataType,
			ColumnType:       pc.DataType, // 使用原始类型
			IsNullable:       pc.IsNullable,
			DefaultValue:     "",     // 插件层暂不提供默认值
			Comment:          pc.Comment,
			IsPrimaryKey:     pc.IsPrimaryKey,
			IsUniqueKey:      false,  // 插件层暂不提供唯一键信息
			CharacterSet:     "",     // 插件层暂不提供字符集
			Collation:        "",     // 插件层暂不提供排序规则
			NumericPrecision: 0,      // 插件层暂不提供精度
			NumericScale:     0,      // 插件层暂不提供标度
		})
	}

	return fields, nil
}

// Close 关闭数据库连接
// 重构后：不需要关闭，由插件系统的 PoolManager 统一管理
func (s *DorisScanner) Close() error {
	// 连接池由 PoolManager 管理，Scanner 不负责关闭
	return nil
}
