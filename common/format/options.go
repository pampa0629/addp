package format

// ParseOptions 解析选项。
type ParseOptions struct {
	Encoding    string
	SkipRows    int
	MaxRows     int64
	SampleSize  int
	ExtraParams map[string]interface{}

	Delimiter rune
	HasHeader bool

	SpatialRefSys string

	SheetName  string
	SheetIndex int
}

// DefaultParseOptions 返回默认解析选项。
func DefaultParseOptions() *ParseOptions {
	return &ParseOptions{
		Encoding:   "utf-8",
		SkipRows:   0,
		MaxRows:    -1,
		SampleSize: 100,
		Delimiter:  ',',
		HasHeader:  true,
		SheetIndex: 0,
	}
}
