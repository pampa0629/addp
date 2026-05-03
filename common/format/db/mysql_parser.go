package db

import (
	"context"
	"fmt"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"gorm.io/gorm"
)

// MySQLTableParser MySQL 表解析器
// 实现 format.DBTableParser 接口
type MySQLTableParser struct {
	typeMapper format.TypeMapper
}

type mySQLMetadataHelper interface {
	plugin.CatalogModelProvider
	DescribeItem(ctx context.Context, connInfo plugin.ConnectionInfo, path plugin.CatalogPath, opts plugin.MetadataOptions) (*plugin.ItemMetadata, error)
	Type() string
}

// NewMySQLTableParser 创建 MySQL 表解析器
func NewMySQLTableParser() *MySQLTableParser {
	return &MySQLTableParser{
		typeMapper: format.GetTypeMapper("mysql"),
	}
}

// ParseTableInfo 从 MySQL 表中提取 TableInfo
func (p *MySQLTableParser) ParseTableInfo(ctx context.Context, db *gorm.DB, enginePlugin interface{}, schema, table string) (*format.TableInfo, error) {
	metadataHelper, ok := enginePlugin.(mySQLMetadataHelper)
	if !ok {
		return nil, fmt.Errorf("enginePlugin must provide MySQL metadata helpers, got %T", enginePlugin)
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

	// 3. 检查是否为空间表（MySQL Spatial）
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
func (p *MySQLTableParser) SupportedEngineTypes() []string {
	return []string{"mysql"}
}

// detectSpatialInfo 检测空间信息（MySQL Spatial）
func (p *MySQLTableParser) detectSpatialInfo(ctx context.Context, db *gorm.DB, schema, table string, fields []format.FieldInfo) *format.SpatialInfo {
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

	// MySQL 的空间类型信息可以从 INFORMATION_SCHEMA.COLUMNS 中获取
	// 但类型信息已经在 ColumnInfo 中，这里使用默认值
	// MySQL 默认使用 SRID 0（未定义），或 4326（WGS84）

	// 查询列的具体空间类型
	type SpatialColumn struct {
		DataType string
		SRS_ID   *int
	}

	var spatialCol SpatialColumn
	query := `
		SELECT
			DATA_TYPE as data_type,
			SRS_ID as srs_id
		FROM INFORMATION_SCHEMA.COLUMNS
		WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ? AND COLUMN_NAME = ?
	`

	err := db.WithContext(ctx).Raw(query, schema, table, geometryField.Name).Scan(&spatialCol).Error
	if err != nil {
		// 如果查询失败，使用默认值
		return &format.SpatialInfo{
			GeometryColumn: geometryField.Name,
			GeometryType:   "Geometry", // 默认类型
			SRID:           4326,       // WGS84 默认
			Dimension:      2,          // 2D 默认
		}
	}

	// 解析几何类型
	geomType := parseGeometryType(spatialCol.DataType)

	// MySQL 8.0+ 支持 SRID，如果没有则使用 0
	srid := 0
	if spatialCol.SRS_ID != nil {
		srid = *spatialCol.SRS_ID
	}
	if srid == 0 {
		srid = 4326 // 默认使用 WGS84
	}

	return &format.SpatialInfo{
		GeometryColumn: geometryField.Name,
		GeometryType:   geomType,
		SRID:           srid,
		Dimension:      2, // MySQL 主要支持 2D，暂不支持 Z/M
	}
}

// parseGeometryType 解析 MySQL 几何类型名称
func parseGeometryType(dataType string) string {
	// MySQL 空间类型: GEOMETRY, POINT, LINESTRING, POLYGON, MULTIPOINT, MULTILINESTRING, MULTIPOLYGON, GEOMETRYCOLLECTION
	// 转换为标准 OGC 类型名称
	switch dataType {
	case "POINT", "point":
		return "Point"
	case "LINESTRING", "linestring":
		return "LineString"
	case "POLYGON", "polygon":
		return "Polygon"
	case "MULTIPOINT", "multipoint":
		return "MultiPoint"
	case "MULTILINESTRING", "multilinestring":
		return "MultiLineString"
	case "MULTIPOLYGON", "multipolygon":
		return "MultiPolygon"
	case "GEOMETRYCOLLECTION", "geometrycollection":
		return "GeometryCollection"
	case "GEOMETRY", "geometry":
		return "Geometry"
	default:
		return "Geometry"
	}
}
