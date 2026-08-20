package parquet

import (
	"encoding/json"
	"fmt"
	"math"
	"reflect"
	"strconv"
	"strings"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	parquetgo "github.com/parquet-go/parquet-go"
)

const (
	geoParquetMetadataKey  = "geo"
	geoParquetWriteVersion = "1.1.0"
)

type geoParquetMetadata struct {
	Version       string                              `json:"version"`
	PrimaryColumn string                              `json:"primary_column"`
	Columns       map[string]geoParquetColumnMetadata `json:"columns"`
}

type geoParquetColumnMetadata struct {
	Encoding      string          `json:"encoding"`
	GeometryTypes []string        `json:"geometry_types"`
	CRS           json.RawMessage `json:"crs,omitempty"`
	BBox          []float64       `json:"bbox,omitempty"`
}

type geoParquetInfo struct {
	metadata   geoParquetMetadata
	formatInfo map[string]interface{}
	spatial    *datatype.SpatialInfo
}

func geoParquetWriteMetadata(fields []datatype.FieldInfo, options *format.WriteOptions) (string, error) {
	geometryFields := make(map[string]datatype.FieldInfo)
	for _, field := range fields {
		if field.Type == datatype.FieldTypeGeometry {
			geometryFields[field.Name] = field
		}
	}
	spatial := writeSpatialInfo(options)
	if len(geometryFields) == 0 {
		if spatial != nil && len(spatial.GeometryColumns) > 0 {
			return "", fmt.Errorf("geoparquet spatial metadata requires geometry table fields")
		}
		return "", nil
	}
	if spatial == nil {
		return "", fmt.Errorf("geoparquet writer requires spatial info for geometry fields")
	}

	primary := strings.TrimSpace(spatial.PrimaryGeometryName())
	if primary == "" {
		return "", fmt.Errorf("geoparquet writer requires a primary geometry column")
	}
	if _, ok := geometryFields[primary]; !ok {
		return "", fmt.Errorf("geoparquet primary geometry column %q is not a geometry table field", primary)
	}

	columnInfo := make(map[string]datatype.GeometryColumnInfo, len(spatial.GeometryColumns))
	for _, column := range spatial.GeometryColumns {
		name := strings.TrimSpace(column.Name)
		if name == "" {
			return "", fmt.Errorf("geoparquet geometry column name is required")
		}
		if _, duplicate := columnInfo[name]; duplicate {
			return "", fmt.Errorf("duplicate geoparquet geometry column %q", name)
		}
		if _, ok := geometryFields[name]; !ok {
			return "", fmt.Errorf("geoparquet spatial column %q is not a geometry table field", name)
		}
		column.Name = name
		columnInfo[name] = column
	}
	for name := range geometryFields {
		if _, ok := columnInfo[name]; !ok {
			return "", fmt.Errorf("geoparquet geometry field %q is missing from spatial info", name)
		}
	}

	metadata := geoParquetMetadata{
		Version:       geoParquetWriteVersion,
		PrimaryColumn: primary,
		Columns:       make(map[string]geoParquetColumnMetadata, len(columnInfo)),
	}
	for _, field := range fields {
		column, ok := columnInfo[field.Name]
		if !ok {
			continue
		}
		geometryTypes, err := geoParquetWriteGeometryTypes(column)
		if err != nil {
			return "", fmt.Errorf("geoparquet geometry column %q: %w", field.Name, err)
		}
		crs, err := geoParquetWriteCRS(field.Name, column, spatial, field.Name == primary)
		if err != nil {
			return "", err
		}
		columnMetadata := geoParquetColumnMetadata{
			Encoding:      "WKB",
			GeometryTypes: geometryTypes,
			CRS:           crs,
		}
		if field.Name == primary && spatial.Extent != nil {
			bbox := *spatial.Extent
			if err := validateGeoParquetWriteBBox(bbox); err != nil {
				return "", err
			}
			columnMetadata.BBox = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
		}
		metadata.Columns[field.Name] = columnMetadata
	}

	encoded, err := json.Marshal(metadata)
	if err != nil {
		return "", fmt.Errorf("marshal geoparquet metadata: %w", err)
	}
	return string(encoded), nil
}

