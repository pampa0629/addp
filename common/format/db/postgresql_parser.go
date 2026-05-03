package db

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"gorm.io/gorm"
)

// PostgreSQLTableParser PostgreSQL 表解析器
// 实现 format.DBTableParser 接口
type PostgreSQLTableParser struct {
	typeMapper format.TypeMapper
}

type postgreSQLMetadataHelper interface {
	plugin.CatalogModelProvider
	DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error)
	Type() string
}

// NewPostgreSQLTableParser 创建 PostgreSQL 表解析器
func NewPostgreSQLTableParser() *PostgreSQLTableParser {
	return &PostgreSQLTableParser{
		typeMapper: format.GetTypeMapper("postgresql"),
	}
}

// ParseTableInfo 从 PostgreSQL 表中提取 TableInfo
func (p *PostgreSQLTableParser) ParseTableInfo(ctx context.Context, db *gorm.DB, enginePlugin interface{}, schema, table string) (*format.TableInfo, error) {
	metadataHelper, ok := enginePlugin.(postgreSQLMetadataHelper)
	if !ok {
		return nil, fmt.Errorf("enginePlugin must provide PostgreSQL metadata helpers, got %T", enginePlugin)
	}

	item, err := metadataHelper.DescribeItem(ctx, plugin.ConnectionInfo{}, dbParserCatalogPath(metadataHelper, schema, table), plugin.MetadataOptions{
		IncludeStatistics: true,
		IncludeIndexes:    true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to describe table: %w", err)
	}

	// 2. 转换为 FieldInfo
	fields := make([]format.FieldInfo, len(item.Fields))
	var primaryKeys []string

	for i, field := range item.Fields {
		// 使用 TypeMapper 转换类型
		dataType := field.NativeType
		if dataType == "" {
			dataType = field.Type
		}
		fieldType := p.typeMapper.ToCommon(dataType)

		fields[i] = format.FieldInfo{
			Name:         field.Name,
			Type:         fieldType,
			OriginalType: dataType,
			Nullable:     field.Nullable,
			IsPrimaryKey: field.PrimaryKey,
			Comment:      field.Comment,
		}

		// 收集主键
		if field.PrimaryKey {
			primaryKeys = append(primaryKeys, field.Name)
		}
	}

	// 3. 检查是否为空间表（PostGIS）
	var extensions []format.ExtensionInfo
	spatialInfo := p.detectSpatialInfo(ctx, db, schema, table, fields)
	if spatialInfo != nil {
		extensions = append(extensions, spatialInfo)
	}

	// 4. 获取表统计信息
	rowCount := int64(0)
	if value, ok := dbParserInt64Stat(item.Stats, "row_count"); ok {
		rowCount = value
	}

	// 5. 构建 TableInfo
	tableInfo := &format.TableInfo{
		Name:       table,
		RowCount:   &rowCount,
		Fields:     fields,
		PrimaryKey: primaryKeys,
		Extensions: extensions,
	}

	return tableInfo, nil
}

// SupportedEngineTypes 返回支持的引擎类型
func (p *PostgreSQLTableParser) SupportedEngineTypes() []string {
	return []string{"postgresql"}
}

// detectSpatialInfo 检测空间信息（PostGIS）
func (p *PostgreSQLTableParser) detectSpatialInfo(ctx context.Context, db *gorm.DB, schema, table string, fields []format.FieldInfo) *format.SpatialInfo {
	// 查找几何字段
	var geometryField *format.FieldInfo
	for i := range fields {
		if fields[i].Type == format.FieldTypeGeometry {
			geometryField = &fields[i]
			break
		}
	}

	// 如果没有几何字段，返回 nil
	if geometryField == nil {
		return nil
	}

	// 查询 PostGIS geometry_columns 视图获取详细信息
	type GeometryColumn struct {
		GeometryType string
		SRID         int
		CoordDim     int
	}

	var geomCol GeometryColumn
	query := `
		SELECT
			type as geometry_type,
			srid,
			coord_dimension as coord_dim
		FROM geometry_columns
		WHERE f_table_schema = ? AND f_table_name = ? AND f_geometry_column = ?
	`

	err := db.WithContext(ctx).Raw(query, schema, table, geometryField.Name).Scan(&geomCol).Error
	if err != nil {
		// 如果查询失败（可能不是 PostGIS 表），使用默认值
		return &format.SpatialInfo{
			GeometryColumn: geometryField.Name,
			GeometryType:   "Geometry", // 默认类型
			SRID:           4326,       // WGS84 默认
			Dimension:      2,          // 2D 默认
		}
	}

	// 返回从 PostGIS 获取的详细信息
	return &format.SpatialInfo{
		GeometryColumn: geometryField.Name,
		GeometryType:   geomCol.GeometryType,
		SRID:           geomCol.SRID,
		Dimension:      geomCol.CoordDim,
	}
}
