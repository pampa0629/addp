package shapefile

import (
	"context"
	"encoding/binary"
	"fmt"
	"github.com/addp/common/contentio"
	"github.com/addp/common/format"
	commonSpatial "github.com/addp/common/spatial"
	"io"
	"os"
	"path/filepath"
	"strings"
)

func (plugin *Plugin) DescribeMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, options *format.ParseOptions) (*format.TableInfo, error) {
	_, basePath, cleanup, err := materializeRefs(ctx, reader, refs)
	if err != nil {
		return nil, err
	}
	defer cleanup()

	opts := plugin.resolveMaterializedOptions(basePath, options)
	return plugin.describeTableInfoFromHeaders(basePath, refs, opts)
}

func (plugin *Plugin) SampleMultiTable(ctx context.Context, reader contentio.Reader, refs []format.RelatedRef, offset, limit int64, options *format.ParseOptions) ([]map[string]interface{}, error) {
	opts := plugin.resolveOptions(options)
	if rows, ok, err := plugin.sampleMultiTableIndexed(ctx, reader, refs, offset, limit, opts); ok {
		if err == nil {
			return rows, nil
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
			return rows, nil
		}
		if !isIndexedSampleFallbackError(err) {
			return nil, err
		}
	}
	return plugin.sampleTableFromPath(ctx, basePath+extSHP, offset, limit, opts)
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
	contentIndex, err := buildShapefileContentIndexFromPath(basePath, refs, int64(dbfHeader.RecordCount), opts)
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
		ContentIndex:  contentIndex,
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
	ContentIndex  *format.ContentIndexInfo
}

func buildShapefileTableInfo(input shapefileTableInfoInput) *format.TableInfo {
	if input.GeometryField == "" {
		input.GeometryField = "geometry"
	}
	fields := make([]format.FieldInfo, 0, len(input.DBFHeader.Fields)+1)
	geomType := determineShapefileGeometryType(input.SHPHeader.ShapeType)
	fields = append(fields, format.FieldInfo{
		Name:         input.GeometryField,
		Type:         format.FieldTypeGeometry,
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
	spatialInfo := &format.SpatialInfo{
		GeometryColumn: input.GeometryField,
		GeometryType:   geomType,
		SRID:           0,
		BoundingBox:    &input.SHPHeader.BBox,
		Dimension:      determineShapefileDimension(input.SHPHeader.ShapeType),
	}
	if input.SpatialRefSys != "" {
		if srid := commonSpatial.ParseSRID(input.SpatialRefSys); srid > 0 {
			spatialInfo.SRID = srid
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
		Name:         "shapefile_data",
		RowCount:     &rowCount,
		Fields:       fields,
		PrimaryKey:   []string{},
		SpatialInfo:  spatialInfo,
		ContentIndex: input.ContentIndex,
		FormatInfo:   map[string]interface{}{"shapefile": info.FormatAttributes()},
	}
}

func buildShapefileContentIndexFromPath(basePath string, refs []format.RelatedRef, rowCount int64, opts *format.ParseOptions) (*format.ContentIndexInfo, error) {
	return buildShapefileContentIndexFromSHX(basePath+extSHX, refs, rowCount, opts)
}

func buildShapefileContentIndexFromSHX(shxPath string, refs []format.RelatedRef, rowCount int64, opts *format.ParseOptions) (*format.ContentIndexInfo, error) {
	if rowCount <= 0 {
		return nil, nil
	}
	step := int64(5000)
	if opts != nil && opts.ContentIndexStep > 0 {
		step = opts.ContentIndexStep
	}
	if step <= 0 {
		step = 5000
	}
	anchors, err := readShapefileContentIndexAnchors(shxPath, rowCount, step)
	if err != nil {
		return nil, err
	}
	return &format.ContentIndexInfo{
		Table: &format.ContentIndex{
			Kind:       format.ContentIndexKindSparseRow,
			DataType:   format.ContentIndexDataTypeTable,
			Format:     string(format.FormatShapefile),
			Unit:       format.ContentIndexUnitRow,
			OffsetUnit: format.ContentIndexOffsetByte,
			Step:       step,
			RowCount:   rowCount,
			Source: map[string]interface{}{
				"index_format": "shx",
				"refs":         relatedRefAttributes(refs),
				"ref_count":    len(refs),
			},
			Anchors: anchors,
		},
	}, nil
}

func readShapefileContentIndexAnchors(shxPath string, rowCount, step int64) ([]format.ContentIndexAnchor, error) {
	file, err := os.Open(shxPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	anchors := make([]format.ContentIndexAnchor, 0, (rowCount+step-1)/step)
	for row := int64(0); row < rowCount; row += step {
		offsetBytes, err := readShapefileSHXOffsetAt(file, row)
		if err != nil {
			return nil, err
		}
		anchors = append(anchors, format.ContentIndexAnchor{
			Row:        row,
			ByteOffset: offsetBytes,
		})
	}
	return anchors, nil
}

func readShapefileSHXOffsetAt(file io.ReaderAt, row int64) (int64, error) {
	if row < 0 {
		return 0, fmt.Errorf("invalid shx row %d", row)
	}
	buf := make([]byte, 8)
	_, err := file.ReadAt(buf, 100+row*8)
	if err != nil {
		return 0, err
	}
	offsetWords := int64(binary.BigEndian.Uint32(buf[0:4]))
	return offsetWords * 2, nil
}

func relatedRefAttributes(refs []format.RelatedRef) []map[string]interface{} {
	if len(refs) == 0 {
		return nil
	}
	items := make([]map[string]interface{}, 0, len(refs))
	for _, ref := range refs {
		if strings.TrimSpace(ref.Ref.Path) == "" {
			continue
		}
		item := map[string]interface{}{
			"path": ref.Ref.Path,
		}
		if ref.Ref.Role != "" {
			item["role"] = ref.Ref.Role
		}
		if ref.Required {
			item["required"] = true
		}
		if ref.Primary {
			item["primary"] = true
		}
		items = append(items, item)
	}
	return items
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
