package format

import (
	"context"
	"io"
)

// Parser 是所有格式解析器的通用接口
// 提供从原始数据读取到结构化数据的转换能力
type Parser interface {
	// ParseSchema 从数据源中提取 Schema 信息
	// 不读取实际数据，只解析结构（列名、类型等）
	ParseSchema(ctx context.Context, input io.Reader) (*Schema, error)

	// SupportedFormats 返回此解析器支持的格式类型
	SupportedFormats() []FormatType
}

// DataReader 扩展了 Parser，支持读取实际数据记录
// 用于数据预览、数据传输等场景
type DataReader interface {
	Parser

	// ReadRecords 读取数据记录
	// offset: 起始位置（从0开始）
	// limit: 最多读取记录数（-1表示读取全部）
	// 返回: []map[string]interface{} - 每条记录是一个字段名到值的映射
	ReadRecords(ctx context.Context, input io.Reader, offset, limit int64) ([]map[string]interface{}, error)

	// CountRecords 统计总记录数
	// 返回: 记录总数，如果无法快速统计则返回 -1
	CountRecords(ctx context.Context, input io.Reader) (int64, error)
}

// MetadataExtractor 扩展了 Parser，支持提取文件级元数据
// 用于 Meta 模块的元数据扫描
type MetadataExtractor interface {
	Parser

	// ExtractMetadata 提取文件元数据
	// 返回通用的元数据信息（大小、修改时间、作者等）
	ExtractMetadata(ctx context.Context, input io.Reader) (map[string]interface{}, error)
}

// ParseResult 解析结果的通用结构
type ParseResult struct {
	Schema   *Schema           // Schema 信息
	Records  []map[string]interface{} // 数据记录
	Metadata map[string]interface{}   // 文件元数据
}

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
