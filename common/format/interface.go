package format

import (
	"context"
	"io"
	"time"

	"gorm.io/gorm"
)

// ============ 数据库表解析器 ============

// DBTableParser 数据库表解析器
// 从数据库引擎中提取表的元数据信息（TableInfo）
//
// 注意：需要导入 "github.com/addp/common/engine/plugin" 以使用 plugin.RelationalDBPlugin
type DBTableParser interface {
	// ParseTableInfo 从数据库表中提取 TableInfo
	// 参数:
	//   - ctx: 上下文
	//   - db: GORM数据库实例
	//   - enginePlugin: 数据库引擎插件（用于调用数据库特定的元数据查询方法）
	//   - schema: Schema名称（PostgreSQL）或数据库名（MySQL）
	//   - table: 表名
	// 返回: TableInfo（包含字段定义、主键、扩展信息等）
	//
	// 使用示例:
	//   parser := db.NewPostgreSQLTableParser()
	//   tableInfo, err := parser.ParseTableInfo(ctx, dbConn, pgPlugin, "public", "users")
	ParseTableInfo(ctx context.Context, db *gorm.DB, enginePlugin interface{}, schema, table string) (*TableInfo, error)

	// SupportedEngineTypes 返回支持的数据库引擎类型
	// 例如: ["postgresql", "mysql", "clickhouse"]
	SupportedEngineTypes() []string
}

// ============ 文件表解析器 ============

// FileTableParser 文件表解析器
// 从文件（CSV、Shapefile、GeoJSON等）中提取表格结构的元数据
type FileTableParser interface {
	// ParseTableInfo 从文件中提取 TableInfo
	// 参数:
	//   - ctx: 上下文
	//   - input: 文件输入流
	//   - options: 解析选项（可选，nil表示使用默认选项）
	// 返回: TableInfo（包含字段定义、扩展信息等）
	ParseTableInfo(ctx context.Context, input io.Reader, options *ParseOptions) (*TableInfo, error)

	// ReadPreview 读取文件的预览数据
	// 参数:
	//   - ctx: 上下文
	//   - input: 文件输入流
	//   - offset: 起始行（从0开始）
	//   - limit: 最多读取行数（-1表示全部）
	//   - options: 解析选项（可选，nil表示使用默认选项）
	// 返回: 记录列表（每条记录是字段名到值的映射）
	ReadPreview(ctx context.Context, input io.Reader, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)

	// SupportedFormats 返回支持的文件格式
	// 例如: [FormatCSV, FormatShapefile, FormatGeoJSON]
	SupportedFormats() []FormatType
}

// ============ 对象信息解析器 ============

// ObjectInfoParser 对象信息解析器
// 用于从MinIO/S3等对象存储中的文件提取完整的 ObjectInfo（包含扩展信息）
type ObjectInfoParser interface {
	// ParseObjectInfo 从对象存储文件中提取 ObjectInfo
	// 参数:
	//   - ctx: 上下文
	//   - input: 文件输入流
	//   - basicInfo: 基础对象信息（从对象存储获取）
	// 返回: ObjectInfo（包含扩展信息，如 ImageInfo、VideoInfo等）
	ParseObjectInfo(ctx context.Context, input io.Reader, basicInfo ObjectBasicInfo) (*ObjectInfo, error)

	// SupportedContentTypes 返回支持的MIME类型
	// 例如: ["image/jpeg", "image/png", "video/mp4"]
	SupportedContentTypes() []string
}

// ObjectBasicInfo 对象基础信息（从对象存储获取）
type ObjectBasicInfo struct {
	Key         string    // 对象键（完整路径）
	SizeBytes   int64     // 对象大小（字节）
	ContentType string    // MIME 类型
	ETag        string    // ETag（对象版本标识）
	ModifiedAt  time.Time // 最后修改时间
}

// ============ 解析选项 ============

// ParseOptions 解析选项
type ParseOptions struct {
	// 通用选项
	Encoding    string                 // 字符编码（如 "utf-8", "gbk"）
	SkipRows    int                    // 跳过的行数
	MaxRows     int64                  // 最多读取的行数
	SampleSize  int                    // 采样大小（用于类型推断）
	ExtraParams map[string]interface{} // 格式特定的额外参数

	// CSV 特定选项
	Delimiter rune // CSV 分隔符
	HasHeader bool // 是否有表头

	// Shapefile 特定选项
	SpatialRefSys string // 空间参考系统（如 "EPSG:4326"）

	// Excel 特定选项
	SheetName  string // 工作表名称
	SheetIndex int    // 工作表索引（从0开始）
}

// DefaultParseOptions 返回默认解析选项
func DefaultParseOptions() *ParseOptions {
	return &ParseOptions{
		Encoding:   "utf-8",
		SkipRows:   0,
		MaxRows:    -1, // 读取全部
		SampleSize: 100,
		Delimiter:  ',',
		HasHeader:  true,
		SheetIndex: 0,
	}
}

// ============ 向后兼容（已废弃）============

// Parser 旧的解析器接口（已废弃）
// @Deprecated: 使用 FileTableParser 替代
type Parser interface {
	// ParseSchema 从数据源中提取 Schema 信息
	// @Deprecated: 使用 FileTableParser.ParseTableInfo() 替代
	ParseSchema(ctx context.Context, input io.Reader) (*Schema, error)

	// SupportedFormats 返回此解析器支持的格式类型
	SupportedFormats() []FormatType
}

// DataReader 旧的数据读取器接口（已废弃）
// @Deprecated: 已合并到 FileTableParser.ReadPreview
type DataReader interface {
	Parser

	// ReadRecords 读取数据记录
	// @Deprecated: 使用 FileTableParser.ReadPreview() 替代
	ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error)

	// CountRecords 统计总记录数
	// @Deprecated: 不再需要此方法，改用 TableInfo.RowCount
	CountRecords(ctx context.Context, input io.Reader) (int64, error)
}

// MetadataExtractor 旧的元数据提取器接口（已废弃）
// @Deprecated: 已合并到 FileTableParser.ParseTableInfo（返回的 TableInfo 包含完整元数据）
type MetadataExtractor interface {
	Parser

	// ExtractMetadata 提取文件元数据
	// @Deprecated: 使用 FileTableParser.ParseTableInfo() 或 ObjectInfoParser.ParseObjectInfo() 替代
	ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error)
}

// ParseResult 解析结果的通用结构（已废弃）
// @Deprecated: 使用 TableInfo 替代
type ParseResult struct {
	Schema   *Schema                  // Schema 信息
	Records  []map[string]interface{} // 数据记录
	Metadata map[string]interface{}   // 文件元数据
}
