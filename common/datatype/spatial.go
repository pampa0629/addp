package datatype

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"

	commonJSON "github.com/addp/common/jsonmap"
)

const (
	CRSDefinitionEncodingWKT     = "wkt"
	CRSDefinitionEncodingESRIWKT = "esri_wkt"
	CRSDefinitionEncodingProj4   = "proj4"

	CRSDefinitionSourcePostGISSpatialRefSys = "postgis_spatial_ref_sys"
	CRSDefinitionSourceMySQLSpatialRefSys   = "mysql_st_spatial_reference_systems"
	CRSDefinitionSourceSidecarPRJ           = "sidecar_prj"
	CRSDefinitionSourceGeoPackageSRS        = "geopackage_srs"
	CRSDefinitionSourceGeoTIFFTags          = "geotiff_tags"
	CRSDefinitionSourceSuperMapRuntimeSDK   = "supermap_runtime_sdk"
)

// SpatialInfo describes spatial facts that cut across data types.
type SpatialInfo struct {
	SRID                  *int                 `json:"srid,omitempty"`
	CRSRef                string               `json:"crs_ref,omitempty"`
	CRSDefinitions        []CRSDefinition      `json:"crs_definitions,omitempty"`
	GeometryColumns       []GeometryColumnInfo `json:"geometry_columns,omitempty"`
	PrimaryGeometryColumn string               `json:"primary_geometry_column,omitempty"`
	Extent                *BoundingBox         `json:"extent,omitempty"`
	HasSpatialIndex       *bool                `json:"has_spatial_index,omitempty"`
	IndexName             string               `json:"index_name,omitempty"`
}

// CRSDefinition describes the CRS definition text that can be registered by a consumer.
type CRSDefinition struct {
	ID                 string `json:"id,omitempty"`
	DefinitionEncoding string `json:"definition_encoding,omitempty"`
	Definition         string `json:"definition,omitempty"`
	Source             string `json:"source,omitempty"`
}

