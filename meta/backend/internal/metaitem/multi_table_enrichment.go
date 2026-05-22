package metaitem

import (
	"context"
	"io"
	"sort"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func resolveCatalogPath(engineID uint, path string, catalogPathFor func(path string) plugin.CatalogPath) plugin.CatalogPath {
	if catalogPathFor != nil {
		return catalogPathFor(path)
	}
	return plugin.FileItemPath(engineID, path)
}

type commonDataItemResolver struct{}

func (d *commonDataItemResolver) Priority() int {
	return 100
}

func (d *commonDataItemResolver) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	files := input.Files
	if len(input.RecursiveFiles) > 0 {
		files = input.RecursiveFiles
	}
	resolved, err := dataitem.ResolveItems(dataitem.ResolveInput{
		ScopeKind:  dataitem.ScopeKindDirectory,
		ScopePath:  input.DirPath,
		Candidates: fileEntriesToCandidates(files),
	})
	if err != nil {
		return nil, err
	}
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	if resolved == nil {
		return result, nil
	}
	for _, item := range resolved.Items {
		if item.Layout != dataitem.LayoutMulti {
			continue
		}
		detected := detectedItemFromResolvedItem(input.DirPath, item)
		enrichRefTableInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, input.CatalogPathFor, item, detected)
		result.Items = append(result.Items, detected)
		for _, path := range detected.RefFilePaths() {
			result.Claims[path] = true
		}
	}
	return result, nil
}

func fileEntriesToCandidates(files []plugin.FileEntry) []dataitem.Candidate {
	candidates := make([]dataitem.Candidate, 0, len(files))
	for _, file := range files {
		size := file.Size
		candidates = append(candidates, dataitem.Candidate{
			Path:        file.Path,
			Name:        file.Name,
			ContentType: file.ContentType,
			SizeBytes:   &size,
		})
	}
	return candidates
}

func detectedItemFromResolvedItem(physicalPath string, item dataitem.ResolvedItem) *DetectedItem {
	sort.Slice(item.RefList, func(i, j int) bool {
		return item.RefList[i].Path < item.RefList[j].Path
	})

	return &DetectedItem{
		ResolvedItem: item,
		PhysicalPath: physicalPath,
	}
}

func enrichRefTableInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item dataitem.ResolvedItem,
	detected *DetectedItem,
) {
	if contentReader == nil || detected == nil || item.Format == "" {
		return
	}
	refProvider, err := format.GetMultiTableInfoProvider(format.FormatType(item.Format))
	if err != nil {
		return
	}
	reader := newMetaRefReader(contentReader, connInfo, engineID, catalogPathFor)
	tableInfo, err := refProvider.DescribeMultiTable(ctx, reader, item.RelatedRefs(), nil)
	if err != nil {
		return
	}
	if tableInfo != nil && tableInfo.Table != nil {
		detected.Fields = format.FormatFieldInfos(tableInfo.Table.Fields)
	}
	upsertRefTableInfo(detected, tableInfo)
}

type metaRefReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.CatalogPath
}

func newMetaRefReader(contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, catalogPathFor func(path string) plugin.CatalogPath) *metaRefReader {
	return &metaRefReader{
		contentReader:  contentReader,
		connInfo:       connInfo,
		engineID:       engineID,
		catalogPathFor: catalogPathFor,
	}
}

func (r *metaRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r *metaRefReader) Stat(context.Context, contentio.Ref) (*contentio.Stat, error) {
	return nil, contentio.ErrContentNotFound
}

func upsertRefTableInfo(item *DetectedItem, tableInfo *datatype.TableDescribeResult) {
	if item == nil || tableInfo == nil {
		return
	}
	upsertItemSection(&item.Attributes, "type_info", "table", tableAttributes(tableInfo))
	if formatAttrs := formatAttributesFromDescribeResult(tableInfo); len(formatAttrs) > 0 {
		upsertItemSection(&item.Attributes, "format_info", item.Format, formatAttrs)
	}
	if spatialAttrs := spatialAttributes(tableInfo.Spatial); len(spatialAttrs) > 0 {
		upsertItemSection(&item.Attributes, "capabilities", "spatial", spatialAttrs)
	}
	if tableInfo.ContentIndex != nil {
		upsertItemSection(&item.Attributes, "content_index", "table", contentIndexAttributes(tableInfo.ContentIndex))
	}
}

