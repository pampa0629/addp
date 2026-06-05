package jsonrecords

// Feature 表示单条 GeoJSON Feature 或 JSON 对象记录。
type Feature struct {
	ID            interface{}
	Geometry      map[string]interface{}
	GeometryField string
	Properties    map[string]interface{}
}

// GeometryType 返回几何类型（Point/LineString/Polygon/等）。
func (f *Feature) GeometryType() string {
	if f.Geometry == nil {
		return ""
	}
	if v, ok := f.Geometry["type"].(string); ok {
		return v
	}
	return ""
}

// ToRecord 转换为通用记录（属性 + 几何字段）。
func (f *Feature) ToRecord(geometryField string) map[string]interface{} {
	record := make(map[string]interface{}, len(f.Properties)+1)
	for k, v := range f.Properties {
		record[k] = v
	}
	if f.GeometryType() != "" {
		field := geometryField
		if f.GeometryField != "" {
			field = f.GeometryField
		}
		record[field] = f.Geometry
	}
	return record
}

// Metadata 解析的附加信息。
type Metadata struct {
	BoundingBox      []float64
	CoordinateSystem string
}
