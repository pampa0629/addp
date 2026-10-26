package metaitem

import (
	"context"
	"fmt"
	"io"
	"path"
	"reflect"
	"sort"
	"strings"

	"github.com/addp/common/contentio"
	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/contentadapter"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"github.com/addp/common/format/plugins/fbx"
)

func resolveStructuredExchangeModelItem(ctx context.Context, input DirectoryResolveInput, file StorageFileRef, files map[string]StorageFileRef, formatType format.FormatType) *DetectedItem {
	declarations, err := readExchangeModelDeclarations(ctx, input, file, formatType)
	return exchangeModelResourceItem(file, formatType, declarations, files, err == nil && (formatType != format.FormatFBX || declarations != nil))
}

// Structural identification persists the exact observed composition. The
// legacy exchange summary fallback does not apply to STL, GLB or FBX. Neither opens dependencies.
func exchangeModelResourceItem(file StorageFileRef, formatType format.FormatType, declarations []string, files map[string]StorageFileRef, complete bool) *DetectedItem {
	refs := []dataitem.ItemRef{{Role: "model", Path: file.Path, Required: true, Primary: true, Extension: strings.ToLower(path.Ext(file.Path))}}
	claims := []string{file.Path}
	total := file.Size
	detectionReason := "model_declared_textures"
	native := map[string]interface{}{"scan_complete": complete}
	if formatType == format.FormatSTL || formatType == format.FormatGLB || formatType == format.FormatFBX {
		detectionReason = "model_single_primary"
		native = map[string]interface{}{"composition_complete": complete && len(declarations) == 0}
		complete = complete && len(declarations) == 0
	}
	if complete {
		if formatType == format.FormatFBX {
			native["profile"] = fbx.Profile
		}
		if formatType != format.FormatSTL && formatType != format.FormatGLB && formatType != format.FormatFBX {
			native["texture_refs"] = append([]string{}, declarations...)
		}
		ordered := append([]string{}, declarations...)
		sort.Strings(ordered)
		seen := map[string]bool{file.Path: true}
		counts := map[string]int{}
		for _, declaration := range ordered {
			if formatType == format.FormatDAE {
				decoded, err := format.DecodeDAETextureRef(declaration)
				if err != nil {
					continue
				}
				declaration = decoded
			}
			resolved, ok := ResolveExchangeModelTexturePath(file.Path, declaration)
			texture, found := files[resolved]
			if !ok || !found || seen[resolved] {
				continue
			}
			refs = append(refs, dataitem.ItemRef{Role: uniqueRefRole("texture", counts), Path: resolved, Required: true, Extension: strings.ToLower(path.Ext(resolved))})
			claims = append(claims, resolved)
			seen[resolved] = true
			total += texture.Size
		}
	}
	item := detectedItemFromResolvedItem(file.Path, dataitem.ResolvedItem{Name: file.Name, FullName: file.Path, Layout: format.LayoutSingle,
		DataType: datatype.Model3D, Format: string(formatType), PrimaryContentPath: file.Path, RefList: refs, ClaimPaths: claims, SizeBytes: &total,
		DetectionReason: detectionReason})
	// An unsuccessful observation replaces the old complete declaration, rather
	// than retaining an authorization boundary that the current scan cannot prove.
	item.Attributes = map[string]interface{}{"format_info": map[string]interface{}{string(formatType): native}}
	return item
}

func readExchangeModelDeclarations(ctx context.Context, input DirectoryResolveInput, file StorageFileRef, formatType format.FormatType) ([]string, error) {
	reader, err := modelStructureReaderAt(ctx, input, file)
	if err != nil {
		return nil, err
	}
	provider, err := format.GetModel3DCompositionProvider(formatType)
	if err != nil {
		return nil, err
	}
	return provider.ReadModel3DComposition(ctx, reader, file.Size)
}

func modelStructureReaderAt(ctx context.Context, input DirectoryResolveInput, file StorageFileRef) (io.ReaderAt, error) {
	if _, ok := input.StructureReader.(plugin.RangeReadableProvider); !ok {
		return nil, fmt.Errorf("model structure requires native range access")
	}
	target, err := modelPrimaryCatalogPath(input, file)
	if err != nil {
		return nil, err
	}
	reader := contentadapter.NewMappedReader(input.StructureReader, input.ConnInfo,
		func(ref contentio.Ref) (plugin.EngineCatalogPath, error) {
			if ref.Path != file.Path {
				return plugin.EngineCatalogPath{}, fmt.Errorf("model structure reference is outside the primary file")
			}
			return target, nil
		}, plugin.ReadOptions{})
	return contentio.NewReaderAt(ctx, reader.(contentio.RangeReader), contentio.NewRef(file.Path, "model")), nil
}

func modelPrimaryCatalogPath(input DirectoryResolveInput, file StorageFileRef) (plugin.EngineCatalogPath, error) {
	target := resolveCatalogPath(input.EngineID, file.Path, input.EngineCatalogPathFor)
	exact := reflect.DeepEqual(target, plugin.FileItemPath(input.EngineID, file.Path))
	if len(target.Segments) >= 3 && target.Segments[1].Term == plugin.EngineCatalogTermBucket {
		exact = reflect.DeepEqual(target, plugin.ObjectItemPath(input.EngineID, target.Segments[1].Name, file.Path))
	}
	if !exact {
		return plugin.EngineCatalogPath{}, fmt.Errorf("model structure requires the exact typed primary file")
	}
	return target, nil
}
