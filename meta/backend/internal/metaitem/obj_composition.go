package metaitem

import (
	"context"
	"fmt"
	"math"
	"path"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/obj"
)

func resolveOBJResourceItems(ctx context.Context, input DirectoryResolveInput, files []StorageFileRef) []*DetectedItem {
	byPath := make(map[string]StorageFileRef, len(files))
	for _, file := range files {
		byPath[file.Path] = file
	}
	var items []*DetectedItem
	for _, file := range files {
		if !strings.EqualFold(path.Ext(file.Path), ".obj") {
			continue
		}
		libraries, err := readOBJMaterialLibraries(ctx, input, file, byPath)
		items = append(items, objResourceItem(file, libraries, byPath, err == nil))
	}
	return items
}

func readOBJMaterialLibraries(ctx context.Context, input DirectoryResolveInput, file StorageFileRef, files map[string]StorageFileRef) ([]obj.MaterialLibraryDeclaration, error) {
	declarations, err := readExchangeModelDeclarations(ctx, input, file, format.FormatOBJ)
	if err != nil {
		return nil, err
	}
	sort.Strings(declarations)
	if len(declarations)+1 > authorizationfacts.MaxFileCompositionRefs {
		return nil, fmt.Errorf("OBJ material libraries exceed the complete reference budget")
	}
	libraries := make([]obj.MaterialLibraryDeclaration, 0, len(declarations))
	var materialBytes int64
	for _, declaration := range declarations {
		if !dataitem.ValidOBJMaterialLibraryRef(declaration) {
			return nil, fmt.Errorf("OBJ requires a same-directory material library")
		}
		material, exists := files[path.Join(path.Dir(file.Path), declaration)]
		if !exists || material.Size < 0 || material.Size > obj.MaxCompositionBytes-materialBytes {
			return nil, fmt.Errorf("OBJ material library missing or over budget")
		}
		materialBytes += material.Size
		reader, err := modelStructureReaderAt(ctx, input, material)
		if err != nil {
			return nil, err
		}
		textures, err := obj.ReadMTLTextureRefs(ctx, reader, material.Size)
		if err != nil {
			return nil, err
		}
		libraries = append(libraries, obj.MaterialLibraryDeclaration{Reference: declaration, TextureRefs: textures})
		partial, err := dataitem.OBJCompositionRefs(file.Path, libraries)
		if err != nil || len(partial)+len(declarations)-len(libraries) > authorizationfacts.MaxFileCompositionRefs {
			return nil, fmt.Errorf("OBJ declarations exceed the complete reference budget")
		}
	}
	if _, err := dataitem.OBJCompositionRefs(file.Path, libraries); err != nil {
		return nil, err
	}
	return libraries, nil
}

func objResourceItem(file StorageFileRef, libraries []obj.MaterialLibraryDeclaration, files map[string]StorageFileRef, complete bool) *DetectedItem {
	refs := []dataitem.ItemRef{{Role: "model", Path: file.Path, Required: true, Primary: true, Extension: strings.ToLower(path.Ext(file.Path))}}
	claims := []string{file.Path}
	total := file.Size
	native := map[string]interface{}{"composition_complete": complete}
	if complete {
		expected, err := dataitem.OBJCompositionRefs(file.Path, libraries)
		if err == nil {
			nextTotal := total
			for _, ref := range expected[1:] {
				member, exists := files[ref.Path]
				if !exists || member.Size < 0 || (member.Size == 0 && strings.HasPrefix(ref.Role, "texture")) || member.Size > math.MaxInt64-nextTotal {
					complete = false
					break
				}
				nextTotal += member.Size
			}
			if complete {
				native["material_libraries"] = libraries
				refs, total = expected, nextTotal
				for _, ref := range expected[1:] {
					claims = append(claims, ref.Path)
				}
			}
		} else {
			complete = false
		}
		native["composition_complete"] = complete
	}
	item := detectedItemFromResolvedItem(file.Path, dataitem.ResolvedItem{Name: file.Name, FullName: file.Path, Layout: format.LayoutSingle,
		DataType: datatype.Model3D, Format: string(format.FormatOBJ), PrimaryContentPath: file.Path, RefList: refs,
		ClaimPaths: claims, SizeBytes: &total, DetectionReason: "obj_declared_materials"})
	item.Attributes = map[string]interface{}{"format_info": map[string]interface{}{"obj": native}}
	return item
}
