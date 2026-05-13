package metaitem

import (
	"context"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/resource"
	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/meta/internal/metaattr"
)

type shapefileItemDetector struct{}

var shapefileItemRule = dataitem.FormatRule{
	Format:       "shapefile",
	DataType:     dataitem.DataTypeTable,
	Organization: dataitem.OrganizationMulti,
	Priority:     100,
	Entry: dataitem.EntryRule{
		Extensions: []string{".shp"},
	},
	Components: &dataitem.ComponentRule{
		MatchScope:         dataitem.ComponentMatchScopeSameDirectory,
		MatchKey:           dataitem.ComponentMatchKeyBaseName,
		RequiredExtensions: []string{".shp", ".shx", ".dbf"},
		OptionalExtensions: []string{".prj", ".cpg", ".sbn", ".sbx"},
		EntryExtension:     ".shp",
		AllowRecursive:     false,
	},
}

func (d *shapefileItemDetector) Rule() dataitem.FormatRule {
	return shapefileItemRule
}

func (d *shapefileItemDetector) Priority() int {
	return shapefileItemRule.Priority
}

func (d *shapefileItemDetector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	_, ok := matchShapefileItem(files)
	return ok
}

func (d *shapefileItemDetector) ResolveItems(ctx context.Context, input DirectoryResolveInput) (*DetectionResult, error) {
	matches := matchShapefileItems(input.Files)
	result := &DetectionResult{
		Items:  []*DetectedItem{},
		Claims: ResourceClaimSet{},
	}
	for _, match := range matches {
		info, err := d.extractMatchedItemInfo(ctx, input.ContentReader, input.ConnInfo, input.EngineID, match)
		if err != nil {
			return nil, err
		}
		if info == nil {
			continue
		}
		totalSize := int64(0)
		if info.SizeBytes != nil {
			totalSize = *info.SizeBytes
		}
		item := &DetectedItem{
			Organization:   shapefileItemRule.Organization,
			DataType:       shapefileItemRule.DataType,
			Format:         info.Format,
			PhysicalPath:   input.DirPath,
			EntryPath:      info.EntryPath,
			ComponentFiles: info.ComponentFiles,
			SizeBytes:      totalSize,
			Fields:         info.Fields,
			Attributes:     info.Attributes,
		}
		result.Items = append(result.Items, item)
		for _, path := range info.ComponentFiles {
			result.Claims[path] = true
		}
	}
	return result, nil
}

func (d *shapefileItemDetector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*CompositeItemInfo, error) {
	match, ok := matchShapefileItem(files)
	if !ok {
		return nil, fmt.Errorf("no complete shapefile component set in directory: %s", dirPath)
	}
	return d.extractMatchedItemInfo(ctx, contentReader, connInfo, engineID, match)
}

func (d *shapefileItemDetector) extractMatchedItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	match shapefileItemMatch,
) (*CompositeItemInfo, error) {
	totalSize := int64(0)
	componentFiles := make([]string, 0, len(match.files))
	extensions := make([]string, 0, len(match.exts))
	for _, file := range match.files {
		totalSize += file.Size
		componentFiles = append(componentFiles, file.Path)
	}
	for ext := range match.exts {
		extensions = append(extensions, strings.TrimPrefix(ext, "."))
	}
	sort.Strings(componentFiles)
	sort.Strings(extensions)

	entryPath := match.files[".shp"].Path
	info := &CompositeItemInfo{
		Organization:   shapefileItemRule.Organization,
		DataType:       shapefileItemRule.DataType,
		Format:         shapefileItemRule.Format,
		EntryPath:      entryPath,
		ComponentFiles: componentFiles,
		SizeBytes:      &totalSize,
		Attributes: map[string]interface{}{
			"format_info": map[string]interface{}{
				"shapefile": map[string]interface{}{
					"base_name":            match.baseName,
					"component_extensions": extensions,
					"has_prj":              match.exts[".prj"],
					"has_cpg":              match.exts[".cpg"],
				},
			},
		},
	}

	enrichShapefileInfo(ctx, contentReader, connInfo, engineID, match, info)
	return info, nil
}

func enrichShapefileInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	match shapefileItemMatch,
	info *CompositeItemInfo,
) {
	if contentReader == nil || info == nil {
		return
	}
	provider, err := format.GetTableProvider(format.FormatShapefile)
	if err != nil {
		return
	}
	componentProvider, ok := provider.(format.ComponentTableProvider)
	if !ok {
		return
	}
	tableInfo, err := componentProvider.DescribeTableComponents(ctx, newShapefileMetaComponentReader(contentReader, connInfo, engineID, match), nil)
	if err != nil {
		return
	}
	info.Fields = tableInfo.Fields
	upsertShapefileTableInfo(info, tableInfo)
}

type shapefileMetaComponentReader struct {
	contentReader plugin.ContentReadableProvider
	connInfo      plugin.ConnectionInfo
	engineID      uint
	components    []resource.ComponentRef
}

func newShapefileMetaComponentReader(contentReader plugin.ContentReadableProvider, connInfo plugin.ConnectionInfo, engineID uint, match shapefileItemMatch) *shapefileMetaComponentReader {
	components := make([]resource.ComponentRef, 0, len(match.files))
	exts := make([]string, 0, len(match.files))
	for ext := range match.files {
		exts = append(exts, ext)
	}
	sort.Strings(exts)
	for _, ext := range exts {
		file := match.files[ext]
		role, required := componentRoleAndRequired(format.FormatShapefile, ext)
		if role == "" {
			role = "component"
		}
		components = append(components, resource.ComponentRef{
			ResourceRef:   resource.NewResourceRef(file.Path, resource.ResourceRoleComponent),
			ComponentRole: role,
			Required:      required,
		})
	}
	return &shapefileMetaComponentReader{
		contentReader: contentReader,
		connInfo:      connInfo,
		engineID:      engineID,
		components:    components,
	}
}

func (r *shapefileMetaComponentReader) Components() []resource.ComponentRef {
	return append([]resource.ComponentRef(nil), r.components...)
}

func (r *shapefileMetaComponentReader) OpenComponent(ctx context.Context, component resource.ComponentRef) (io.ReadCloser, error) {
	return r.contentReader.OpenContent(ctx, r.connInfo, shapefileCatalogPathForContent(r.engineID, component.Path), plugin.ReadOptions{})
}

func (r *shapefileMetaComponentReader) OpenComponentRole(ctx context.Context, role string) (io.ReadCloser, error) {
	for _, component := range r.components {
		if strings.EqualFold(component.ComponentRole, role) {
			return r.OpenComponent(ctx, component)
		}
	}
	return nil, resource.ErrComponentNotFound
}

