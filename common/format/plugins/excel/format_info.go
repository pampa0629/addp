package excel

// Info 表示 Excel 格式私有信息。
type Info struct {
	SheetName  string
	SheetIndex int
	SheetCount int
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	return map[string]interface{}{
		"sheet_name":  i.SheetName,
		"sheet_index": i.SheetIndex,
		"sheet_count": i.SheetCount,
	}
}
