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
// 注意：enginePlugin 应提供 CatalogModelProvider 与 ItemMetadataProvider 能力。
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

// ============ 文档数据库集合解析器 ============

// DocCollectionParser 文档数据库集合解析器
// 用于从 MongoDB、CouchDB 等文档数据库中提取 Collection 的元数据
type DocCollectionParser interface {
	// ParseTableInfo 从 Collection 中提取 TableInfo
	// 通过采样文档推断 Schema，混合类型字段标记为 'mixed'
	// 参数:
	//   - ctx: 上下文
	//   - client: 数据库客户端或 provider 内部采样上下文
	//   - database: 数据库名称
	//   - collection: 集合名称
	//   - options: 解析选项（SampleSize 控制采样数量，默认 100）
	// 返回: TableInfo（包含推断的字段定义和 DocCollectionInfo 扩展）
	ParseTableInfo(ctx context.Context, client interface{}, database, collection string, options *ParseOptions) (*TableInfo, error)

	// ReadPreview 读取 Collection 的预览数据
	// 参数:
	//   - ctx: 上下文
	//   - client: 数据库客户端
	//   - database: 数据库名称
	//   - collection: 集合名称
	//   - offset: 起始位置（从0开始）
	//   - limit: 最多读取文档数
	//   - options: 解析选项（可选）
	// 返回: 文档列表（每条记录是字段名到值的映射）
	ReadPreview(ctx context.Context, client interface{}, database, collection string, offset, limit int64, options *ParseOptions) ([]map[string]interface{}, error)

	// SupportedEngineTypes 返回支持的引擎类型
	// 例如: ["mongodb", "couchdb"]
	SupportedEngineTypes() []string
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