func shapefileCatalogPathForContent(engineID uint, path string) plugin.CatalogPath {
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

func upsertShapefileTableInfo(info *CompositeItemInfo, tableInfo *format.TableInfo) {
	if info == nil || tableInfo == nil {
		return
	}
	upsertShapefileItemSection(info, "type_info", "table", shapefileTableAttributes(tableInfo))
	if formatAttrs := shapefileFormatAttributes(tableInfo); len(formatAttrs) > 0 {
		upsertShapefileItemSection(info, "format_info", "shapefile", formatAttrs)
	}
	if spatialAttrs := shapefileSpatialAttributes(tableInfo.GetSpatialInfo()); len(spatialAttrs) > 0 {
		upsertShapefileItemSection(info, "capabilities", "spatial", spatialAttrs)
	}
}

func shapefileTableAttributes(tableInfo *format.TableInfo) map[string]interface{} {
	attrs := map[string]interface{}{
		"fields":      metaattr.FieldAttributesFromFormat(tableInfo.Fields),
		"primary_key": append([]string(nil), tableInfo.PrimaryKey...),
	}
	if tableInfo.RowCount != nil {
		attrs["row_count"] = *tableInfo.RowCount
	}
	return attrs
}

func shapefileFormatAttributes(tableInfo *format.TableInfo) map[string]interface{} {
	for _, ext := range tableInfo.Extensions {
		if ext == nil || ext.ExtensionType() != "shapefile" {
			continue
		}
		if provider, ok := ext.(interface{ FormatAttributes() map[string]interface{} }); ok {
			return provider.FormatAttributes()
		}
	}
	return nil
}

func shapefileSpatialAttributes(spatialInfo *format.SpatialInfo) map[string]interface{} {
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

func componentRoleAndRequired(formatType format.FormatType, ext string) (string, bool) {
	normalizedExt := resource.NormalizeExtension(ext)
	for _, spec := range componentSpecsForFormat(formatType) {
		if resource.NormalizeExtension(spec.Extension) == normalizedExt {
			return spec.Role, spec.Required
		}
	}
	return "", false
}

func upsertShapefileItemSection(info *CompositeItemInfo, section string, namespace string, values map[string]interface{}) {
	if info.Attributes == nil {
		info.Attributes = map[string]interface{}{}
	}
	sectionAttrs := map[string]interface{}{}
	if existing, ok := info.Attributes[section].(map[string]interface{}); ok {
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
	info.Attributes[section] = sectionAttrs
}

type shapefileItemMatch struct {
	baseName string
	files    map[string]plugin.FileEntry
	exts     map[string]bool
}

func matchShapefileItem(files []plugin.FileEntry) (shapefileItemMatch, bool) {
	matches := matchShapefileItems(files)
	if len(matches) == 0 {
		return shapefileItemMatch{}, false
	}
	return matches[0], true
}

func matchShapefileItems(files []plugin.FileEntry) []shapefileItemMatch {
	requiredExts, knownExts := shapefileComponentExtensionSets()
	groups := map[string]map[string]plugin.FileEntry{}
	for _, file := range files {
		ext := resource.NormalizeExtension(filepath.Ext(file.Name))
		if !knownExts[ext] {
			continue
		}
		base := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
		if base == "" {
			continue
		}
		groupKey := shapefileItemGroupKey(file.Path, base)
		if _, ok := groups[groupKey]; !ok {
			groups[groupKey] = map[string]plugin.FileEntry{}
		}
		groups[groupKey][ext] = file
	}

	groupKeys := make([]string, 0, len(groups))
	for key := range groups {
		groupKeys = append(groupKeys, key)
	}
	sort.Strings(groupKeys)

	matches := make([]shapefileItemMatch, 0, len(groupKeys))
	for _, key := range groupKeys {
		group := groups[key]
		complete := true
		for ext := range requiredExts {
			if _, ok := group[ext]; !ok {
				complete = false
				break
			}
		}
		if !complete {
			continue
		}
		exts := make(map[string]bool, len(group))
		for ext := range group {
			exts[ext] = true
		}
		matches = append(matches, shapefileItemMatch{
			baseName: strings.TrimSuffix(group[".shp"].Name, filepath.Ext(group[".shp"].Name)),
			files:    group,
			exts:     exts,
		})
	}
	return matches
}

func shapefileComponentExtensionSets() (required map[string]bool, known map[string]bool) {
	required = map[string]bool{}
	known = map[string]bool{}
	for _, spec := range componentSpecsForFormat(format.FormatShapefile) {
		ext := resource.NormalizeExtension(spec.Extension)
		if ext == "" {
			continue
		}
		known[ext] = true
		if spec.Required {
			required[ext] = true
		}
	}
	return required, known
}

func componentSpecsForFormat(formatType format.FormatType) []resource.ComponentSpec {
	provider, err := format.GetTableProvider(formatType)
	if err != nil {
		return nil
	}
	specProvider, ok := provider.(format.ComponentSpecProvider)
	if !ok {
		return nil
	}
	return specProvider.ComponentSpecs()
}

func shapefileItemGroupKey(path, base string) string {
	dir := filepath.Dir(strings.Trim(path, "/"))
	if dir == "." {
		dir = ""
	}
	return dir + "\x00" + base
}
