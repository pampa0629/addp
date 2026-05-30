package datatype

import commonJSON "github.com/addp/common/jsonmap"

// SpatialInfo describes spatial facts that cut across data types.
type SpatialInfo struct {
	SRID                  *int                 `json:"srid,omitempty"`
	CRS                   string               `json:"crs,omitempty"`
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

// NewSingleGeometrySpatialInfo returns a SpatialInfo with one geometry column.
func NewSingleGeometrySpatialInfo(columnName, geometryType string, srid int, dimension int) *SpatialInfo {
	column := GeometryColumnInfo{
		Name:         columnName,
		GeometryType: geometryType,
	}
	if srid > 0 {
		column.SRID = &srid
	}
	if dimension > 0 {
		column.Dimension = &dimension
	}
	return &SpatialInfo{
		GeometryColumns:       []GeometryColumnInfo{column},
		PrimaryGeometryColumn: columnName,
	}
}

// NewBoundingBox returns a bounding box using [min_x, min_y, max_x, max_y] order.
func NewBoundingBox(minX, minY, maxX, maxY float64) BoundingBox {
	return BoundingBox{minX, minY, maxX, maxY}
}

// SpatialInfoFromPayload restores common spatial facts from a spatial JSON payload.
func SpatialInfoFromPayload(payload map[string]interface{}) *SpatialInfo {
	if len(payload) == 0 {
		return nil
	}

	geometryColumns := geometryColumnsFromPayload(payload)
	primaryName := commonJSON.InterfaceString(payload["primary_geometry_column"])
	if primaryName == "" && len(geometryColumns) == 1 {
		primaryName = geometryColumns[0].Name
	}

	info := &SpatialInfo{
		GeometryColumns:       geometryColumns,
		PrimaryGeometryColumn: primaryName,
		CRS:                   commonJSON.InterfaceString(payload["crs"]),
		IndexName:             commonJSON.InterfaceString(payload["index_name"]),
	}
	if srid := int(commonJSON.InterfaceInt64(payload["srid"])); srid > 0 {
		info.SRID = &srid
	}
	if _, ok := payload["has_spatial_index"]; ok {
		hasSpatialIndex := commonJSON.InterfaceBool(payload["has_spatial_index"])
		info.HasSpatialIndex = &hasSpatialIndex
	}
	if extent := commonJSON.InterfaceFloat64Slice(payload["extent"]); len(extent) == 4 {
		boundingBox := BoundingBox{extent[0], extent[1], extent[2], extent[3]}
		info.Extent = &boundingBox
	}
	if primaryName == "" && len(geometryColumns) == 0 && info.SRID == nil && info.CRS == "" && info.Extent == nil && info.HasSpatialIndex == nil && info.IndexName == "" {
		return nil
	}
	return info
}

// SpatialInfoPayload converts common spatial facts to a JSON payload.
func SpatialInfoPayload(info *SpatialInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	srid := info.SRID
	crs := info.CRS
	if len(info.GeometryColumns) == 1 && info.GeometryColumns[0].Name == "" {
		if srid == nil {
			srid = info.GeometryColumns[0].SRID
		}
		if crs == "" {
			crs = info.GeometryColumns[0].CRS
		}
	}
	if srid != nil {
		crs = ""
	}
	payload := commonJSON.MapFromStruct(spatialInfoPayload{
		SRID:                  srid,
		CRS:                   crs,
		PrimaryGeometryColumn: info.PrimaryGeometryColumn,
		HasSpatialIndex:       info.HasSpatialIndex,
		IndexName:             info.IndexName,
	})
	if payload == nil {
		payload = map[string]interface{}{}
	}
	if len(info.GeometryColumns) > 0 {
		geometryColumns := geometryColumnPayloads(info.GeometryColumns)
		if len(geometryColumns) > 0 {
			payload["geometry_columns"] = geometryColumns
		}
	}
	if info.Extent != nil {
		bbox := *info.Extent
		payload["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	}
	if len(payload) == 0 {
		return nil
	}
	return payload
}

type spatialInfoPayload struct {
	SRID                  *int   `json:"srid,omitempty"`
	CRS                   string `json:"crs,omitempty"`
	PrimaryGeometryColumn string `json:"primary_geometry_column,omitempty"`
	HasSpatialIndex       *bool  `json:"has_spatial_index,omitempty"`
	IndexName             string `json:"index_name,omitempty"`
}

type geometryColumnPayload struct {
	Name         string `json:"name,omitempty"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRS          string `json:"crs,omitempty"`
	Dimension    *int   `json:"dimension,omitempty"`
	Nullable     *bool  `json:"nullable,omitempty"`
}

func geometryColumnsFromPayload(payload map[string]interface{}) []GeometryColumnInfo {
	items := commonJSON.InterfaceSlice(payload["geometry_columns"])
	columns := make([]GeometryColumnInfo, 0, len(items))
	for _, item := range items {
		columnPayload := commonJSON.InterfaceMap(item)
		name := commonJSON.InterfaceString(columnPayload["name"])
		if name == "" {
			continue
		}
		column := GeometryColumnInfo{
			Name:         name,
			GeometryType: commonJSON.InterfaceString(columnPayload["geometry_type"]),
			CRS:          commonJSON.InterfaceString(columnPayload["crs"]),
		}
		if srid := int(commonJSON.InterfaceInt64(columnPayload["srid"])); srid > 0 {
			column.SRID = &srid
		}
		if dimension := int(commonJSON.InterfaceInt64(columnPayload["dimension"])); dimension > 0 {
			column.Dimension = &dimension
		}
		if _, ok := columnPayload["nullable"]; ok {
			nullable := commonJSON.InterfaceBool(columnPayload["nullable"])
			column.Nullable = &nullable
		}
		columns = append(columns, column)
	}
	return columns
}

func geometryColumnPayloads(columns []GeometryColumnInfo) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(columns))
	for _, column := range columns {
		if column.Name == "" {
			continue
		}
		crs := column.CRS
		if column.SRID != nil {
			crs = ""
		}
		payload := commonJSON.MapFromStruct(geometryColumnPayload{
			Name:         column.Name,
			GeometryType: column.GeometryType,
			SRID:         column.SRID,
			CRS:          crs,
			Dimension:    column.Dimension,
			Nullable:     column.Nullable,
		})
		if len(payload) == 0 {
			continue
		}
		result = append(result, payload)
	}
	return result
}

// Clone returns a deep copy of SpatialInfo.
func (s *SpatialInfo) Clone() *SpatialInfo {
	if s == nil {
		return nil
	}
	cloned := &SpatialInfo{
		SRID:                  cloneIntPtr(s.SRID),
		CRS:                   s.CRS,
		GeometryColumns:       make([]GeometryColumnInfo, 0, len(s.GeometryColumns)),
		PrimaryGeometryColumn: s.PrimaryGeometryColumn,
		IndexName:             s.IndexName,
	}
	for _, column := range s.GeometryColumns {
		nextColumn := column
		if column.SRID != nil {
			srid := *column.SRID
			nextColumn.SRID = &srid
		}
		if column.Dimension != nil {
			dimension := *column.Dimension
			nextColumn.Dimension = &dimension
		}
		if column.Nullable != nil {
			nullable := *column.Nullable
			nextColumn.Nullable = &nullable
		}
		cloned.GeometryColumns = append(cloned.GeometryColumns, nextColumn)
	}
	if s.Extent != nil {
		extent := *s.Extent
		cloned.Extent = &extent
	}
	if s.HasSpatialIndex != nil {
		hasSpatialIndex := *s.HasSpatialIndex
		cloned.HasSpatialIndex = &hasSpatialIndex
	}
	return cloned
}

func cloneIntPtr(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
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

// PrimaryGeometryName returns the primary geometry column name.
func (s *SpatialInfo) PrimaryGeometryName() string {
	column := s.PrimaryGeometry()
	if column == nil {
		return ""
	}
	return column.Name
}

// PrimaryGeometryType returns the primary geometry type.
func (s *SpatialInfo) PrimaryGeometryType() string {
	column := s.PrimaryGeometry()
	if column == nil {
		return ""
	}
	return column.GeometryType
}

// PrimaryDimension returns the coordinate dimension of the primary geometry column.
func (s *SpatialInfo) PrimaryDimension() (int, bool) {
	column := s.PrimaryGeometry()
	if column == nil || column.Dimension == nil {
		return 0, false
	}
	return *column.Dimension, true
}

// PrimaryDimensionValue returns the primary geometry dimension, or 0 when unknown.
func (s *SpatialInfo) PrimaryDimensionValue() int {
	dimension, ok := s.PrimaryDimension()
	if !ok {
		return 0
	}
	return dimension
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

// PrimarySRIDValue returns the primary geometry SRID, or 0 when unknown.
func (s *SpatialInfo) PrimarySRIDValue() int {
	srid, ok := s.PrimarySRID()
	if !ok {
		return 0
	}
	return srid
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