func writeSpatialInfo(options *format.WriteOptions) *datatype.SpatialInfo {
	if options == nil {
		return nil
	}
	return options.SpatialInfo
}

func geoParquetWriteGeometryTypes(column datatype.GeometryColumnInfo) ([]string, error) {
	geometryType := datatype.ParseGeometryType(column.GeometryType)
	if geometryType == datatype.GeometryTypeUnknown || geometryType == datatype.GeometryTypeGeometry || column.Dimension == nil {
		return []string{}, nil
	}
	switch *column.Dimension {
	case 0:
		return []string{}, nil
	case 2:
	case 3:
		return []string{string(geometryType) + " Z"}, nil
	default:
		return nil, fmt.Errorf("unsupported coordinate dimension %d", *column.Dimension)
	}
	return []string{string(geometryType)}, nil
}

func geoParquetWriteColumns(options *format.WriteOptions) map[string]datatype.GeometryColumnInfo {
	spatial := writeSpatialInfo(options)
	if spatial == nil {
		return nil
	}
	result := make(map[string]datatype.GeometryColumnInfo, len(spatial.GeometryColumns))
	for _, column := range spatial.GeometryColumns {
		result[column.Name] = column
	}
	return result
}

func geoParquetWriteCRS(columnName string, column datatype.GeometryColumnInfo, spatial *datatype.SpatialInfo, primary bool) (json.RawMessage, error) {
	crsRef, srid, err := geoParquetColumnCRSIdentity(column, spatial, primary)
	if err != nil {
		return nil, fmt.Errorf("geoparquet geometry column %q: %w", columnName, err)
	}

	if strings.EqualFold(crsRef, "OGC:CRS84") {
		return nil, nil
	}
	if crsRef == "" && (srid == nil || *srid <= 0) {
		return json.RawMessage("null"), nil
	}
	definition := geoParquetPROJJSONDefinition(spatial, crsRef)
	if definition == nil && strings.EqualFold(crsRef, "EPSG:4326") {
		return nil, nil
	}
	if definition == nil {
		return nil, fmt.Errorf("geoparquet geometry column %q with CRS %q requires a projjson CRS definition in spatial info", columnName, crsRef)
	}
	encoded := []byte(strings.TrimSpace(definition.Definition))
	var projJSON map[string]interface{}
	if err := json.Unmarshal(encoded, &projJSON); err != nil || len(projJSON) == 0 {
		return nil, fmt.Errorf("geoparquet PROJJSON for geometry column %q must be a non-empty JSON object", columnName)
	}
	projJSONType, ok := projJSON["type"].(string)
	if !ok || strings.TrimSpace(projJSONType) == "" {
		return nil, fmt.Errorf("geoparquet PROJJSON for geometry column %q requires type", columnName)
	}
	encoded, err = json.Marshal(projJSON)
	if err != nil {
		return nil, fmt.Errorf("canonicalize geoparquet PROJJSON for geometry column %q: %w", columnName, err)
	}
	parsedRef, _, err := geoParquetCRS(encoded)
	if err != nil {
		return nil, fmt.Errorf("invalid geoparquet PROJJSON for geometry column %q: %w", columnName, err)
	}
	if parsedRef == "" {
		parsedRef = datatype.CustomCRSRef(string(encoded))
	}
	if !strings.EqualFold(parsedRef, crsRef) {
		return nil, fmt.Errorf("geoparquet PROJJSON id %q for geometry column %q does not match CRS %q", parsedRef, columnName, crsRef)
	}
	return json.RawMessage(encoded), nil
}

func geoParquetColumnCRSIdentity(column datatype.GeometryColumnInfo, spatial *datatype.SpatialInfo, primary bool) (string, *int, error) {
	crsRef := strings.TrimSpace(column.CRSRef)
	srid := cloneIntPointer(column.SRID)
	if primary {
		if crsRef == "" {
			crsRef = strings.TrimSpace(spatial.CRSRef)
		}
		if srid == nil {
			srid = cloneIntPointer(spatial.SRID)
		}
	}
	if crsRef == "" && srid != nil && *srid > 0 {
		crsRef = "EPSG:" + strconv.Itoa(*srid)
	}
	if err := validateGeoParquetCRSIdentity(crsRef, srid); err != nil {
		return "", nil, err
	}
	return crsRef, srid, nil
}

