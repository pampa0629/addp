package shapefile

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/engine/plugin"
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

func init() {
	dataitem.Register(&Detector{})
}

func (d *Detector) Priority() int {
	return 100
}

func (d *Detector) ItemType() string {
	return "table"
}

func (d *Detector) Detect(ctx context.Context, files []plugin.FileEntry, subdirs []plugin.DirEntry) bool {
	_, ok := matchShapefile(files)
	return ok
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
	return &dataitem.CompositeItemInfo{
		CompositionType: dataitem.CompositionTypeMultiFile,
		DataFamily:      dataitem.DataFamilyTabular,
		Format:          "shapefile",
		EntryPath:       entryPath,
		ComponentFiles:  componentFiles,
		SizeBytes:       &totalSize,
		Attributes: map[string]interface{}{
			"format":               "shapefile",
			"mode":                 "multi_file",
			"base_name":            match.baseName,
			"component_extensions": extensions,
			"has_prj":              match.exts[".prj"],
			"has_cpg":              match.exts[".cpg"],
		},
	}, nil
}

type shapefileMatch struct {
	baseName string
	files    map[string]plugin.FileEntry
	exts     map[string]bool
}

func matchShapefile(files []plugin.FileEntry) (shapefileMatch, bool) {
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
		if _, ok := groups[base]; !ok {
			groups[base] = map[string]plugin.FileEntry{}
		}
		groups[base][ext] = file
	}

	bases := make([]string, 0, len(groups))
	for base := range groups {
		bases = append(bases, base)
	}
	sort.Strings(bases)

	for _, base := range bases {
		group := groups[base]
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
		return shapefileMatch{
			baseName: base,
			files:    group,
			exts:     exts,
		}, true
	}
	return shapefileMatch{}, false
}
