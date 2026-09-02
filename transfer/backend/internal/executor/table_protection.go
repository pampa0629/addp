package executor

import (
	"context"
	"fmt"
	"sort"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/resume"
)

type protectedTableBatchReader struct {
	inner       TableBatchReader
	protect     func(*engineplugin.QueryResult) error
	tableInfo   *datatype.TableInfo
	spatialInfo *datatype.SpatialInfo
}

func protectTableBatchReader(inner TableBatchReader, protect func(*engineplugin.QueryResult) error) (TableBatchReader, error) {
	if inner == nil || protect == nil {
		return inner, nil
	}
	tableInfo, visible, err := protectTableInfo(inner.TableInfo(), protect)
	if err != nil {
		_ = inner.Close(context.Background())
		return nil, err
	}
	return &protectedTableBatchReader{
		inner: inner, protect: protect, tableInfo: tableInfo,
		spatialInfo: filterProtectedSpatialInfo(inner.SpatialInfo(), visible),
	}, nil
}

func (r *protectedTableBatchReader) TableInfo() *datatype.TableInfo { return r.tableInfo }

func (r *protectedTableBatchReader) SpatialInfo() *datatype.SpatialInfo {
	return r.spatialInfo.Clone()
}

func (r *protectedTableBatchReader) ReadBatch(ctx context.Context, limit int) (*engineplugin.BatchData, error) {
	batch, err := r.inner.ReadBatch(ctx, limit)
	if err != nil || batch == nil {
		return batch, err
	}
	columns := batchFieldNames(batch.Fields)
	if len(columns) == 0 && r.inner.TableInfo() != nil {
		columns = r.inner.TableInfo().FieldNames()
	}
	if len(columns) == 0 {
		columns = rowFieldNames(batch.Rows)
	}
	result := &engineplugin.QueryResult{Columns: columns, Rows: batch.Rows}
	if err := r.protect(result); err != nil {
		return nil, fmt.Errorf("protect source batch: %w", err)
	}
	batch.Rows = result.Rows
	batch.Fields = filterProtectedFields(batch.Fields, result.Columns)
	batch.Spatial = filterProtectedSpatialInfo(batch.Spatial, stringSet(result.Columns))
	return batch, nil
}

func (r *protectedTableBatchReader) Close(ctx context.Context) error {
	return r.inner.Close(ctx)
}

func (r *protectedTableBatchReader) ResumeMarker() *resume.Marker {
	return r.inner.ResumeMarker()
}

func protectTableInfo(info *datatype.TableInfo, protect func(*engineplugin.QueryResult) error) (*datatype.TableInfo, map[string]struct{}, error) {
	if info == nil {
		return nil, nil, nil
	}
	result := &engineplugin.QueryResult{Columns: info.FieldNames()}
	if err := protect(result); err != nil {
		return nil, nil, fmt.Errorf("protect source table schema: %w", err)
	}
	visible := stringSet(result.Columns)
	protected := info.Clone()
	protected.Fields = filterProtectedFields(protected.Fields, result.Columns)
	protected.PrimaryKey = filterProtectedNames(protected.PrimaryKey, visible)
	return protected, visible, nil
}

func batchFieldNames(fields []datatype.FieldInfo) []string {
	names := make([]string, 0, len(fields))
	for _, field := range fields {
		if field.Name != "" {
			names = append(names, field.Name)
		}
	}
	return names
}

func rowFieldNames(rows []map[string]interface{}) []string {
	seen := map[string]struct{}{}
	for _, row := range rows {
		for name := range row {
			seen[name] = struct{}{}
		}
	}
	names := make([]string, 0, len(seen))
	for name := range seen {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func filterProtectedFields(fields []datatype.FieldInfo, columns []string) []datatype.FieldInfo {
	if len(fields) == 0 {
		return nil
	}
	visible := stringSet(columns)
	result := make([]datatype.FieldInfo, 0, len(fields))
	for _, field := range fields {
		if _, ok := visible[field.Name]; ok {
			result = append(result, field)
		}
	}
	return result
}

func filterProtectedNames(names []string, visible map[string]struct{}) []string {
	result := make([]string, 0, len(names))
	for _, name := range names {
		if _, ok := visible[name]; ok {
			result = append(result, name)
		}
	}
	return result
}

func filterProtectedSpatialInfo(info *datatype.SpatialInfo, visible map[string]struct{}) *datatype.SpatialInfo {
	if info == nil {
		return nil
	}
	if visible == nil {
		return info.Clone()
	}
	protected := info.Clone()
	columns := make([]datatype.GeometryColumnInfo, 0, len(protected.GeometryColumns))
	for _, column := range protected.GeometryColumns {
		if _, ok := visible[column.Name]; ok {
			columns = append(columns, column)
		}
	}
	protected.GeometryColumns = columns
	if len(columns) == 0 {
		return nil
	}
	if _, ok := visible[protected.PrimaryGeometryColumn]; !ok {
		protected.PrimaryGeometryColumn = columns[0].Name
	}
	return protected
}

func stringSet(values []string) map[string]struct{} {
	result := make(map[string]struct{}, len(values))
	for _, value := range values {
		result[value] = struct{}{}
	}
	return result
}
