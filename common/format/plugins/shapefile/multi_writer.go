package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
)

var _ format.MultiTableWriterProvider = (*Plugin)(nil)

func (plugin *Plugin) OpenMultiTableWriter(ctx context.Context, output contentio.Writer, refs []format.RelatedRef, schema *format.TableInfo, options *format.WriteOptions) (format.TableWriter, error) {
	if output == nil {
		return nil, fmt.Errorf("ref writer cannot be nil")
	}
	if schema == nil {
		return nil, fmt.Errorf("shapefile table writer requires schema")
	}
	if len(refs) == 0 {
		return nil, fmt.Errorf("shapefile table writer requires related refs")
	}
	primaryRef, err := format.PrimaryRelatedRef(refs)
	if err != nil {
		return nil, fmt.Errorf("shapefile table writer requires valid related refs: %w", err)
	}

	opts := format.DefaultWriteOptions()
	if options != nil {
		*opts = *options
	}
	geometryField := multiWriterGeometryField(schema, opts)
	if geometryField == "" {
		return nil, fmt.Errorf("shapefile table writer requires geometry field")
	}
	shapeType, err := shapeTypeFromSchema(schema)
	if err != nil {
		return nil, err
	}

	tempDir, err := os.MkdirTemp("", "addp-shapefile-write-*")
	if err != nil {
		return nil, fmt.Errorf("create shapefile temp dir: %w", err)
	}
	baseName := contentio.BaseName(primaryRef.Ref)
	baseName = strings.TrimSuffix(baseName, filepath.Ext(baseName))
	if baseName == "" || baseName == "." {
		baseName = "data"
	}
	basePath := filepath.Join(tempDir, baseName)

	writer, err := Create(basePath+".shp", shapeType)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("create shapefile writer: %w", err)
	}
	dbfSchema := shapefileDBFSchema(schema, geometryField)
	if err := writer.SetFields(dbfSchema.fields); err != nil {
		writer.Close()
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("set shapefile fields: %w", err)
	}

	return &multiTableWriter{
		output:        output,
		tempDir:       tempDir,
		basePath:      basePath,
		writer:        writer,
		refs:          append([]format.RelatedRef(nil), refs...),
		geometryField: geometryField,
		fieldNames:    dbfSchema.originalNames,
		fields:        dbfSchema.fields,
		options:       opts,
	}, nil
}

type multiTableWriter struct {
	output        contentio.Writer
	refs          []format.RelatedRef
	tempDir       string
	basePath      string
	writer        *Writer
	geometryField string
	fieldNames    []string
	fields        []shp.Field
	options       *format.WriteOptions
	closed        bool
}

func (w *multiTableWriter) WriteRows(ctx context.Context, rows []map[string]interface{}) error {
	if w == nil || w.writer == nil {
		return fmt.Errorf("shapefile table writer is closed")
	}
	for _, row := range rows {
		if err := w.writeRow(row); err != nil {
			return err
		}
	}
	return nil
}

func (w *multiTableWriter) writeRow(row map[string]interface{}) error {
	geomValue, exists := findGeometryValue(row, w.geometryField)
	if !exists {
		return fmt.Errorf("geometry field '%s' not found", w.geometryField)
	}
	shape, err := ToShapefileGeometry(geomValue)
	if err != nil {
		return fmt.Errorf("failed to convert geometry: %w", err)
	}
	w.writer.Writer.Write(shape)
	for i, fieldName := range w.fieldNames {
		value, _ := valueByName(row, fieldName)
		value = normalizeDBFValue(value, w.fields[i])
		if err := w.writer.Writer.WriteAttribute(w.writer.recordCount, i, value); err != nil {
			return err
		}
	}
	w.writer.recordCount++
	return nil
}

func normalizeDBFValue(value interface{}, field shp.Field) interface{} {
	if value == nil {
		return strings.Repeat(" ", int(field.Size))
	}
	switch field.Fieldtype {
	case 'C':
		text := fmt.Sprint(value)
		if len(text) > int(field.Size) {
			return text[:field.Size]
		}
		return text + strings.Repeat(" ", int(field.Size)-len(text))
	case 'N':
		if parsed, ok := intDBFValue(value); ok {
			return fitDBFText(fmt.Sprintf("%*d", int(field.Size), parsed), int(field.Size))
		}
	case 'F':
		if parsed, ok := floatDBFValue(value); ok {
			text := strconv.FormatFloat(parsed, 'f', int(field.Precision), 64)
			return fitDBFText(fmt.Sprintf("%*s", int(field.Size), text), int(field.Size))
		}
	case 'D':
		switch v := value.(type) {
		case time.Time:
			return v.Format("20060102")
		case string:
			text := strings.TrimSpace(v)
			if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
				return strings.ReplaceAll(text[:10], "-", "")
			}
			return fitDBFText(text, int(field.Size))
		}
	case 'L':
		switch v := value.(type) {
		case bool:
			if v {
				return "T"
			}
			return "F"
		}
	}
	return value
}

