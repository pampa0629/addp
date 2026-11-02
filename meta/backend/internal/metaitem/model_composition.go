package metaitem

import (
	"context"
	"path"
	"sort"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/datatype"
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
	reader, err := fileStructureReaderAt(ctx, input, file)
	if err != nil {
		return nil, err
	}
	provider, err := format.GetModel3DCompositionProvider(formatType)
	if err != nil {
		return nil, err
	}
	return provider.ReadModel3DComposition(ctx, reader, file.Size)
}
