package datatype

// SpatialInfo describes spatial facts that cut across data types.
type SpatialInfo struct {
	GeometryColumns       []GeometryColumnInfo `json:"geometry_columns,omitempty"`
	PrimaryGeometryColumn string               `json:"primary_geometry_column,omitempty"`
	Extent                *BoundingBox         `json:"extent,omitempty"`
	HasSpatialIndex       *bool                `json:"has_spatial_index,omitempty"`
	IndexName             string               `json:"index_name,omitempty"`
}

// GeometryColumnInfo describes one spatial field.
type GeometryColumnInfo struct {
	Name         string `json:"name,omitempty"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRS          string `json:"crs,omitempty"`
	Dimension    *int   `json:"dimension,omitempty"`
	Nullable     *bool  `json:"nullable,omitempty"`
}

// BoundingBox stores [min_x, min_y, max_x, max_y].
type BoundingBox [4]float64

// NewBoundingBox returns a bounding box using [min_x, min_y, max_x, max_y] order.
func NewBoundingBox(minX, minY, maxX, maxY float64) BoundingBox {
	return BoundingBox{minX, minY, maxX, maxY}
}

// PrimaryGeometry returns the primary geometry column when it can be determined.
func (s *SpatialInfo) PrimaryGeometry() *GeometryColumnInfo {
	if s == nil || len(s.GeometryColumns) == 0 {
		return nil
	}
	if s.PrimaryGeometryColumn != "" {
		for i := range s.GeometryColumns {
			if s.GeometryColumns[i].Name == s.PrimaryGeometryColumn {
				return &s.GeometryColumns[i]
			}
		}
		return nil
	}
	if len(s.GeometryColumns) == 1 {
		return &s.GeometryColumns[0]
	}
	return nil
}

// GeometryColumnNames returns all spatial field names in declared order.
func (s *SpatialInfo) GeometryColumnNames() []string {
	if s == nil || len(s.GeometryColumns) == 0 {
		return nil
	}
	names := make([]string, len(s.GeometryColumns))
	for i, column := range s.GeometryColumns {
		names[i] = column.Name
	}
	return names
}

// IsSpatial reports whether at least one geometry column is declared.
func (s *SpatialInfo) IsSpatial() bool {
	return s != nil && len(s.GeometryColumns) > 0
}

// PrimarySRID returns the SRID of the primary geometry column.
func (s *SpatialInfo) PrimarySRID() (int, bool) {
	column := s.PrimaryGeometry()
	if column == nil || column.SRID == nil {
		return 0, false
	}
	return *column.SRID, true
}

// IsPrimaryWGS84 reports whether the primary geometry column uses EPSG:4326.
func (s *SpatialInfo) IsPrimaryWGS84() bool {
	srid, ok := s.PrimarySRID()
	return ok && srid == 4326
}

// IsPrimaryWebMercator reports whether the primary geometry column uses EPSG:3857.
func (s *SpatialInfo) IsPrimaryWebMercator() bool {
	srid, ok := s.PrimarySRID()
	return ok && srid == 3857
}
