package shapefile

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	formatShapefile "github.com/addp/common/format/shapefile"
)

var (
	requiredExts = map[string]bool{
		".shp": true,
		".shx": true,
		".dbf": true,
	}
	knownExts = map[string]bool{
		".shp": true,
		".shx": true,
		".dbf": true,
		".prj": true,
		".cpg": true,
		".sbn": true,
		".sbx": true,
	}
)

type Detector struct{}

var rule = dataitem.FormatRule{
	Format:          "shapefile",
	DataFamily:      dataitem.DataFamilyTabular,
	ItemType:        "table",
	CompositionType: dataitem.CompositionTypeMultiFile,
	Priority:        100,
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

func init() {
	dataitem.Register(&Detector{})
}

func (d *Detector) Rule() dataitem.FormatRule {
	return rule
}

func (d *Detector) Priority() int {
	return rule.Priority
}

func (d *Detector) ItemType() string {
	return rule.ItemType
}

func (d *Detector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	_, ok := matchShapefile(files)
	return ok
}

func (d *Detector) ResolveItems(ctx context.Context, input dataitem.DirectoryResolveInput) (*dataitem.DetectionResult, error) {
	matches := matchShapefiles(input.Files)
	result := &dataitem.DetectionResult{
		Items:  []*dataitem.DetectedItem{},
		Claims: dataitem.ResourceClaimSet{},
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
		item := &dataitem.DetectedItem{
			ItemType:        d.ItemType(),
			CompositionType: rule.CompositionType,
			DataFamily:      rule.DataFamily,
			Format:          info.Format,
			PhysicalPath:    input.DirPath,
			EntryPath:       info.EntryPath,
			ComponentFiles:  info.ComponentFiles,
			SizeBytes:       totalSize,
			Fields:          info.Fields,
			Attributes:      info.Attributes,
		}
		result.Items = append(result.Items, item)
		for _, path := range info.ComponentFiles {
			result.Claims[path] = true
		}
	}
	return result, nil
}

func (d *Detector) ExtractItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	dirPath string,
	files []plugin.FileEntry,
) (*dataitem.CompositeItemInfo, error) {
	match, ok := matchShapefile(files)
	if !ok {
		return nil, fmt.Errorf("no complete shapefile component set in directory: %s", dirPath)
	}
	return d.extractMatchedItemInfo(ctx, contentReader, connInfo, engineID, match)
}

func (d *Detector) extractMatchedItemInfo(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	match shapefileMatch,
) (*dataitem.CompositeItemInfo, error) {
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
	info := &dataitem.CompositeItemInfo{
		CompositionType: rule.CompositionType,
		DataFamily:      rule.DataFamily,
		Format:          rule.Format,
		EntryPath:       entryPath,
		ComponentFiles:  componentFiles,
		SizeBytes:       &totalSize,
		Attributes: map[string]interface{}{
			"extensions": map[string]interface{}{
				"builtin.shapefile": map[string]interface{}{
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
	match shapefileMatch,
	info *dataitem.CompositeItemInfo,
) {
	if contentReader == nil || info == nil {
		return
	}
	tempDir, cleanup, err := copyComponentsToTempDir(ctx, contentReader, connInfo, engineID, match)
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
	upsertAttributesSection(info, "schema", map[string]interface{}{
		"row_count":   rowCount,
		"primary_key": []string{},
	})
	upsertExtensionSection(info, "spatial", map[string]interface{}{
		"geometry_column":    "geometry",
		"geometry_type":      geometryType,
		"srid":               0,
		"extent":             extent,
		"dimension":          2,
		"has_spatial_index":  match.exts[".sbn"] && match.exts[".sbx"],
		"source":             "builtin.shapefile",
		"inference_complete": true,
	})
	upsertExtensionSection(info, "builtin.shapefile", map[string]interface{}{
		"shape_type": geometryType,
	})
}

func copyComponentsToTempDir(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	match shapefileMatch,
) (string, func(), error) {
	tempDir, err := os.MkdirTemp("", "addp-shapefile-*")
	if err != nil {
		return "", nil, err
	}
	cleanup := func() { _ = os.RemoveAll(tempDir) }
	for ext, file := range match.files {
		if err := copyContentToFile(ctx, contentReader, connInfo, engineID, file.Path, filepath.Join(tempDir, match.baseName+ext)); err != nil {
			cleanup()
			return "", nil, err
		}
	}
	return tempDir, cleanup, nil
}

func copyContentToFile(
	ctx context.Context,
	contentReader plugin.ContentReadableProvider,
	connInfo plugin.ConnectionInfo,
	engineID uint,
	sourcePath string,
	targetPath string,
) error {
	rc, err := contentReader.OpenContent(ctx, connInfo, catalogPathForContent(engineID, sourcePath), plugin.ReadOptions{})
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

func upsertAttributesSection(info *dataitem.CompositeItemInfo, section string, values map[string]interface{}) {
	if info.Attributes == nil {
		info.Attributes = map[string]interface{}{}
	}
	sectionAttrs := map[string]interface{}{}
	if existing, ok := info.Attributes[section].(map[string]interface{}); ok {
		for k, v := range existing {
			sectionAttrs[k] = v
		}
	}
	for k, v := range values {
		sectionAttrs[k] = v
	}
	info.Attributes[section] = sectionAttrs
}

func upsertExtensionSection(info *dataitem.CompositeItemInfo, namespace string, values map[string]interface{}) {
	if info.Attributes == nil {
		info.Attributes = map[string]interface{}{}
	}
	extensions := map[string]interface{}{}
	if existing, ok := info.Attributes["extensions"].(map[string]interface{}); ok {
		for k, v := range existing {
			extensions[k] = v
		}
	}
	namespaceAttrs := map[string]interface{}{}
	if existing, ok := extensions[namespace].(map[string]interface{}); ok {
		for k, v := range existing {
			namespaceAttrs[k] = v
		}
	}
	for k, v := range values {
		namespaceAttrs[k] = v
	}
	extensions[namespace] = namespaceAttrs
	info.Attributes["extensions"] = extensions
}

type shapefileMatch struct {
	baseName string
	files    map[string]plugin.FileEntry
	exts     map[string]bool
}

func matchShapefile(files []plugin.FileEntry) (shapefileMatch, bool) {
	matches := matchShapefiles(files)
	if len(matches) == 0 {
		return shapefileMatch{}, false
	}
	return matches[0], true
}

func matchShapefiles(files []plugin.FileEntry) []shapefileMatch {
	groups := map[string]map[string]plugin.FileEntry{}
	for _, file := range files {
		ext := strings.ToLower(filepath.Ext(file.Name))
		if !knownExts[ext] {
			continue
		}
		base := strings.TrimSuffix(file.Name, filepath.Ext(file.Name))
		if base == "" {
			continue
		}
		groupKey := shapefileGroupKey(file.Path, base)
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

	matches := make([]shapefileMatch, 0, len(groupKeys))
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
		matches = append(matches, shapefileMatch{
			baseName: strings.TrimSuffix(group[".shp"].Name, filepath.Ext(group[".shp"].Name)),
			files:    group,
			exts:     exts,
		})
	}
	return matches
}

func shapefileGroupKey(path, base string) string {
	dir := filepath.Dir(strings.Trim(path, "/"))
	if dir == "." {
		dir = ""
	}
	return dir + "\x00" + base
}
