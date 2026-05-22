package shapefile

import (
	"context"
	"fmt"
	"github.com/addp/common/contentio"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (plugin *Plugin) DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, options *format.ParseOptions) (*datatype.TableDescribeResult, error) {
	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	opts := plugin.resolveMaterializedOptions(basePath, options)
	info, err := plugin.describeTableInfoFromHeaders(basePath, refs, opts)
	if err != nil {
		return nil, err
	}
	selected, err := format.ApplyFieldSelectionToTableInfo(info, opts.FieldSelection)
	if err != nil {
		return nil, err
	}
	return format.TableDescribeResultFromSchema(selected), nil
}

func (plugin *Plugin) SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	opts := plugin.resolveOptions(options)
	if rows, ok, err := plugin.sampleMultiTableIndexed(ctx, reader, refs, offset, limit, opts); ok {
		if err == nil {
			return format.ApplyFieldSelectionToRows(rows, opts.FieldSelection), nil
		}
		if !isIndexedSampleFallbackError(err) {
			return nil, err
		}
	}

	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	applyMaterializedSidecarOptions(basePath, opts)
	if rows, ok, err := plugin.sampleMaterializedTableIndexed(ctx, basePath, refs, offset, limit, opts); ok {
		if err == nil {
			return format.ApplyFieldSelectionToRows(rows, opts.FieldSelection), nil
		}
		if !isIndexedSampleFallbackError(err) {
			return nil, err
		}
	}
	rows, err := plugin.sampleTableFromPath(ctx, basePath+extSHP, offset, limit, opts)
	if err != nil {
		return nil, err
	}
	return format.ApplyFieldSelectionToRows(rows, opts.FieldSelection), nil
}

func (plugin *Plugin) describeTableInfoFromHeaders(basePath string, refs []format.RelatedRef, opts *format.ParseOptions) (*format.TableInfo, error) {
	encodingName := ""
	spatialRefSys := ""
	if opts != nil {
		encodingName = NormalizeDBFEncoding(opts.Encoding)
		spatialRefSys = opts.SpatialRefSys
	}

	shpHeader, err := readSHPHeader(basePath + extSHP)
	if err != nil {
		return nil, err
	}
	dbfHeader, err := readDBFHeader(basePath+extDBF, encodingName)
	if err != nil {
		return nil, err
	}

	return buildShapefileTableInfo(shapefileTableInfoInput{
		GeometryField: plugin.getGeometryFieldName(opts),
		BaseName:      filepath.Base(basePath),
		Refs:          refs,
		SHPHeader:     shpHeader,
		DBFHeader:     dbfHeader,
		Encoding:      encodingName,
		HasPRJ:        fileExists(basePath + extPRJ),
		HasCPG:        fileExists(basePath + extCPG),
		SpatialRefSys: spatialRefSys,
	}), nil
}

func (plugin *Plugin) sampleTableFromPath(ctx context.Context, shpPath string, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, error) {
	opts = plugin.resolveOptions(opts)
	encodingName := ""
	if opts != nil {
		encodingName = opts.Encoding
	}
	if encodingName == "" || NormalizeDBFEncoding(encodingName) == "utf-8" {
		if cpgEncoding := readCPGEncoding(strings.TrimSuffix(shpPath, filepath.Ext(shpPath))); cpgEncoding != "" {
			encodingName = cpgEncoding
		}
	}
	reader, err := openWithEncoding(shpPath, encodingName)
	if err != nil {
		return nil, fmt.Errorf("failed to open shapefile: %w", err)
	}
	defer reader.Close()

	geometryField := plugin.getGeometryFieldName(opts)
	currentRecord := int64(0)
	for currentRecord < offset && reader.Next() {
		currentRecord++
	}

	maxRows := limit
	if limit < 0 {
		maxRows = 1<<31 - 1
	}
	records := make([]map[string]interface{}, 0)
	readCount := int64(0)
	recordIndex := int(offset)

	for readCount < maxRows && reader.Next() {
		select {
		case <-ctx.Done():
			return records, ctx.Err()
		default:
		}

		records = append(records, readSequentialRow(reader, recordIndex, geometryField, opts, sridFromParseOptions(opts)))
		readCount++
		recordIndex++
	}
	return records, nil
}

type shapefileTableInfoInput struct {
	GeometryField string
	BaseName      string
	Refs          []format.RelatedRef
	SHPHeader     *shpHeaderInfo
	DBFHeader     *dbfHeaderInfo
	Encoding      string
	HasPRJ        bool
	HasCPG        bool
	SpatialRefSys string
}