func geoParquetCRSDefinitionWriteRequirements(spatial *datatype.SpatialInfo) ([]format.CRSDefinitionWriteRequirement, error) {
	if spatial == nil {
		return nil, nil
	}
	requirements := make([]format.CRSDefinitionWriteRequirement, 0)
	seen := map[string]bool{}
	for _, column := range spatial.GeometryColumns {
		columnName := strings.TrimSpace(column.Name)
		crsRef, srid, err := geoParquetColumnCRSIdentity(column, spatial, columnName == strings.TrimSpace(spatial.PrimaryGeometryName()))
		if err != nil {
			return nil, fmt.Errorf("geoparquet geometry column %q: %w", columnName, err)
		}
		if crsRef == "" && (srid == nil || *srid <= 0) {
			continue
		}
		if strings.EqualFold(crsRef, "OGC:CRS84") || strings.EqualFold(crsRef, "EPSG:4326") || geoParquetPROJJSONDefinition(spatial, crsRef) != nil {
			continue
		}
		key := strings.ToUpper(strings.TrimSpace(crsRef))
		if seen[key] {
			continue
		}
		seen[key] = true
		requirements = append(requirements, format.CRSDefinitionWriteRequirement{
			CRSRef:             crsRef,
			DefinitionEncoding: datatype.CRSDefinitionEncodingPROJJSON,
		})
	}
	return requirements, nil
}

func geoParquetPROJJSONDefinition(spatial *datatype.SpatialInfo, crsRef string) *datatype.CRSDefinition {
	if spatial == nil {
		return nil
	}
	for i := range spatial.CRSDefinitions {
		definition := &spatial.CRSDefinitions[i]
		if strings.EqualFold(strings.TrimSpace(definition.ID), strings.TrimSpace(crsRef)) &&
			strings.EqualFold(strings.TrimSpace(definition.DefinitionEncoding), datatype.CRSDefinitionEncodingPROJJSON) {
			return definition
		}
	}
	return nil
}

func validateGeoParquetCRSIdentity(crsRef string, srid *int) error {
	crsRef = strings.TrimSpace(crsRef)
	if srid == nil || *srid <= 0 || crsRef == "" {
		return nil
	}
	authority, code, ok := strings.Cut(crsRef, ":")
	if ok && strings.EqualFold(strings.TrimSpace(authority), "EPSG") && strings.TrimSpace(code) != strconv.Itoa(*srid) {
		return fmt.Errorf("CRS ref %q conflicts with SRID %d", crsRef, *srid)
	}
	return nil
}

func validateGeoParquetWriteBBox(bbox datatype.BoundingBox) error {
	for _, value := range bbox {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return fmt.Errorf("geoparquet primary bbox must contain finite numbers")
		}
	}
	if bbox[0] > bbox[2] || bbox[1] > bbox[3] {
		return fmt.Errorf("geoparquet primary bbox min values must not exceed max values")
	}
	return nil
}

func parseGeoParquetFile(file *parquetgo.File, fields []datatype.FieldInfo) (*geoParquetInfo, error) {
	if file == nil {
		return nil, nil
	}
	raw, ok := file.Lookup(geoParquetMetadataKey)
	if !ok {
		return nil, nil
	}
	if strings.TrimSpace(raw) == "" {
		return nil, format.NewDefinitiveParseError(fmt.Errorf("geoparquet metadata %q must not be empty", geoParquetMetadataKey))
	}

	var metadata geoParquetMetadata
	if err := json.Unmarshal([]byte(raw), &metadata); err != nil {
		return nil, format.NewDefinitiveParseError(fmt.Errorf("invalid geoparquet metadata: %w", err))
	}
	var formatInfo map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &formatInfo); err != nil {
		return nil, format.NewDefinitiveParseError(fmt.Errorf("invalid geoparquet metadata object: %w", err))
	}
	if err := validateGeoParquetMetadata(metadata, formatInfo, file.Schema(), fields); err != nil {
		return nil, format.NewDefinitiveParseError(err)
	}
	spatial, err := geoParquetSpatialInfo(metadata, fields)
	if err != nil {
		return nil, format.NewDefinitiveParseError(err)
	}
	return &geoParquetInfo{metadata: metadata, formatInfo: formatInfo, spatial: spatial}, nil
}

