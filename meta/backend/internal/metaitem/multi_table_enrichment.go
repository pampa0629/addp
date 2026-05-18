package metaitem

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
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
		if item.Organization != dataitem.OrganizationMulti {
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
	provider, err := format.GetTableProvider(format.FormatType(item.Format))
	if err != nil {
		return
	}
	refProvider, ok := provider.(format.MultiTableProvider)
	if !ok {
		return
	}
	tableInfo, err := refProvider.DescribeMultiTable(ctx, newMetaRefReader(contentReader, connInfo, engineID, catalogPathFor, item.ContentRefs()), nil)
	if err != nil {
		return
	}
	detected.Fields = tableInfo.Fields
	upsertRefTableInfo(detected, tableInfo)
}

type metaRefReader struct {
	contentReader  plugin.ContentReadableProvider
	connInfo       plugin.ConnectionInfo
	engineID       uint
	catalogPathFor func(path string) plugin.CatalogPath
	refs           []contentio.Ref
}

func newMetaRefReader(contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, catalogPathFor func(path string) plugin.CatalogPath, refs []contentio.Ref) *metaRefReader {
	return &metaRefReader{
		contentReader:  contentReader,
		connInfo:       connInfo,
		engineID:       engineID,
		catalogPathFor: catalogPathFor,
		refs:           append([]contentio.Ref(nil), refs...),
	}
}

func (r *metaRefReader) Refs() []contentio.Ref {
	return append([]contentio.Ref(nil), r.refs...)
}

func (r *metaRefReader) Open(ctx context.Context, ref contentio.Ref) (io.ReadCloser, error) {
	return r.contentReader.OpenContent(ctx, r.connInfo, resolveCatalogPath(r.engineID, ref.Path, r.catalogPathFor), plugin.ReadOptions{})
}

func (r *metaRefReader) OpenRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, ref := range r.refs {
		if strings.EqualFold(ref.Role, role) {
			return r.Open(ctx, ref)
		}
	}
	return nil, contentio.ErrContentNotFound
}

func upsertRefTableInfo(item *DetectedItem, tableInfo *format.TableInfo) {
	if item == nil || tableInfo == nil {
		return
	}
	upsertItemSection(&item.Attributes, "type_info", "table", tableAttributes(tableInfo))
	if formatAttrs := formatAttributesFromTableInfo(item.Format, tableInfo); len(formatAttrs) > 0 {
		upsertItemSection(&item.Attributes, "format_info", item.Format, formatAttrs)
	}
	if spatialAttrs := spatialAttributes(tableInfo.GetSpatialInfo()); len(spatialAttrs) > 0 {
		upsertItemSection(&item.Attributes, "capabilities", "spatial", spatialAttrs)
	}
}

func tableAttributes(tableInfo *format.TableInfo) map[string]interface{} {
	attrs := map[string]interface{}{
		"fields":      fieldAttributesFromFormat(tableInfo.Fields),
		"primary_key": append([]string(nil), tableInfo.PrimaryKey...),
	}
	if tableInfo.RowCount != nil {
		attrs["row_count"] = *tableInfo.RowCount
	}
	return attrs
}

func fieldAttributesFromFormat(fields []format.FieldInfo) []map[string]interface{} {
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

type formatAttributesProvider interface {
	FormatAttributes() map[string]interface{}
}

func formatAttributesFromTableInfo(formatName string, tableInfo *format.TableInfo) map[string]interface{} {
	if tableInfo == nil || len(tableInfo.FormatInfo) == 0 {
		return nil
	}
	if provider, ok := tableInfo.FormatInfo[formatName].(formatAttributesProvider); ok {
		return provider.FormatAttributes()
	}
	if attrs, ok := tableInfo.FormatInfo[formatName].(map[string]interface{}); ok {
		return attrs
	}
	return nil
}

func spatialAttributes(spatialInfo *format.SpatialInfo) map[string]interface{} {
	if spatialInfo == nil {
		return nil
	}
	attrs := map[string]interface{}{
		"geometry_columns": []map[string]interface{}{{
			"name":          spatialInfo.GeometryColumn,
			"geometry_type": spatialInfo.GeometryType,
			"srid":          spatialInfo.SRID,
			"dimension":     spatialInfo.Dimension,
			"nullable":      false,
		}},
		"primary_geometry_column": spatialInfo.GeometryColumn,
		"has_spatial_index":       spatialInfo.HasSpatialIndex,
	}
	if spatialInfo.BoundingBox != nil {
		bbox := *spatialInfo.BoundingBox
		attrs["extent"] = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
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
