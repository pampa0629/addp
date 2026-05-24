package format

import "github.com/addp/common/datatype"

// GeometryEncoding 描述表格行值中 geometry 字段的编码形式。
type GeometryEncoding string

const (
	GeometryEncodingWKT  GeometryEncoding = "wkt"
	GeometryEncodingWKB  GeometryEncoding = "wkb"
	GeometryEncodingEWKB GeometryEncoding = "ewkb"
)

type MissingFieldPolicy string

const (
	MissingFieldError  MissingFieldPolicy = "error"
	MissingFieldIgnore MissingFieldPolicy = "ignore"
)

const FieldSelectionOptionKey = "field_selection"

// ParseOptions 解析选项。
type ParseOptions struct {
	Encoding         string
	SkipRows         int
	MaxRows          int64
	SampleSize       int
	ExtraParams      map[string]interface{}
	ContentIndexStep int64

	Delimiter rune
	HasHeader bool

	SpatialRefSys    string
	GeometryEncoding GeometryEncoding

	SheetName   string
	SheetIndex  int
	TableSample *TableSampleOptions

	FieldSelection *FieldSelectionOptions
}

type FieldSelectionOptions struct {
	Include            []string
	MissingFieldPolicy MissingFieldPolicy
}

// TableSampleOptions 描述 TableSampleReader 输入流的上下文。
//
// 默认情况下 SampleTable 的 input 是资源起点。InputIsPositioned=true 时，
// input 必须从某个数据行边界开始，InputStartsAtRow 表示该局部流第一条
// 数据行的全局行号，Fields 提供列定义，格式 plugin 不再从 input 读取表头。
type TableSampleOptions struct {
	Fields            []datatype.FieldInfo
	InputStartsAtRow  int64
	InputHasHeader    bool
	InputIsPositioned bool
}

// WriteOptions 描述 data type writer 的通用写出选项。
//
// 具体格式可以通过 ExtraParams 扩展格式私有选项；通用字段只放跨格式稳定语义。
type WriteOptions struct {
	Encoding    string
	ExtraParams map[string]interface{}

	Delimiter   rune
	OmitHeader  bool
	SpatialInfo *datatype.SpatialInfo
}

// DefaultWriteOptions 返回默认写出选项。
func DefaultWriteOptions() *WriteOptions {
	return &WriteOptions{
		Encoding:  "utf-8",
		Delimiter: ',',
	}
}

// DefaultParseOptions 返回默认解析选项。
func DefaultParseOptions() *ParseOptions {
	return &ParseOptions{
		Encoding:         "utf-8",
		SkipRows:         0,
		MaxRows:          -1,
		SampleSize:       100,
		Delimiter:        ',',
		HasHeader:        true,
		SheetIndex:       0,
		ContentIndexStep: 5000,
		GeometryEncoding: GeometryEncodingWKT,
	}
}

func (s *FieldSelectionOptions) EffectiveMissingFieldPolicy() MissingFieldPolicy {
	if s == nil || s.MissingFieldPolicy == "" {
		return MissingFieldError
	}
	return s.MissingFieldPolicy
}