func validateGeoParquetMetadata(metadata geoParquetMetadata, raw map[string]interface{}, schema *parquetgo.Schema, fields []datatype.FieldInfo) error {
	if !supportedGeoParquetVersion(metadata.Version) {
		return fmt.Errorf("unsupported geoparquet version %q: supported versions are 1.0.x and 1.1.x", metadata.Version)
	}
	if strings.TrimSpace(metadata.PrimaryColumn) == "" {
		return fmt.Errorf("invalid geoparquet metadata: primary_column is required")
	}
	if len(metadata.Columns) == 0 {
		return fmt.Errorf("invalid geoparquet metadata: columns is required")
	}
	if _, ok := metadata.Columns[metadata.PrimaryColumn]; !ok {
		return fmt.Errorf("invalid geoparquet metadata: primary_column %q is not declared in columns", metadata.PrimaryColumn)
	}

	fieldByName := make(map[string]datatype.FieldInfo, len(fields))
	for _, field := range fields {
		fieldByName[field.Name] = field
	}
	for name, column := range metadata.Columns {
		field, ok := fieldByName[name]
		if !ok {
			return fmt.Errorf("invalid geoparquet metadata: geometry column %q does not exist in parquet schema", name)
		}
		if strings.TrimSpace(column.Encoding) == "" {
			return fmt.Errorf("invalid geoparquet metadata: encoding is required for geometry column %q", name)
		}
		if column.Encoding != "WKB" {
			return fmt.Errorf("unsupported geoparquet encoding %q for geometry column %q: only WKB is supported", column.Encoding, name)
		}
		schemaField, schemaFieldExists := geoParquetSchemaField(schema, name)
		if !schemaFieldExists || schemaField.Type() == nil || schemaField.Type().Kind() != parquetgo.ByteArray || (field.Type != datatype.FieldTypeString && field.Type != datatype.FieldTypeBytes) {
			return fmt.Errorf("invalid geoparquet metadata: WKB geometry column %q must use parquet BYTE_ARRAY", name)
		}
		if err := validateGeoParquetGeometryTypes(name, column.GeometryTypes); err != nil {
			return err
		}
		if !geoParquetGeometryTypesPresent(raw, name) {
			return fmt.Errorf("invalid geoparquet metadata: geometry_types is required for geometry column %q", name)
		}
		if len(column.BBox) != 0 && len(column.BBox) != 4 && len(column.BBox) != 6 {
			return fmt.Errorf("invalid geoparquet metadata: bbox for geometry column %q must contain 4 or 6 numbers", name)
		}
	}
	return nil
}

func geoParquetGeometryTypesPresent(raw map[string]interface{}, name string) bool {
	columns, ok := raw["columns"].(map[string]interface{})
	if !ok {
		return false
	}
	column, ok := columns[name].(map[string]interface{})
	if !ok {
		return false
	}
	value, exists := column["geometry_types"]
	if !exists || value == nil {
		return false
	}
	_, ok = value.([]interface{})
	return ok
}

func geoParquetSchemaField(schema *parquetgo.Schema, name string) (parquetgo.Field, bool) {
	if schema == nil {
		return nil, false
	}
	for _, field := range schema.Fields() {
		if field.Name() == name {
			return field, true
		}
	}
	return nil, false
}

func supportedGeoParquetVersion(version string) bool {
	version = strings.TrimSpace(version)
	return strings.HasPrefix(version, "1.0.") || strings.HasPrefix(version, "1.1.")
}