func fitDBFText(text string, size int) string {
	if size <= 0 {
		return text
	}
	if len(text) > size {
		return text[:size]
	}
	return strings.Repeat(" ", size-len(text)) + text
}

func intDBFValue(value interface{}) (int, bool) {
	switch v := value.(type) {
	case int:
		return v, true
	case int8:
		return int(v), true
	case int16:
		return int(v), true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case uint:
		return int(v), true
	case uint8:
		return int(v), true
	case uint16:
		return int(v), true
	case uint32:
		return int(v), true
	case uint64:
		return int(v), true
	case float32:
		return int(v), true
	case float64:
		return int(v), true
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(v))
		return parsed, err == nil
	default:
		return 0, false
	}
}

func floatDBFValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case float64:
		return v, true
	case float32:
		return float64(v), true
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		return parsed, err == nil
	default:
		return 0, false
	}
}

func (w *multiTableWriter) Close(ctx context.Context) error {
	if w == nil || w.closed {
		return nil
	}
	w.closed = true
	defer os.RemoveAll(w.tempDir)

	if w.writer != nil {
		w.writer.Close()
		w.writer = nil
	}
	if err := w.writeSidecarFiles(); err != nil {
		return err
	}
	for _, ref := range w.refs {
		if !ref.Required && !fileExists(refPath(w.basePath, ref)) {
			continue
		}
		if err := copyRef(ctx, w.output, w.basePath, ref); err != nil {
			return err
		}
	}
	return nil
}

func (w *multiTableWriter) writeSidecarFiles() error {
	if w.options == nil {
		return nil
	}
	encoding := NormalizeDBFEncoding(w.options.Encoding)
	if encoding != "" {
		if err := os.WriteFile(w.basePath+".cpg", []byte(encoding), 0o644); err != nil {
			return fmt.Errorf("write shapefile cpg: %w", err)
		}
	}
	if prj, ok := stringWriteOption(w.options, "spatial_ref_sys"); ok && strings.TrimSpace(prj) != "" {
		if err := os.WriteFile(w.basePath+".prj", []byte(strings.TrimSpace(prj)), 0o644); err != nil {
			return fmt.Errorf("write shapefile prj: %w", err)
		}
	}
	return nil
}

func copyRef(ctx context.Context, output contentio.Writer, basePath string, ref format.RelatedRef) error {
	sourcePath := refPath(basePath, ref)
	source, err := os.Open(sourcePath)
	if err != nil {
		return fmt.Errorf("open shapefile ref %s: %w", ref.Ref.Role, err)
	}
	defer source.Close()

	target, err := output.Create(ctx, ref.Ref)
	if err != nil {
		return fmt.Errorf("create shapefile ref %s: %w", ref.Ref.Role, err)
	}
	targetClosed := false
	defer func() {
		if !targetClosed {
			_ = target.Close()
		}
	}()
	if _, err := io.Copy(target, source); err != nil {
		return fmt.Errorf("copy shapefile ref %s: %w", ref.Ref.Role, err)
	}
	if err := target.Close(); err != nil {
		return fmt.Errorf("close shapefile ref %s: %w", ref.Ref.Role, err)
	}
	targetClosed = true
	return nil
}

func refPath(basePath string, ref format.RelatedRef) string {
	return basePath + filepath.Ext(ref.Ref.Path)
}

func multiWriterGeometryField(schema *format.TableInfo, opts *format.WriteOptions) string {
	if opts != nil {
		if value, ok := stringWriteOption(opts, "geometry_field"); ok {
			return strings.TrimSpace(value)
		}
	}
	if schema != nil && schema.SpatialInfo != nil && strings.TrimSpace(schema.SpatialInfo.GeometryColumn) != "" {
		return strings.TrimSpace(schema.SpatialInfo.GeometryColumn)
	}
	for _, field := range schema.Fields {
		if format.IsGeometryType(field.Type) {
			return field.Name
		}
	}
	return ""
}

