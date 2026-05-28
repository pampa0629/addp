package excel

// Info 表示 Excel 格式私有信息。
type Info struct {
	SheetCount      int
	DefaultSheet    string
	SampledSheets   int
	SheetsTruncated bool
	RowsTruncated   bool
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"sheet_count": i.SheetCount,
	}
	if i.DefaultSheet != "" {
		attrs["default_sheet"] = i.DefaultSheet
	}
	if i.SampledSheets > 0 {
		attrs["sampled_sheets"] = i.SampledSheets
	}
	attrs["sheets_truncated"] = i.SheetsTruncated
	attrs["rows_truncated"] = i.RowsTruncated
	return attrs
}
