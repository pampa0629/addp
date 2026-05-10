package metaitem

import (
	"context"
	"database/sql"
	"fmt"
	"io"
	"math"
	"os"
	"strings"

	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/common/format"
	"github.com/addp/common/format/codecs/excel"
	commonSQLite "github.com/addp/common/format/codecs/sqlite"
	spatialiteMapper "github.com/addp/common/format/mappers/spatialite"
	"github.com/addp/meta/internal/metaattr"
	"github.com/addp/meta/internal/models"
	_ "github.com/mattn/go-sqlite3"
	"github.com/xuri/excelize/v2"
)

const (
	containerChildLimit       = 100
	containerSampleChildLimit = 100
)

// EnrichContainerChildren 枚举容器内部对象，并写入 type_info.container.children。
func EnrichContainerChildren(ctx context.Context, attrs models.JSONMap, detected *DetectedItem, reader io.Reader) error {
	if attrs == nil || detected == nil || reader == nil || detected.DataType != dataitem.DataTypeContainer {
		return nil
	}
	switch detected.Format {
	case string(format.FormatExcel):
		return enrichExcelContainerChildren(ctx, attrs, reader)
	case string(format.FormatSQLite):
		return enrichSQLiteContainerChildren(ctx, attrs, reader, false)
	case string(format.FormatGeoPackage):
		return enrichSQLiteContainerChildren(ctx, attrs, reader, true)
	default:
		return nil
	}
}

func enrichExcelContainerChildren(ctx context.Context, attrs models.JSONMap, reader io.Reader) error {
	workbook, err := excelize.OpenReader(reader)
	if err != nil {
		return fmt.Errorf("open excel container: %w", err)
	}
	defer workbook.Close()

	opts := excel.DefaultOptions()
	opts.SheetLimit = containerChildLimit
	opts.RowLimit = 20
	analysis, err := excel.Analyze(ctx, workbook, &opts)
	if err != nil {
		return err
	}

	children := make([]map[string]interface{}, 0, len(analysis.Sheets))
	for _, sheet := range analysis.Sheets {
		children = append(children, map[string]interface{}{
			"name":         sheet.Name,
			"kind":         "sheet",
			"data_type":    string(dataitem.DataTypeTable),
			"row_count":    sheet.RowCount,
			"column_count": sheet.ColumnCount,
			"has_header":   sheet.HasHeader,
			"fields":       excelSheetFieldAttributes(sheet),
		})
	}
	metaattr.UpsertNested(attrs, "type_info", "container", map[string]interface{}{
		"children":       children,
		"child_count":    analysis.SheetCount,
		"default_child":  analysis.DefaultSheet,
		"resource_count": 1,
	})
	metaattr.UpsertNested(attrs, "format_info", "excel", map[string]interface{}{
		"sheet_count":        analysis.SheetCount,
		"default_sheet":      analysis.DefaultSheet,
		"sampled_sheets":     len(children),
		"children_truncated": analysis.SheetCount > len(children),
	})
	return nil
}

func excelSheetFieldAttributes(sheet excel.SheetSummary) []map[string]interface{} {
	fields := make([]format.FieldInfo, 0, len(sheet.Headers))
	for i, header := range sheet.Headers {
		fieldType := format.FieldTypeString
		originalType := ""
		if i < len(sheet.ColumnTypes) {
			originalType = sheet.ColumnTypes[i]
			fieldType = excelColumnType(originalType)
		}
		fields = append(fields, format.FieldInfo{
			Name:         header,
			Type:         fieldType,
			OriginalType: originalType,
			Nullable:     true,
		})
	}
	return metaattr.FieldAttributesFromFormat(fields)
}

func excelColumnType(value string) format.FieldType {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "integer":
		return format.FieldTypeInt
	case "float":
		return format.FieldTypeDouble
	case "boolean":
		return format.FieldTypeBool
	case "date":
		return format.FieldTypeTimestamp
	default:
		return format.FieldTypeString
	}
}

