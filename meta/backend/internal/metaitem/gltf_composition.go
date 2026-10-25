package metaitem

import (
	"context"
	"io"
	"math"
	"path"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
)

func resolveGLTFManifestItems(ctx context.Context, input DirectoryResolveInput, files []StorageFileRef) []*DetectedItem {
	byPath := input.ModelReferenceFiles
	if byPath == nil {
		byPath = make(map[string]StorageFileRef, len(files))
		for _, file := range files {
			byPath[file.Path] = file
		}
	}
	var result []*DetectedItem
	for _, file := range files {
		if !strings.EqualFold(path.Ext(file.Path), ".gltf") {
			continue
		}
		refs := []dataitem.ItemRef{{Path: file.Path, Role: "manifest", Required: true, Primary: true, Extension: strings.ToLower(path.Ext(file.Path))}}
		complete := false
		target, targetErr := modelPrimaryCatalogPath(input, file)
		if input.ContentReader != nil && targetErr == nil && file.Size > 0 && file.Size <= format.MaxGLTFManifestBytes {
			reader, err := input.ContentReader.OpenContent(ctx, input.ConnInfo, target, plugin.ReadOptions{Length: format.MaxGLTFManifestBytes + 1})
			if err != nil && reader != nil {
				_ = reader.Close()
			}
			if err == nil && reader != nil {
				raw, readErr := io.ReadAll(io.LimitReader(reader, format.MaxGLTFManifestBytes+1))
				closeErr := reader.Close()
				parsed, parseErr := format.ParseGLTFComposition(ctx, raw)
				if readErr == nil && parseErr == nil && closeErr == nil {
					expected, refErr := dataitem.GLTFCompositionRefs(file.Path, parsed.Resources)
					if refErr == nil {
						complete = true
						for _, ref := range expected[1:] {
							member, exists := byPath[ref.Path]
							if !exists || member.Size <= 0 {
								complete = false
								break
							}
						}
						if complete {
							for _, resource := range parsed.Resources {
								member := byPath[path.Join(path.Dir(file.Path), resource.Path)]
								if resource.Role == "buffer" && member.Size < resource.ByteLength {
									complete = false
									break
								}
							}
						}
						if complete {
							refs = expected
						}
					}
				}
			}
		}
		total := file.Size
		claims := []string{file.Path}
		for _, ref := range refs[1:] {
			if byPath[ref.Path].Size > math.MaxInt64-total {
				complete = false
				break
			}
			total += byPath[ref.Path].Size
			claims = append(claims, ref.Path)
		}
		if !complete {
			refs = refs[:1]
			claims = claims[:1]
			total = file.Size
		}
		item := detectedItemFromResolvedItem(file.Path, dataitem.ResolvedItem{Name: file.Name, FullName: file.Path, Layout: format.LayoutMulti, DataType: datatype.Model3D, Format: "gltf", PrimaryContentPath: file.Path, RefList: refs, ClaimPaths: claims, SizeBytes: &total, DetectionReason: "gltf_manifest"})
		item.Attributes = map[string]interface{}{"format_info": map[string]interface{}{"gltf": map[string]interface{}{"composition_complete": complete}}}
		result = append(result, item)
	}
	return result
}
