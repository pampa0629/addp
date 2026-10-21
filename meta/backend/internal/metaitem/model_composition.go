package metaitem

import (
	"context"
	"fmt"
	"net/url"
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
)

func resolveStructured3DSItem(ctx context.Context, input DirectoryResolveInput, file StorageFileRef, files map[string]StorageFileRef) *DetectedItem {
	declarations, err := read3DSDeclarations(ctx, input, file)
	return exchangeModelResourceItem(file, format.Format3DS, declarations, files, err == nil)
}

// Both structural identification and deep summaries use the same resource
// matching and persistence rules. Neither path opens declared dependencies.
func exchangeModelResourceItem(file StorageFileRef, formatType format.FormatType, declarations []string, files map[string]StorageFileRef, complete bool) *DetectedItem {
	refs := []dataitem.ItemRef{{Role: "model", Path: file.Path, Required: true, Primary: true, Extension: strings.ToLower(path.Ext(file.Path))}}
	claims := []string{file.Path}
	total := file.Size
	native := map[string]interface{}{"scan_complete": complete}
	if complete {
		native["texture_refs"] = append([]string{}, declarations...)
		ordered := append([]string{}, declarations...)
		sort.Strings(ordered)
		seen := map[string]bool{file.Path: true}
		counts := map[string]int{}
		for _, declaration := range ordered {
			if formatType == format.FormatDAE {
				decoded, err := url.PathUnescape(declaration)
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
		DetectionReason: "model_declared_textures"})
	// An unsuccessful observation replaces the old complete declaration, rather
	// than retaining an authorization boundary that the current scan cannot prove.
	item.Attributes = map[string]interface{}{"format_info": map[string]interface{}{string(formatType): native}}
	return item
}

func read3DSDeclarations(ctx context.Context, input DirectoryResolveInput, file StorageFileRef) ([]string, error) {
	if _, ok := input.StructureReader.(plugin.RangeReadableProvider); !ok {
		return nil, fmt.Errorf("model structure requires native range access")
	}
	target := resolveCatalogPath(input.EngineID, file.Path, input.EngineCatalogPathFor)
	if !reflect.DeepEqual(target, plugin.FileItemPath(input.EngineID, file.Path)) {
		return nil, fmt.Errorf("model structure requires the exact typed primary file")
	}
	provider, err := format.GetModel3DCompositionProvider(format.Format3DS)
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
	return provider.ReadModel3DComposition(ctx, contentio.NewReaderAt(ctx, reader.(contentio.RangeReader), contentio.NewRef(file.Path, "model")), file.Size)
}