func validateGeoParquetGeometryTypes(column string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || datatype.ParseGeometryType(value) == datatype.GeometryTypeUnknown {
			return fmt.Errorf("invalid geoparquet metadata: unknown geometry type %q for column %q", value, column)
		}
		if strings.HasSuffix(strings.ToUpper(value), " M") || strings.HasSuffix(strings.ToUpper(value), " ZM") {
			return fmt.Errorf("unsupported geoparquet geometry type %q for column %q: measured coordinates are not supported", value, column)
		}
		if seen[value] {
			return fmt.Errorf("invalid geoparquet metadata: duplicate geometry type %q for column %q", value, column)
		}
		seen[value] = true
	}
	return nil
}

func geoParquetSpatialInfo(metadata geoParquetMetadata, fields []datatype.FieldInfo) (*datatype.SpatialInfo, error) {
	spatial := &datatype.SpatialInfo{PrimaryGeometryColumn: metadata.PrimaryColumn}
	definitionIDs := map[string]bool{}
	for _, field := range fields {
		columnMetadata, ok := metadata.Columns[field.Name]
		if !ok {
			continue
		}
		geometryType, dimension := geoParquetGeometryType(columnMetadata.GeometryTypes)
		crsRef, srid, err := geoParquetCRS(columnMetadata.CRS)
		if err != nil {
			return nil, fmt.Errorf("invalid geoparquet CRS for geometry column %q: %w", field.Name, err)
		}
		definition, normalizedCRSRef, err := geoParquetCRSDefinition(columnMetadata.CRS, crsRef)
		if err != nil {
			return nil, fmt.Errorf("invalid geoparquet CRS definition for geometry column %q: %w", field.Name, err)
		}
		crsRef = normalizedCRSRef
		if definition != nil && !definitionIDs[definition.ID] {
			spatial.CRSDefinitions = append(spatial.CRSDefinitions, *definition)
			definitionIDs[definition.ID] = true
		}
		nullable := field.Nullable
		column := datatype.GeometryColumnInfo{
			Name:         field.Name,
			GeometryType: geometryType,
			SRID:         srid,
			CRSRef:       crsRef,
			Nullable:     &nullable,
		}
		if dimension > 0 {
			column.Dimension = &dimension
		}
		spatial.GeometryColumns = append(spatial.GeometryColumns, column)
		if field.Name == metadata.PrimaryColumn {
			spatial.CRSRef = crsRef
			spatial.SRID = cloneIntPointer(srid)
			spatial.Extent = geoParquetBoundingBox(columnMetadata.BBox)
		}
	}
	return spatial, nil
}

func geoParquetCRSDefinition(raw json.RawMessage, crsRef string) (*datatype.CRSDefinition, string, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" || trimmed == "null" {
		return nil, crsRef, nil
	}
	var projJSON map[string]interface{}
	if err := json.Unmarshal(raw, &projJSON); err != nil {
		return nil, "", err
	}
	canonical, err := json.Marshal(projJSON)
	if err != nil {
		return nil, "", err
	}
	if crsRef == "" {
		crsRef = datatype.CustomCRSRef(string(canonical))
	}
	if crsRef == "" {
		return nil, "", fmt.Errorf("PROJJSON CRS definition is empty")
	}
	return &datatype.CRSDefinition{
		ID:                 crsRef,
		DefinitionEncoding: datatype.CRSDefinitionEncodingPROJJSON,
		Definition:         string(canonical),
		Source:             datatype.CRSDefinitionSourceGeoParquetMetadata,
	}, crsRef, nil
}

func geoParquetGeometryType(values []string) (string, int) {
	if len(values) == 0 {
		return "", 0
	}
	var topology datatype.GeometryType
	dimension := -1
	for _, value := range values {
		currentTopology := datatype.ParseGeometryType(value)
		if topology == datatype.GeometryTypeUnknown {
			topology = currentTopology
		} else if topology != currentTopology {
			topology = datatype.GeometryTypeGeometry
		}
		currentDimension := 2
		upper := strings.ToUpper(strings.TrimSpace(value))
		if strings.HasSuffix(upper, " Z") || strings.HasSuffix(upper, " ZM") {
			currentDimension = 3
		}
		if dimension == -1 {
			dimension = currentDimension
		} else if dimension != currentDimension {
			dimension = 0
		}
	}
	return string(topology), dimension
}