// GeometryColumnInfo describes one spatial field.
type GeometryColumnInfo struct {
	Name         string `json:"name,omitempty"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRSRef       string `json:"crs_ref,omitempty"`
	Dimension    *int   `json:"dimension,omitempty"`
	Nullable     *bool  `json:"nullable,omitempty"`
}

// BoundingBox stores [min_x, min_y, max_x, max_y].
type BoundingBox [4]float64

// NewSingleGeometrySpatialInfo returns a SpatialInfo with one geometry column.
func NewSingleGeometrySpatialInfo(columnName, geometryType string, srid int, dimension int) *SpatialInfo {
	column := GeometryColumnInfo{
		Name:         columnName,
		GeometryType: normalizeGeometryTypeString(geometryType),
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
		CRSRef:                commonJSON.InterfaceString(payload["crs_ref"]),
		CRSDefinitions:        crsDefinitionsFromPayload(payload),
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
	if primaryName == "" && len(geometryColumns) == 0 && info.SRID == nil && info.CRSRef == "" && len(info.CRSDefinitions) == 0 && info.Extent == nil && info.HasSpatialIndex == nil && info.IndexName == "" {
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
	crsRef := strings.TrimSpace(info.CRSRef)
	if len(info.GeometryColumns) == 1 && info.GeometryColumns[0].Name == "" {
		if srid == nil {
			srid = info.GeometryColumns[0].SRID
		}
		if crsRef == "" {
			crsRef = strings.TrimSpace(info.GeometryColumns[0].CRSRef)
		}
	}
	payload := commonJSON.MapFromStruct(spatialInfoPayload{
		SRID:                  srid,
		CRSRef:                crsRef,
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
	if definitions := crsDefinitionPayloads(info.CRSDefinitions); len(definitions) > 0 {
		payload["crs_definitions"] = definitions
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
	CRSRef                string `json:"crs_ref,omitempty"`
	PrimaryGeometryColumn string `json:"primary_geometry_column,omitempty"`
	HasSpatialIndex       *bool  `json:"has_spatial_index,omitempty"`
	IndexName             string `json:"index_name,omitempty"`
}

type geometryColumnPayload struct {
	Name         string `json:"name,omitempty"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRSRef       string `json:"crs_ref,omitempty"`
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
			GeometryType: normalizeGeometryTypeString(commonJSON.InterfaceString(columnPayload["geometry_type"])),
			CRSRef:       commonJSON.InterfaceString(columnPayload["crs_ref"]),
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
		payload := commonJSON.MapFromStruct(geometryColumnPayload{
			Name:         column.Name,
			GeometryType: normalizeGeometryTypeString(column.GeometryType),
			SRID:         column.SRID,
			CRSRef:       strings.TrimSpace(column.CRSRef),
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

func normalizeGeometryTypeString(value string) string {
	if geometryType := StandardGeometryType(value); geometryType != "" {
		return geometryType
	}
	return strings.TrimSpace(value)
}

// Clone returns a deep copy of SpatialInfo.
func (s *SpatialInfo) Clone() *SpatialInfo {
	if s == nil {
		return nil
	}
	cloned := &SpatialInfo{
		SRID:                  cloneIntPtr(s.SRID),
		CRSRef:                s.CRSRef,
		CRSDefinitions:        append([]CRSDefinition(nil), s.CRSDefinitions...),
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

func crsDefinitionsFromPayload(payload map[string]interface{}) []CRSDefinition {
	items := commonJSON.InterfaceSlice(payload["crs_definitions"])
	definitions := make([]CRSDefinition, 0, len(items))
	for _, item := range items {
		definitionPayload := commonJSON.InterfaceMap(item)
		definition := CRSDefinition{
			ID:                 commonJSON.InterfaceString(definitionPayload["id"]),
			DefinitionEncoding: commonJSON.InterfaceString(definitionPayload["definition_encoding"]),
			Definition:         commonJSON.InterfaceString(definitionPayload["definition"]),
			Source:             commonJSON.InterfaceString(definitionPayload["source"]),
		}
		if definition.ID == "" || definition.DefinitionEncoding == "" || definition.Definition == "" {
			continue
		}
		definitions = append(definitions, definition)
	}
	return definitions
}

func crsDefinitionPayloads(definitions []CRSDefinition) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(definitions))
	seen := map[string]struct{}{}
	for _, definition := range definitions {
		definition.ID = strings.TrimSpace(definition.ID)
		definition.DefinitionEncoding = strings.TrimSpace(definition.DefinitionEncoding)
		definition.Definition = strings.TrimSpace(definition.Definition)
		definition.Source = strings.TrimSpace(definition.Source)
		if definition.ID == "" || definition.DefinitionEncoding == "" || definition.Definition == "" {
			continue
		}
		if _, ok := seen[definition.ID]; ok {
			continue
		}
		seen[definition.ID] = struct{}{}
		payload := commonJSON.MapFromStruct(definition)
		if len(payload) > 0 {
			result = append(result, payload)
		}
	}
	return result
}

// EPSGCRSRef returns the canonical ADDP CRS reference for an EPSG code.
func EPSGCRSRef(srid int) string {
	if srid <= 0 {
		return ""
	}
	return fmt.Sprintf("EPSG:%d", srid)
}

// CustomCRSRef returns a deterministic ADDP CRS reference for definition text without an EPSG code.
func CustomCRSRef(definition string) string {
	trimmed := strings.TrimSpace(definition)
	if trimmed == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(trimmed))
	return "ADDP:CRS:" + hex.EncodeToString(sum[:])
}

// CRSRefFromAuthority returns an ADDP CRS reference from an authority/code pair or definition text.
func CRSRefFromAuthority(authority string, code int, definition string) string {
	if strings.EqualFold(strings.TrimSpace(authority), "EPSG") && code > 0 {
		return EPSGCRSRef(code)
	}
	return CustomCRSRef(definition)
}

// CRSDefinitionByID returns a CRS definition by ID.
func (s *SpatialInfo) CRSDefinitionByID(id string) *CRSDefinition {
	if s == nil {
		return nil
	}
	target := strings.TrimSpace(id)
	if target == "" {
		return nil
	}
	for i := range s.CRSDefinitions {
		if strings.TrimSpace(s.CRSDefinitions[i].ID) == target {
			return &s.CRSDefinitions[i]
		}
	}
	return nil
}

// PrimaryCRSRef returns the CRS reference for the primary spatial fact.
func (s *SpatialInfo) PrimaryCRSRef() string {
	if s == nil {
		return ""
	}
	if primary := s.PrimaryGeometry(); primary != nil {
		if ref := strings.TrimSpace(primary.CRSRef); ref != "" {
			return ref
		}
		if primary.SRID != nil && *primary.SRID > 0 {
			return EPSGCRSRef(*primary.SRID)
		}
	}
	if ref := strings.TrimSpace(s.CRSRef); ref != "" {
		return ref
	}
	if s.SRID != nil && *s.SRID > 0 {
		return EPSGCRSRef(*s.SRID)
	}
	return ""
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