func buildShapefileTableInfo(input shapefileTableInfoInput) *format.TableInfo {
	if input.GeometryField == "" {
		input.GeometryField = "geometry"
	}
	fields := make([]format.FieldInfo, 0, len(input.DBFHeader.Fields)+1)
	geomType := determineShapefileGeometryType(input.SHPHeader.ShapeType)
	fields = append(fields, format.FieldInfo{
		Name:         input.GeometryField,
		Type:         datatype.FieldTypeGeometry,
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	})
	for _, field := range input.DBFHeader.Fields {
		fields = append(fields, format.FieldInfo{
			Name:      field.Name,
			Type:      dbfFieldToCommonType(field),
			Nullable:  true,
			Size:      field.Size,
			Precision: field.Precision,
		})
	}

	rowCount := int64(input.DBFHeader.RecordCount)
	spatialInfo := datatype.NewSingleGeometrySpatialInfo(input.GeometryField, geomType, 0, determineShapefileDimension(input.SHPHeader.ShapeType))
	bbox := datatype.BoundingBox(input.SHPHeader.BBox)
	spatialInfo.Extent = &bbox
	if input.SpatialRefSys != "" {
		if srid := commonSpatial.ParseSRID(input.SpatialRefSys); srid > 0 {
			spatialInfo.GeometryColumns[0].SRID = &srid
		}
	}
	info := &Info{
		BaseName:      input.BaseName,
		RefExtensions: refExtensions(input.Refs),
		HasPRJ:        input.HasPRJ,
		HasCPG:        input.HasCPG,
		ShapeType:     geomType,
		DBFVersion:    input.DBFHeader.Version,
		Encoding:      NormalizeDBFEncoding(input.Encoding),
	}
	return &format.TableInfo{
		Name:        "shapefile_data",
		RowCount:    &rowCount,
		Fields:      fields,
		PrimaryKey:  []string{},
		SpatialInfo: spatialInfo,
		FormatInfo:  map[string]interface{}{"shapefile": info.FormatAttributes()},
	}
}

func materializeRefs(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef) (tempDir string, basePath string, cleanup func(), err error) {
	tempDir, err = os.MkdirTemp("", "shapefile-refs-*")
	if err != nil {
		return "", "", nil, err
	}
	cleanup = func() {
		os.RemoveAll(tempDir)
	}
	primaryRef, err := format.PrimaryRelatedRef(refs)
	if err != nil {
		cleanup()
		return "", "", nil, fmt.Errorf("invalid shapefile related refs: %w", err)
	}

	var mainLocalPath string
	for _, ref := range refs {
		localPath := filepath.Join(tempDir, filepath.Base(ref.Ref.Path))
		if err := materializeRef(ctx, reader, ref, localPath); err != nil {
			if ref.Required {
				cleanup()
				return "", "", nil, fmt.Errorf("failed to read required ref %s: %w", ref.Ref.Path, err)
			}
			continue
		}
		if ref.Ref.Path == primaryRef.Ref.Path {
			mainLocalPath = localPath
		}
	}
	if mainLocalPath == "" {
		cleanup()
		return "", "", nil, fmt.Errorf("main ref missing")
	}
	return tempDir, strings.TrimSuffix(mainLocalPath, filepath.Ext(mainLocalPath)), cleanup, nil
}

func materializeRef(ctx context.Context, reader contentio.Reader, ref format.RelatedRef, destPath string) error {
	src, err := reader.Open(ctx, ref.Ref)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(destPath)
	if err != nil {
		return err
	}
	defer dst.Close()
	_, err = io.Copy(dst, src)
	return err
}

func (plugin *Plugin) sampleMaterializedTableIndexed(ctx context.Context, basePath string, refs []format.RelatedRef, offset, limit int64, opts *format.ParseOptions) ([]map[string]interface{}, bool, error) {
	reader := materializedRefRangeReader{basePath: basePath}
	return plugin.sampleMultiTableIndexed(ctx, reader, refs, offset, limit, opts)
}

type materializedRefRangeReader struct {
	basePath string
}

func (r materializedRefRangeReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return os.Open(r.localPath(ref))
}

func (r materializedRefRangeReader) OpenRange(ctx context.Context, ref contentio.Ref, offset, length int64) (io.ReadCloser, error) {
	file, err := os.Open(r.localPath(ref))
	if err != nil {
		return nil, err
	}
	if _, err := file.Seek(offset, io.SeekStart); err != nil {
		file.Close()
		return nil, err
	}
	return struct {
		io.Reader
		io.Closer
	}{
		Reader: io.LimitReader(file, length),
		Closer: file,
	}, nil
}

func (r materializedRefRangeReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, nil
}

func (r materializedRefRangeReader) localPath(ref contentio.Ref) string {
	return r.basePath + filepath.Ext(ref.Path)
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
