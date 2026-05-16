package format

import "fmt"

// SpatialInfo 表示 table/media 等 data type 上的空间横切事实。
type SpatialInfo struct {
	GeometryColumn  string
	GeometryType    string
	SRID            int
	BoundingBox     *[4]float64
	HasSpatialIndex bool
	IndexName       string
	Dimension       int
}

func (s *SpatialInfo) IsSRIDWGS84() bool {
	return s.SRID == 4326
}

func (s *SpatialInfo) IsSRIDWebMercator() bool {
	return s.SRID == 3857
}

func (s *SpatialInfo) GetBoundingBoxString() string {
	if s.BoundingBox == nil {
		return ""
	}
	bbox := *s.BoundingBox
	return fmt.Sprintf("[%.6f, %.6f, %.6f, %.6f]", bbox[0], bbox[1], bbox[2], bbox[3])
}