func EnrichKnownMultiTableItem(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	catalogPathFor func(path string) plugin.CatalogPath,
	item *DetectedItem,
) (*DetectedItem, bool, error) {
	if contentReader == nil || item == nil || item.Layout != dataitem.LayoutMulti || item.Format == "" || len(item.RefList) == 0 {
		return item, false, nil
	}
	refProvider, err := format.GetMultiTableInfoProvider(format.FormatType(item.Format))
	if err != nil {
		return item, false, nil
	}
	reader := newMetaRefReader(contentReader, connInfo, engineID, catalogPathFor)
	tableInfo, err := refProvider.DescribeMultiTable(ctx, reader, item.RelatedRefs(), nil)
	if err != nil {
		return item, false, err
	}
	if tableInfo.Table != nil {
		item.Fields = format.FormatFieldInfos(tableInfo.Table.Fields)
	}
	upsertRefTableInfo(item, tableInfo)
	return item, true, nil
}

func tableAttributes(tableInfo *datatype.TableDescribeResult) map[string]interface{} {
	if tableInfo == nil || tableInfo.Table == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"fields":      fieldAttributesFromDatatype(tableInfo.Table.Fields),
		"primary_key": append([]string(nil), tableInfo.Table.PrimaryKey...),
	}
	if tableInfo.Table.RowCount != nil {
		attrs["row_count"] = *tableInfo.Table.RowCount
	}
	return attrs
}

func fieldAttributesFromDatatype(fields []datatype.FieldInfo) []map[string]interface{} {
	fieldsData := make([]map[string]interface{}, 0, len(fields))
	for _, f := range fields {
		fieldsData = append(fieldsData, map[string]interface{}{
			"name":     f.Name,
			"type":     string(f.Type),
			"nullable": f.Nullable,
		})
	}
	return fieldsData
}

func formatAttributesFromDescribeResult(tableInfo *datatype.TableDescribeResult) map[string]interface{} {
	if tableInfo == nil || len(tableInfo.FormatInfo) == 0 {
		return nil
	}
	return tableInfo.FormatInfo
}

func spatialAttributes(spatialInfo *datatype.SpatialInfo) map[string]interface{} {
	if spatialInfo == nil {
		return nil
	}
	geometryColumns := make([]map[string]interface{}, 0, len(spatialInfo.GeometryColumns))
	for _, column := range spatialInfo.GeometryColumns {
		columnAttrs := map[string]interface{}{
			"name":          column.Name,
			"geometry_type": column.GeometryType,
		}
		if column.SRID != nil {
			columnAttrs["srid"] = *column.SRID
		}
		if column.Dimension != nil {
			columnAttrs["dimension"] = *column.Dimension
		}
		if column.Nullable != nil {
			columnAttrs["nullable"] = *column.Nullable
		}
		geometryColumns = append(geometryColumns, columnAttrs)
	}
	attrs := map[string]interface{}{
		"geometry_columns":        geometryColumns,
		"primary_geometry_column": spatialInfo.PrimaryGeometryColumn,
	}
	if spatialInfo.HasSpatialIndex != nil {
		attrs["has_spatial_index"] = *spatialInfo.HasSpatialIndex
	}
	if spatialInfo.Extent != nil {
		bbox := *spatialInfo.Extent
		attrs["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	}
	return attrs
}

func contentIndexAttributes(index *datatype.ContentIndex) map[string]interface{} {
	if index == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"kind":        index.Kind,
		"data_type":   index.DataType,
		"format":      index.Format,
		"unit":        index.Unit,
		"offset_unit": index.OffsetUnit,
		"step":        index.Step,
		"row_count":   index.RowCount,
	}
	if index.HeaderBytes > 0 {
		attrs["header_bytes"] = index.HeaderBytes
	}
	if len(index.Source) > 0 {
		attrs["source"] = index.Source
	}
	if len(index.Anchors) > 0 {
		anchors := make([]map[string]interface{}, 0, len(index.Anchors))
		for _, anchor := range index.Anchors {
			anchors = append(anchors, map[string]interface{}{
				"row":         anchor.Row,
				"byte_offset": anchor.ByteOffset,
			})
		}
		attrs["anchors"] = anchors
	}
	return attrs
}

func upsertItemSection(attrs *map[string]interface{}, section string, namespace string, values map[string]interface{}) {
	if len(values) == 0 {
		return
	}
	if *attrs == nil {
		*attrs = map[string]interface{}{}
	}
	itemAttrs := *attrs
	sectionAttrs := map[string]interface{}{}
	if existing, ok := itemAttrs[section].(map[string]interface{}); ok {
		for k, v := range existing {
			sectionAttrs[k] = v
		}
	}
	namespaceAttrs := map[string]interface{}{}
	if existing, ok := sectionAttrs[namespace].(map[string]interface{}); ok {
		for k, v := range existing {
			namespaceAttrs[k] = v
		}
	}
	for k, v := range values {
		namespaceAttrs[k] = v
	}
	sectionAttrs[namespace] = namespaceAttrs
	itemAttrs[section] = sectionAttrs
}
