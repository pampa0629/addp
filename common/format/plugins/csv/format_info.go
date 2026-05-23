package csv

import "github.com/addp/common/datatype"

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
		"encoding":    i.Encoding,
		"line_ending": i.LineEnding,
	}
}

var tableNativeKeys = datatype.NewNativeAllowedKeys("delimiter", "has_header", "quote_char", "escape_char")

func (i *Info) TableNative() map[string]interface{} {
	if i == nil {
		return nil
	}
	return datatype.FilterTableNative(map[string]interface{}{
		"delimiter":   string(i.Delimiter),
		"has_header":  i.HasHeader,
		"quote_char":  string(i.QuoteChar),
		"escape_char": string(i.EscapeChar),
	}, tableNativeKeys)
}
