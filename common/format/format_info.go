package format

// CSVInfo 表示 CSV/TSV 格式私有信息。
type CSVInfo struct {
	Delimiter  rune
	Encoding   string
	HasHeader  bool
	QuoteChar  rune
	EscapeChar rune
	LineEnding string
}

func (c *CSVInfo) ExtensionType() string {
	return "csv"
}

func (c *CSVInfo) FormatAttributes() map[string]interface{} {
	if c == nil {
		return nil
	}
	return map[string]interface{}{
		"delimiter":   string(c.Delimiter),
		"encoding":    c.Encoding,
		"has_header":  c.HasHeader,
		"quote_char":  string(c.QuoteChar),
		"escape_char": string(c.EscapeChar),
		"line_ending": c.LineEnding,
	}
}

// ShapefileInfo 表示 Shapefile 格式私有信息。
type ShapefileInfo struct {
	Encoding   string
	ShapeType  string
	HasPRJ     bool
	HasCPG     bool
	DBFVersion byte
}

func (s *ShapefileInfo) ExtensionType() string {
	return "shapefile"
}

func (s *ShapefileInfo) FormatAttributes() map[string]interface{} {
	if s == nil {
		return nil
	}
	return map[string]interface{}{
		"encoding":    s.Encoding,
		"shape_type":  s.ShapeType,
		"has_prj":     s.HasPRJ,
		"has_cpg":     s.HasCPG,
		"dbf_version": s.DBFVersion,
	}
}

// ExcelInfo 表示 Excel 格式私有信息。
type ExcelInfo struct {
	SheetName  string
	SheetIndex int
	SheetCount int
}

func (e *ExcelInfo) ExtensionType() string {
	return "excel"
}

func (e *ExcelInfo) FormatAttributes() map[string]interface{} {
	if e == nil {
		return nil
	}
	return map[string]interface{}{
		"sheet_name":  e.SheetName,
		"sheet_index": e.SheetIndex,
		"sheet_count": e.SheetCount,
	}
}