func geoParquetCRS(raw json.RawMessage) (string, *int, error) {
	trimmed := strings.TrimSpace(string(raw))
	if trimmed == "" {
		return "OGC:CRS84", nil, nil
	}
	if trimmed == "null" {
		return "", nil, nil
	}
	var projJSON map[string]interface{}
	if err := json.Unmarshal(raw, &projJSON); err != nil {
		return "", nil, err
	}
	id, ok := projJSON["id"].(map[string]interface{})
	if !ok {
		return "", nil, nil
	}
	authority, _ := id["authority"].(string)
	authority = strings.TrimSpace(authority)
	code := geoParquetCRSCode(id["code"])
	if authority == "" || code == "" {
		return "", nil, nil
	}
	crsRef := strings.ToUpper(authority) + ":" + code
	if strings.EqualFold(authority, "EPSG") {
		value, err := strconv.Atoi(code)
		if err == nil && value > 0 {
			return crsRef, &value, nil
		}
	}
	return crsRef, nil, nil
}

func geoParquetCRSCode(value interface{}) string {
	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed)
	case float64:
		if typed == float64(int64(typed)) {
			return strconv.FormatInt(int64(typed), 10)
		}
	}
	return ""
}

func geoParquetBoundingBox(values []float64) *datatype.BoundingBox {
	if len(values) == 4 {
		bbox := datatype.NewBoundingBox(values[0], values[1], values[2], values[3])
		return &bbox
	}
	if len(values) == 6 {
		bbox := datatype.NewBoundingBox(values[0], values[1], values[3], values[4])
		return &bbox
	}
	return nil
}

func applyGeoParquetFieldTypes(fields []datatype.FieldInfo, info *geoParquetInfo) []datatype.FieldInfo {
	result := append([]datatype.FieldInfo(nil), fields...)
	if info == nil {
		return result
	}
	for i := range result {
		if _, ok := info.metadata.Columns[result[i].Name]; ok {
			result[i].Type = datatype.FieldTypeGeometry
		}
	}
	return result
}

func geoParquetGeometryFieldSet(info *geoParquetInfo, fields []datatype.FieldInfo) map[string]bool {
	if info == nil {
		return nil
	}
	result := make(map[string]bool)
	for _, field := range fields {
		if _, ok := info.metadata.Columns[field.Name]; ok {
			result[field.Name] = true
		}
	}
	return result
}

func geoParquetGeometrySRIDs(spatial *datatype.SpatialInfo) map[string]int {
	if spatial == nil {
		return nil
	}
	result := make(map[string]int, len(spatial.GeometryColumns))
	for _, column := range spatial.GeometryColumns {
		if column.SRID != nil && *column.SRID > 0 {
			result[column.Name] = *column.SRID
		}
	}
	return result
}

func geoParquetReadEncoding(info *geoParquetInfo, options *format.ParseOptions) (format.GeometryEncoding, error) {
	if info == nil {
		return "", nil
	}
	encoding := format.GeometryEncodingWKB
	if options != nil && options.GeometryEncoding != "" {
		encoding = options.GeometryEncoding
	}
	if encoding != format.GeometryEncodingWKB && encoding != format.GeometryEncodingEWKB && encoding != format.GeometryEncodingWKT {
		return "", fmt.Errorf("unsupported geoparquet geometry read encoding %q: supported encodings are wkb, ewkb and wkt", encoding)
	}
	return encoding, nil
}

func geoParquetFormatAttributes(info *geoParquetInfo) map[string]interface{} {
	if info == nil || len(info.formatInfo) == 0 {
		return nil
	}
	return map[string]interface{}{"geo": info.formatInfo}
}

func geoParquetFormatInfo(info *geoParquetInfo) map[string]interface{} {
	if info == nil {
		return nil
	}
	return info.formatInfo
}

