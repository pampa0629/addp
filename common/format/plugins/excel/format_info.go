package excel

// Info 表示 Excel 格式私有信息。
type Info struct {
	SheetCount int
}

func (i *Info) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	return map[string]interface{}{
		"sheet_count": i.SheetCount,
	}
}