func stringWriteOption(opts *format.WriteOptions, key string) (string, bool) {
	if opts == nil || opts.ExtraParams == nil {
		return "", false
	}
	value, ok := opts.ExtraParams[key]
	if !ok {
		return "", false
	}
	text, ok := value.(string)
	return text, ok
}

func shapeTypeFromSchema(schema *format.TableInfo) (shp.ShapeType, error) {
	geometryType := ""
	if schema != nil && schema.SpatialInfo != nil {
		geometryType = schema.SpatialInfo.GeometryType
	}
	if geometryType == "" && schema != nil {
		for _, field := range schema.Fields {
			if format.IsGeometryType(field.Type) {
				geometryType = string(field.Type)
				break
			}
		}
	}
	switch strings.ToLower(strings.ReplaceAll(geometryType, "_", "")) {
	case "point":
		return shp.POINT, nil
	case "linestring", "multilinestring":
		return shp.POLYLINE, nil
	case "polygon", "multipolygon":
		return shp.POLYGON, nil
	case "multipoint":
		return shp.MULTIPOINT, nil
	default:
		return shp.NULL, fmt.Errorf("unsupported or missing shapefile geometry type %q", geometryType)
	}
}

type shapefileDBFSchemaInfo struct {
	fields        []shp.Field
	originalNames []string
}

func shapefileDBFSchema(schema *format.TableInfo, geometryField string) shapefileDBFSchemaInfo {
	info := shapefileDBFSchemaInfo{
		fields:        make([]shp.Field, 0, len(schema.Fields)),
		originalNames: make([]string, 0, len(schema.Fields)),
	}
	used := map[string]int{}
	mapper := format.GetTypeMapper("shapefile")
	for _, field := range schema.Fields {
		if strings.EqualFold(field.Name, geometryField) || format.IsGeometryType(field.Type) {
			continue
		}
		dbfType, size, precision := "C", 254, 0
		if mapper != nil {
			dbfType, size, precision = mapper.FromCommon(field.Type)
		}
		if field.Size > 0 {
			size = field.Size
		}
		if field.Precision > 0 {
			precision = field.Precision
		}
		name := uniqueDBFFieldName(field.Name, used)
		info.fields = append(info.fields, dbfField(name, dbfType, size, precision))
		info.originalNames = append(info.originalNames, field.Name)
	}
	return info
}

func valueByName(row map[string]interface{}, name string) (interface{}, bool) {
	if value, ok := row[name]; ok {
		return value, true
	}
	lowerName := strings.ToLower(name)
	for key, value := range row {
		if strings.ToLower(key) == lowerName {
			return value, true
		}
	}
	return nil, false
}

func uniqueDBFFieldName(name string, used map[string]int) string {
	normalized := strings.ToUpper(strings.TrimSpace(name))
	if normalized == "" {
		normalized = "FIELD"
	}
	normalized = strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z':
			return r
		case r >= '0' && r <= '9':
			return r
		case r == '_':
			return r
		default:
			return '_'
		}
	}, normalized)
	if len(normalized) > 10 {
		normalized = normalized[:10]
	}
	count := used[normalized]
	used[normalized] = count + 1
	if count == 0 {
		return normalized
	}
	suffix := fmt.Sprintf("_%d", count+1)
	prefixLen := 10 - len(suffix)
	if prefixLen < 1 {
		prefixLen = 1
	}
	if len(normalized) > prefixLen {
		normalized = normalized[:prefixLen]
	}
	return normalized + suffix
}

func dbfField(name, dbfType string, size, precision int) shp.Field {
	if size <= 0 {
		size = 254
	}
	if size > 254 {
		size = 254
	}
	if precision < 0 {
		precision = 0
	}
	if precision > 15 {
		precision = 15
	}
	switch strings.ToUpper(dbfType) {
	case "N":
		return shp.NumberField(name, uint8(size))
	case "F":
		return shp.FloatField(name, uint8(size), uint8(precision))
	case "D":
		return shp.DateField(name)
	case "L":
		field := shp.StringField(name, 1)
		field.Fieldtype = 'L'
		return field
	default:
		return shp.StringField(name, uint8(size))
	}
}
