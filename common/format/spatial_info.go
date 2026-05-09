package format

import "fmt"

// SpatialInfo 空间数据扩展信息
// 适用于: PostgreSQL/PostGIS表、Shapefile、JSON 空间扩展、GeoPackage
type SpatialInfo struct {
	GeometryColumn  string      // 几何字段名（如 "geom", "geometry", "shape"）
	GeometryType    string      // 几何类型（Point, LineString, Polygon, MultiPoint, etc.）
	SRID            int         // 空间参考系统 ID（如 4326 for WGS84, 3857 for Web Mercator）
	BoundingBox     *[4]float64 // 边界框 [minX, minY, maxX, maxY]（WGS84 度）
	HasSpatialIndex bool        // 是否有空间索引
	IndexName       string      // 空间索引名称（如果有）
	Dimension       int         // 维度（2D, 3D, 4D）
}

func (s *SpatialInfo) ExtensionType() string {
	return "spatial"
}

// IsSRIDWGS84 判断是否为 WGS84 坐标系
func (s *SpatialInfo) IsSRIDWGS84() bool {
	return s.SRID == 4326
}

// IsSRIDWebMercator 判断是否为 Web Mercator 坐标系
func (s *SpatialInfo) IsSRIDWebMercator() bool {
	return s.SRID == 3857
}

// GetBoundingBoxString 获取边界框字符串表示
func (s *SpatialInfo) GetBoundingBoxString() string {
	if s.BoundingBox == nil {
		return ""
	}
	bbox := *s.BoundingBox
	return fmt.Sprintf("[%.6f, %.6f, %.6f, %.6f]", bbox[0], bbox[1], bbox[2], bbox[3])
}