func enrichSQLiteContainerChildren(ctx context.Context, attrs models.JSONMap, reader io.Reader, geoPackage bool) error {
	tmp, cleanup, err := saveContainerReaderToTemp(reader, "container-sqlite-*.db")
	if err != nil {
		return err
	}
	defer cleanup()

	db, err := sql.Open("sqlite3", fmt.Sprintf("file:%s?mode=ro", tmp))
	if err != nil {
		return err
	}
	defer db.Close()

	opts := commonSQLite.DefaultOptions()
	opts.TableLimit = containerChildLimit
	opts.SampleRowLimit = 0
	analysis, err := commonSQLite.Analyze(ctx, db, &opts)
	if err != nil {
		return err
	}

	layerByTable := map[string]geoPackageLayer{}
	if geoPackage {
		layerByTable = readGeoPackageLayers(ctx, db)
	}

	children := make([]map[string]interface{}, 0, len(analysis.Metadata.Tables))
	spatialColumns := make([]map[string]interface{}, 0)
	primaryGeometryColumn := ""
	geoPackageExtent := geoPackageLayersExtent(layerByTable)
	hasSpatialIndex := geoPackageHasSpatialIndex(ctx, db, layerByTable)
	for _, table := range analysis.Metadata.Tables {
		if geoPackage && isGeoPackageSystemTable(table.Name) {
			continue
		}
		childKind := table.Type
		childName := table.Name
		if layer, ok := layerByTable[table.Name]; ok {
			childKind = "layer"
			childName = layer.Identifier
			if childName == "" {
				childName = table.Name
			}
			if primaryGeometryColumn == "" {
				primaryGeometryColumn = layer.GeometryColumn
			}
			spatialColumn := map[string]interface{}{
				"name":          layer.GeometryColumn,
				"table_name":    table.Name,
				"geometry_type": layer.GeometryType,
			}
			if layer.SRID > 0 {
				spatialColumn["srid"] = layer.SRID
			}
			spatialColumns = append(spatialColumns, spatialColumn)
		}
		children = append(children, map[string]interface{}{
			"name":      childName,
			"table":     table.Name,
			"kind":      childKind,
			"data_type": string(dataitem.DataTypeTable),
			"row_count": table.RowCount,
			"fields":    sqliteFieldAttributes(table.Columns),
		})
	}

	formatName := "sqlite"
	if geoPackage {
		formatName = "geopackage"
	}
	metaattr.UpsertNested(attrs, "type_info", "container", map[string]interface{}{
		"children":       children,
		"child_count":    analysis.Metadata.TableCount + analysis.Metadata.ViewCount,
		"resource_count": 1,
	})
	metaattr.UpsertNested(attrs, "format_info", formatName, map[string]interface{}{
		"version":            analysis.Metadata.Version,
		"table_count":        analysis.Metadata.TableCount,
		"view_count":         analysis.Metadata.ViewCount,
		"index_count":        analysis.Metadata.IndexCount,
		"sampled_children":   len(children),
		"children_truncated": (analysis.Metadata.TableCount + analysis.Metadata.ViewCount) > len(children),
	})
	if geoPackage && len(spatialColumns) > 0 {
		spatialAttrs := map[string]interface{}{
			"geometry_columns":        spatialColumns,
			"primary_geometry_column": primaryGeometryColumn,
			"has_spatial_index":       hasSpatialIndex,
		}
		if geoPackageExtent != nil {
			spatialAttrs["extent"] = geoPackageExtent
		}
		metaattr.UpsertNested(attrs, "capabilities", "spatial", spatialAttrs)
	}
	return nil
}