func geoParquetScopeFormatAttributes(info *geoParquetInfo, spatial *datatype.SpatialInfo) map[string]interface{} {
	if info == nil || len(info.formatInfo) == 0 {
		return nil
	}
	encoded, err := json.Marshal(info.formatInfo)
	if err != nil {
		return nil
	}
	var result map[string]interface{}
	if err := json.Unmarshal(encoded, &result); err != nil {
		return nil
	}
	if spatial == nil || spatial.Extent == nil {
		return result
	}
	primary := strings.TrimSpace(fmt.Sprint(result["primary_column"]))
	columns, ok := result["columns"].(map[string]interface{})
	if !ok {
		return result
	}
	column, ok := columns[primary].(map[string]interface{})
	if !ok {
		return result
	}
	bbox := *spatial.Extent
	column["bbox"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	return result
}

func geoInfoSpatial(info *geoParquetInfo) *datatype.SpatialInfo {
	if info == nil {
		return nil
	}
	return info.spatial.Clone()
}

func sameGeoParquetSpatialSchema(left, right *datatype.SpatialInfo) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	if left.PrimaryGeometryColumn != right.PrimaryGeometryColumn || len(left.GeometryColumns) != len(right.GeometryColumns) {
		return false
	}
	for i := range left.GeometryColumns {
		l, r := left.GeometryColumns[i], right.GeometryColumns[i]
		if l.Name != r.Name || l.GeometryType != r.GeometryType || l.CRSRef != r.CRSRef || !sameIntPointer(l.SRID, r.SRID) || !sameIntPointer(l.Dimension, r.Dimension) {
			return false
		}
	}
	return true
}

func sameGeoParquetFormatSchema(left, right map[string]interface{}) bool {
	if len(left) == 0 || len(right) == 0 {
		return len(left) == 0 && len(right) == 0
	}
	if strings.TrimSpace(fmt.Sprint(left["primary_column"])) != strings.TrimSpace(fmt.Sprint(right["primary_column"])) {
		return false
	}
	leftColumns, leftOK := left["columns"].(map[string]interface{})
	rightColumns, rightOK := right["columns"].(map[string]interface{})
	if !leftOK || !rightOK || len(leftColumns) != len(rightColumns) {
		return false
	}
	for name, leftRaw := range leftColumns {
		leftColumn, leftOK := leftRaw.(map[string]interface{})
		rightColumn, rightOK := rightColumns[name].(map[string]interface{})
		if !leftOK || !rightOK {
			return false
		}
		if !sameGeoParquetGeometryTypeSet(leftColumn["geometry_types"], rightColumn["geometry_types"]) {
			return false
		}
		for _, key := range []string{"encoding", "crs", "orientation", "edges"} {
			leftValue, leftExists := leftColumn[key]
			rightValue, rightExists := rightColumn[key]
			if leftExists != rightExists || !reflect.DeepEqual(leftValue, rightValue) {
				return false
			}
		}
	}
	return true
}

func sameGeoParquetGeometryTypeSet(left, right interface{}) bool {
	leftValues, leftOK := left.([]interface{})
	rightValues, rightOK := right.([]interface{})
	if !leftOK || !rightOK || len(leftValues) != len(rightValues) {
		return false
	}
	seen := make(map[string]bool, len(leftValues))
	for _, value := range leftValues {
		seen[strings.TrimSpace(fmt.Sprint(value))] = true
	}
	for _, value := range rightValues {
		if !seen[strings.TrimSpace(fmt.Sprint(value))] {
			return false
		}
	}
	return true
}

func mergeGeoParquetExtent(target *datatype.SpatialInfo, source *datatype.SpatialInfo) {
	if target == nil || source == nil || source.Extent == nil {
		return
	}
	if target.Extent == nil {
		bbox := *source.Extent
		target.Extent = &bbox
		return
	}
	if source.Extent[0] < target.Extent[0] {
		target.Extent[0] = source.Extent[0]
	}
	if source.Extent[1] < target.Extent[1] {
		target.Extent[1] = source.Extent[1]
	}
	if source.Extent[2] > target.Extent[2] {
		target.Extent[2] = source.Extent[2]
	}
	if source.Extent[3] > target.Extent[3] {
		target.Extent[3] = source.Extent[3]
	}
}

func sameIntPointer(left, right *int) bool {
	if left == nil || right == nil {
		return left == nil && right == nil
	}
	return *left == *right
}

func cloneIntPointer(value *int) *int {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
