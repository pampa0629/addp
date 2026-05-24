package shapefile

import (
	"context"
	"fmt"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/jonas-p/go-shp"
	"io"
	"os"
	"path/filepath"
	"strings"
)

type writer struct {
	*shp.Writer
	filePath  string
	shapeType shp.ShapeType
}

func create(filename string, shapeType shp.ShapeType) (*writer, error) {
	shpWriter, err := shp.Create(filename, shapeType)
	if err != nil {
		return nil, err
	}

	return &writer{
		Writer:    shpWriter,
		filePath:  filename,
		shapeType: shapeType,
	}, nil
}

func (w *writer) setFields(fields []shp.Field) error {
	if err := w.Writer.SetFields(fields); err != nil {
		return err
	}

	w.normalizeDBFFilePath()

	return nil
}

func (w *writer) normalizeDBFFilePath() {
	basePath := w.filePath
	if strings.HasSuffix(strings.ToLower(basePath), extSHP) {
		basePath = basePath[:len(basePath)-len(extSHP)]
	}

	undottedDBFPath := basePath + "dbf"
	dottedDBFPath := basePath + extDBF

	if _, err := os.Stat(undottedDBFPath); err != nil {
		return
	}
	_ = os.Rename(undottedDBFPath, dottedDBFPath)
}

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
	shapeType, err := shapeTypeFromSchema(schema, opts.SpatialInfo)
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

	writer, err := create(basePath+extSHP, shapeType)
	if err != nil {
		_ = os.RemoveAll(tempDir)
		return nil, fmt.Errorf("create shapefile writer: %w", err)
	}
	dbfSchema := shapefileDBFSchema(schema, geometryField)
	if err := writer.setFields(dbfSchema.fields); err != nil {
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
	writer        *writer
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
	shape, err := toShapefileGeometry(geomValue, w.writer.shapeType)
	if err != nil {
		return fmt.Errorf("failed to convert geometry: %w", err)
	}
	recordIndex := int(w.writer.Writer.Write(shape))
	for i, fieldName := range w.fieldNames {
		value, _ := valueByName(row, fieldName)
		value = normalizeDBFValue(value, w.fields[i])
		if err := w.writer.Writer.WriteAttribute(recordIndex, i, value); err != nil {
			return err
		}
	}
	return nil
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

func multiWriterGeometryField(schema *format.TableInfo, opts *format.WriteOptions) string {
	if opts != nil {
		if value, ok := stringWriteOption(opts, "geometry_field"); ok {
			return strings.TrimSpace(value)
		}
	}
	if opts != nil && opts.SpatialInfo != nil && strings.TrimSpace(opts.SpatialInfo.PrimaryGeometryName()) != "" {
		return strings.TrimSpace(opts.SpatialInfo.PrimaryGeometryName())
	}
	for _, field := range schema.Fields {
		if datatype.IsSpatialFieldType(field.Type) {
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

func (w *multiTableWriter) writeSidecarFiles() error {
	if w.options == nil {
		return nil
	}
	encoding := NormalizeDBFEncoding(w.options.Encoding)
	if encoding != "" {
		if err := os.WriteFile(w.basePath+extCPG, []byte(encoding), 0o644); err != nil {
			return fmt.Errorf("write shapefile cpg: %w", err)
		}
	}
	if prj, ok := stringWriteOption(w.options, "spatial_ref_sys"); ok && strings.TrimSpace(prj) != "" {
		if err := os.WriteFile(w.basePath+extPRJ, []byte(strings.TrimSpace(prj)), 0o644); err != nil {
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
