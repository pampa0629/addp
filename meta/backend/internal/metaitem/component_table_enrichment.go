package metaitem

import (
	"context"
	"io"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/meta/internal/metaattr"
)

type commonDataItemDetector struct{}

func (d *commonDataItemDetector) Priority() int {
	return 100
}

func (d *commonDataItemDetector) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
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
		enrichComponentTableInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, item, detected)
		result.Items = append(result.Items, detected)
		for _, path := range detected.ComponentFiles {
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
	size := int64(0)
	if item.SizeBytes != nil {
		size = *item.SizeBytes
	}
	componentFiles := make([]string, 0, len(item.ComponentList))
	for _, component := range item.ComponentList {
		if component.Path != "" {
			componentFiles = append(componentFiles, component.Path)
		}
	}
	sort.Strings(componentFiles)

	return &DetectedItem{
		Organization:   item.Organization,
		DataType:       item.DataType,
		Format:         item.Format,
		PhysicalPath:   physicalPath,
		EntryPath:      item.EntryPath,
		ComponentFiles: componentFiles,
		SizeBytes:      size,
		Attributes:     nil,
	}
}

func enrichComponentTableInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
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
	componentProvider, ok := provider.(format.ComponentTableProvider)
	if !ok {
		return
	}
	tableInfo, err := componentProvider.DescribeTableComponents(ctx, newMetaComponentReader(contentReader, connInfo, engineID, item.ResourceComponents()), nil)
	if err != nil {
		return
	}
	detected.Fields = tableInfo.Fields
	upsertComponentTableInfo(detected, tableInfo)
}

type metaComponentReader struct {
	contentReader plugin.ContentReadableProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
	components    []resource.ComponentRef
}

func newMetaComponentReader(contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, components []resource.ComponentRef) *metaComponentReader {
	return &metaComponentReader{
		contentReader: contentReader,
		connInfo:      connInfo,
		engineID:      engineID,
		components:    append([]resource.ComponentRef(nil), components...),
	}
}

func (r *metaComponentReader) Components() []resource.ComponentRef {
	return append([]resource.ComponentRef(nil), r.components...)
}

func (r *metaComponentReader) OpenComponent(ctx context.Context, component resource.ComponentRef) (io.ReadCloser, error) {
	return r.contentReader.OpenContent(ctx, r.connInfo, catalogPathForContent(r.engineID, component.Path), plugin.ReadOptions{})
}

func (r *metaComponentReader) OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, component := range r.components {
		if strings.EqualFold(component.ComponentRole, role) {
			return r.OpenComponent(ctx, component)
		}
	}
	return nil, resource.ErrComponentNotFound
}

func catalogPathForContent(engineID uint, path string) plugin.CatalogPath {
	return plugin.CatalogPath{
		Version:  plugin.CatalogPathVersion,
		EngineID: engineID,
		Segments: []plugin.CatalogSegment{{
			Term: plugin.CatalogTermPath,
			Kind: plugin.CatalogKindFile,
			Name: path,
		}},
	}
}

func upsertComponentTableInfo(item *DetectedItem, tableInfo *format.TableInfo) {
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
		"fields":      metaattr.FieldAttributesFromFormat(tableInfo.Fields),
		"primary_key": append([]string(nil), tableInfo.PrimaryKey...),
	}
	if tableInfo.RowCount != nil {
		attrs["row_count"] = *tableInfo.RowCount
	}
	return attrs
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
