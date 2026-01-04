package format

// CSVInfo CSV 格式扩展信息
type CSVInfo struct {
	Delimiter  rune   // 分隔符（',' 或 '\t' 等）
	Encoding   string // 字符编码（如 "utf-8", "gbk"）
	HasHeader  bool   // 是否有表头
	QuoteChar  rune   // 引用字符（通常是 '"'）
	EscapeChar rune   // 转义字符
	LineEnding string // 行结束符（"\n" 或 "\r\n"）
}

func (c *CSVInfo) ExtensionType() string {
	return "csv"
}

// ShapefileInfo Shapefile 格式扩展信息
type ShapefileInfo struct {
	Encoding   string // DBF 编码（如 "utf-8", "gbk", "cp936"）
	ShapeType  string // Shape 类型（POINT, POLYLINE, POLYGON, etc.）
	HasPRJ     bool   // 是否有 .prj 文件（投影信息）
	HasCPG     bool   // 是否有 .cpg 文件（编码信息）
	DBFVersion byte   // DBF 版本号
}

func (s *ShapefileInfo) ExtensionType() string {
	return "shapefile"
}

// GeoJSONInfo GeoJSON 格式扩展信息
type GeoJSONInfo struct {
	FeatureCount int    // Feature 数量
	CRS          string // 坐标系（如 "EPSG:4326"）
}

func (g *GeoJSONInfo) ExtensionType() string {
	return "geojson"
}

// ExcelInfo Excel 格式扩展信息
type ExcelInfo struct {
	SheetName  string // 工作表名称
	SheetIndex int    // 工作表索引（从0开始）
	SheetCount int    // 工作表总数
}

func (e *ExcelInfo) ExtensionType() string {
	return "excel"
}
