package format

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

	SpatialRefSys string

	SheetName   string
	SheetIndex  int
	TableSample *TableSampleOptions
}

// TableSampleOptions 描述 TableSampleReader 输入流的上下文。
//
// 默认情况下 SampleTable 的 input 是资源起点。InputIsPositioned=true 时，
// input 必须从某个数据行边界开始，InputStartsAtRow 表示该局部流第一条
// 数据行的全局行号，Fields 提供列定义，格式 plugin 不再从 input 读取表头。
type TableSampleOptions struct {
	Fields            []FieldInfo
	InputStartsAtRow  int64
	InputHasHeader    bool
	InputIsPositioned bool
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
	}
}