func sqliteFieldAttributes(columns []commonSQLite.ColumnInfo) []map[string]interface{} {
	mapper := &spatialiteMapper.TypeMapper{}
	fields := make([]format.FieldInfo, 0, len(columns))
	for _, column := range columns {
		fields = append(fields, format.FieldInfo{
			Name:         column.Name,
			Type:         mapper.ToCommon(column.Type),
			OriginalType: column.Type,
			Nullable:     !column.NotNull,
			IsPrimaryKey: column.PrimaryKey,
		})
	}
	return metaattr.FieldAttributesFromFormat(fields)
}

type geoPackageLayer struct {
	TableName      string
	Identifier     string
	GeometryColumn string
	GeometryType   string
	SRID           int
	MinX           sql.NullFloat64
	MinY           sql.NullFloat64
	MaxX           sql.NullFloat64
	MaxY           sql.NullFloat64
}

func readGeoPackageLayers(ctx context.Context, db *sql.DB) map[string]geoPackageLayer {
	query := `
		SELECT c.table_name, COALESCE(c.identifier, c.table_name), g.column_name, g.geometry_type_name, g.srs_id,
		       c.min_x, c.min_y, c.max_x, c.max_y
		FROM gpkg_contents c
		JOIN gpkg_geometry_columns g ON g.table_name = c.table_name
		WHERE c.data_type = 'features'
	`
	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return map[string]geoPackageLayer{}
	}
	defer rows.Close()

	result := map[string]geoPackageLayer{}
	for rows.Next() {
		var layer geoPackageLayer
		if err := rows.Scan(&layer.TableName, &layer.Identifier, &layer.GeometryColumn, &layer.GeometryType, &layer.SRID, &layer.MinX, &layer.MinY, &layer.MaxX, &layer.MaxY); err != nil {
			continue
		}
		result[layer.TableName] = layer
	}
	return result
}

func geoPackageLayersExtent(layers map[string]geoPackageLayer) []float64 {
	if len(layers) == 0 {
		return nil
	}
	var extent []float64
	for _, layer := range layers {
		if !layer.MinX.Valid || !layer.MinY.Valid || !layer.MaxX.Valid || !layer.MaxY.Valid {
			continue
		}
		values := []float64{layer.MinX.Float64, layer.MinY.Float64, layer.MaxX.Float64, layer.MaxY.Float64}
		if !validExtent(values) {
			continue
		}
		if extent == nil {
			extent = append([]float64(nil), values...)
			continue
		}
		extent[0] = math.Min(extent[0], values[0])
		extent[1] = math.Min(extent[1], values[1])
		extent[2] = math.Max(extent[2], values[2])
		extent[3] = math.Max(extent[3], values[3])
	}
	return extent
}

func validExtent(values []float64) bool {
	if len(values) != 4 || values[0] > values[2] || values[1] > values[3] {
		return false
	}
	for _, value := range values {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return true
}

func geoPackageHasSpatialIndex(ctx context.Context, db *sql.DB, layers map[string]geoPackageLayer) bool {
	for _, layer := range layers {
		indexTable := "rtree_" + layer.TableName + "_" + layer.GeometryColumn
		var exists int
		err := db.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type IN ('table', 'virtual table') AND name = ? LIMIT 1`, indexTable).Scan(&exists)
		if err == nil && exists == 1 {
			return true
		}
	}
	return false
}

func isGeoPackageSystemTable(name string) bool {
	normalized := strings.ToLower(strings.TrimSpace(name))
	return strings.HasPrefix(normalized, "gpkg_") || strings.HasPrefix(normalized, "rtree_")
}

func saveContainerReaderToTemp(reader io.Reader, pattern string) (string, func(), error) {
	tmp, err := os.CreateTemp("", pattern)
	if err != nil {
		return "", nil, err
	}
	path := tmp.Name()
	cleanup := func() { _ = os.Remove(path) }
	if _, err := io.Copy(tmp, reader); err != nil {
		_ = tmp.Close()
		cleanup()
		return "", nil, err
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return "", nil, err
	}
	return path, cleanup, nil
}
