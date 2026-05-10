package metaitem

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/meta/internal/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	formatShapefile "github.com/addp/common/format/codecs/shapefile"
)

var (
	shapefileRequiredExts = map[string]bool{
		".shp": true,
		".shx": true,
		".dbf": true,
	}
	shapefileKnownExts = map[string]bool{
		".shp": true,
		".shx": true,
		".dbf": true,
		".prj": true,
		".cpg": true,
		".sbn": true,
		".sbx": true,
	}
)

type shapefileItemDetector struct{}

var shapefileItemRule = dataitem.FormatRule{
	Format:       "shapefile",
	DataType:     dataitem.DataTypeTable,
	ItemType:     "table",
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

func (d *shapefileItemDetector) ItemType() string {
	return shapefileItemRule.ItemType
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
			ItemType:       d.ItemType(),
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
	tempDir, cleanup, err := copyShapefileComponentsToTempDir(ctx, contentReader, connInfo, engineID, match)
	if err != nil {
		return
	}
	defer cleanup()

	reader, err := formatShapefile.Open(filepath.Join(tempDir, match.baseName+".shp"))
	if err != nil {
		return
	}
	defer reader.Close()

	geometryType := formatShapefile.MapShapeType(reader.GeometryType)
	fields := []format.FieldInfo{{
		Name:         "geometry",
		Type:         format.FieldTypeGeometry,
		OriginalType: geometryType,
		Nullable:     false,
		IsPrimaryKey: false,
		Comment:      "Shapefile geometry field",
	}}

	mapper := format.GetTypeMapper("shapefile")
	for _, field := range reader.GetSchema() {
		fieldType := format.FieldTypeUnknown
		if mapper != nil {
			fieldType = mapper.ToCommon(field.RawType)
		}
		fields = append(fields, format.FieldInfo{
			Name:         field.Name,
			Type:         fieldType,
			OriginalType: field.RawType,
			Nullable:     true,
			Size:         field.Size,
			Precision:    field.Precision,
		})
	}
	info.Fields = fields

	var rowCount int64
	var bbox *[4]float64
	for reader.Next() {
		rowCount++
		_, shape := reader.Shape()
		if shape == nil {
			continue
		}
		shapeBox := shape.BBox()
		if bbox == nil {
			bbox = &[4]float64{shapeBox.MinX, shapeBox.MinY, shapeBox.MaxX, shapeBox.MaxY}
			continue
		}
		if shapeBox.MinX < bbox[0] {
			bbox[0] = shapeBox.MinX
		}
		if shapeBox.MinY < bbox[1] {
			bbox[1] = shapeBox.MinY
		}
		if shapeBox.MaxX > bbox[2] {
			bbox[2] = shapeBox.MaxX
		}
		if shapeBox.MaxY > bbox[3] {
			bbox[3] = shapeBox.MaxY
		}
	}

	var extent interface{}
	if bbox != nil {
		extent = []float64{bbox[0], bbox[1], bbox[2], bbox[3]}
	}
	upsertShapefileItemSection(info, "type_info", "table", map[string]interface{}{
		"fields":      shapefileFieldAttributes(fields),
		"row_count":   rowCount,
		"primary_key": []string{},
	})
	upsertShapefileItemSection(info, "capabilities", "spatial", map[string]interface{}{
		"geometry_columns": []map[string]interface{}{{
			"name":          "geometry",
			"geometry_type": geometryType,
			"dimension":     2,
			"nullable":      false,
		}},
		"primary_geometry_column": "geometry",
		"extent":                  extent,
		"has_spatial_index":       match.exts[".sbn"] && match.exts[".sbx"],
	})
	upsertShapefileItemSection(info, "format_info", "shapefile", map[string]interface{}{
		"shape_type": geometryType,
	})
}

func copyShapefileComponentsToTempDir(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	match shapefileItemMatch,
) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "addp-shapefile-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	for ext, file := range match.files {
		if err := copyShapefileContentToFile(ctx, contentReader, connInfo, engineID, file.Path, filepath.Join(tempDir, match.baseName+ext)); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tempDir, cleanup, nil
}

func copyShapefileContentToFile(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	sourcePath string,
	targetPath string,
) error {
	rc, err := contentReader.OpenContent(ctx, connInfo, shapefileCatalogPathForContent(engineID, sourcePath), plugin.ReadOptions{})
	if err != nil {
		return err
	}
	defer rc.Close()

	out, err := os.Create(targetPath)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, rc)
	return err
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

func shapefileFieldAttributes(fields []format.FieldInfo) []map[string]interface{} {
	attrs := make([]map[string]interface{}, 0, len(fields))
	for _, field := range fields {
		attrs = append(attrs, map[string]interface{}{
			"name":          field.Name,
			"type":          string(field.Type),
			"original_type": field.OriginalType,
			"nullable":      field.Nullable,
			"size":          field.Size,
			"precision":     field.Precision,
		})
	}
	return attrs
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
	groups := map[string]map[string]plugin.FileEntry{}
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !shapefileKnownExts[ext] {
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
		for ext := range shapefileRequiredExts {
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

func shapefileItemGroupKey(path, base string) string {
	dir := filepath.Dir(strings.Trim(path, "/"))
	if dir == "." {
		dir = ""
	}
	return dir + "\x00" + base
}
