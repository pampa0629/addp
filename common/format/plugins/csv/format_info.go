package csv

// Info 表示 CSV/TSV 格式私有信息。
type Info struct {
	Delimiter  rune
	Encoding   string
	HasHeader  bool
	QuoteChar  rune
	EscapeChar rune
	LineEnding string
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	return map[string]interface{}{
		"delimiter":   string(i.Delimiter),
		"encoding":    i.Encoding,
		"has_header":  i.HasHeader,
		"quote_char":  string(i.QuoteChar),
		"escape_char": string(i.EscapeChar),
		"line_ending": i.LineEnding,
	}
}
