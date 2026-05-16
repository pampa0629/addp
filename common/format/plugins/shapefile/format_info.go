package shapefile

// FormatInfo 表示 Shapefile 格式私有信息。
type FormatInfo struct {
	Encoding   string
	ShapeType  string
	HasPRJ     bool
	HasCPG     bool
	DBFVersion byte
}

func (i *FormatInfo) FormatAttributes() map[string]interface{} {
	if i == nil {
		return nil
	}
	return map[string]interface{}{
		"encoding":    i.Encoding,
		"shape_type":  i.ShapeType,
		"has_prj":     i.HasPRJ,
		"has_cpg":     i.HasCPG,
		"dbf_version": i.DBFVersion,
	}
}
