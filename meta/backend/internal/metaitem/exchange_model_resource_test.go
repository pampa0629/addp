package metaitem

import (
	"context"
	"encoding/binary"
	"reflect"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
	"github.com/addp/meta/internal/metaattr"
)

func exchange3DSChunk(id uint16, body []byte) []byte {
	head := make([]byte, 6)
	binary.LittleEndian.PutUint16(head, id)
	binary.LittleEndian.PutUint32(head[2:], uint32(6+len(body)))
	return append(head, body...)
}

func TestExchangeModelBasicAttributesKeepCompleteDeclaredComposition(t *testing.T) {
	for _, formatName := range []string{"dae", "3ds"} {
		for _, ref := range []string{"textures/color.png", "missing.png", ""} {
			t.Run(formatName+"/"+ref, func(t *testing.T) {
				primary := "models/model." + formatName
				input := DirectoryResolveInput{DirPath: "models", Files: []StorageFileRef{{Name: "model." + formatName, Path: primary, Size: 100}, {Name: "color.png", Path: "models/textures/color.png", Size: 25}}, Options: ResolveOptions{IncludeSingleResources: true}, ContentReader: refMapContentReader{content: map[string][]byte{primary: exchangeSource(formatName, ref)}}}
				result, err := (&commonDataItemResolver{}).ResolveItems(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				var model *DetectedItem
				for _, item := range result.Items {
					if item.PrimaryContentPath == primary {
						model = item
					}
				}
				if model == nil {
					t.Fatal("model was not identified")
				}
				attrs := metaattr.BuildAttributes(AttributeInput(model))
				nativeFormats, ok := attrs["format_info"].(map[string]interface{})
				if !ok {
					t.Fatalf("basic composition declaration missing: %#v", attrs)
				}
				native, ok := nativeFormats[formatName].(map[string]interface{})
				if !ok || native["scan_complete"] != true {
					t.Fatalf("incomplete declaration: %#v", nativeFormats)
				}
				refs, ok := native["texture_refs"].([]string)
				if !ok || refs == nil || (ref == "" && len(refs) != 0) || (ref != "" && (len(refs) != 1 || refs[0] != ref)) {
					t.Fatalf("missing or empty declaration was confused: %#v", native)
				}
				item := attrs["item"].(map[string]interface{})
				want := 1
				if ref == "textures/color.png" {
					want = 2
				}
				persistedRefs, ok := item["refs"].([]map[string]interface{})
				if item["layout"] != "single" || !ok || len(persistedRefs) != want {
					t.Fatalf("exact single composition lost: %#v", item)
				}
				primaryCount := 0
				for _, persisted := range persistedRefs {
					if persisted["required"] != true {
						t.Fatalf("composition ref became optional: %#v", persisted)
					}
					if persisted["primary"] == true {
						primaryCount++
						if persisted["path"] != primary || persisted["role"] != "model" {
							t.Fatalf("invalid primary: %#v", persisted)
						}
					}
				}
				if primaryCount != 1 {
					t.Fatalf("expected one primary, got %d", primaryCount)
				}
				if attrs["storage"].(map[string]interface{})["physical_path"] != primary {
					t.Fatal("model entrypoint became its parent directory")
				}
				if _, found := attrs["type_info"]; found {
					t.Fatal("basic composition read invented deep model statistics")
				}
			})
		}
	}
}

func TestExchangeModelUnreadableCompositionIsNotAnEmptyDeclaration(t *testing.T) {
	for _, formatName := range []string{"dae", "3ds"} {
		for _, raw := range [][]byte{nil, []byte("truncated model")} {
			t.Run(formatName+"/"+string(raw), func(t *testing.T) {
				primary := "models/model." + formatName
				result, err := (&commonDataItemResolver{}).ResolveItems(context.Background(), DirectoryResolveInput{
					DirPath: "models", Files: []StorageFileRef{{Name: "model." + formatName, Path: primary, Size: int64(len(raw))}},
					Options: ResolveOptions{IncludeSingleResources: true}, ContentReader: refMapContentReader{content: map[string][]byte{primary: raw}},
				})
				if err != nil {
					t.Fatal(err)
				}
				found := false
				for _, item := range result.Items {
					if item.PrimaryContentPath == primary {
						found = true
						attrs := metaattr.BuildAttributes(AttributeInput(item))
						if native, ok := attrs["format_info"].(map[string]interface{}); ok && native[formatName] != nil {
							t.Fatalf("unreadable composition invented a complete declaration: %#v", native)
						}
					}
				}
				if !found {
					t.Fatal("unreadable model identity was lost")
				}
			})
		}
	}
}

func TestExchangeModelCompositionRolesIgnoreDeclarationOrder(t *testing.T) {
	primary := "models/model.3ds"
	resolve := func(refs []string) []*DetectedItem {
		var maps []byte
		for _, ref := range refs {
			maps = append(maps, exchange3DSChunk(0xa200, exchange3DSChunk(0xa300, []byte(ref+"\x00")))...)
		}
		input := DirectoryResolveInput{DirPath: "models", ContentReader: refMapContentReader{content: map[string][]byte{
			primary: exchange3DSChunk(0x4d4d, exchange3DSChunk(0x3d3d, exchange3DSChunk(0xafff, maps))),
		}}}
		return resolveExchangeModelResourceItems(context.Background(), input, []StorageFileRef{
			{Name: "model.3ds", Path: primary}, {Name: "a.png", Path: "models/a.png"}, {Name: "b.png", Path: "models/b.png"},
		})
	}
	a, b := resolve([]string{"a.png", "b.png"}), resolve([]string{"b.png", "a.png"})
	if len(a) != 1 || len(b) != 1 || len(a[0].RefList) != 3 || !reflect.DeepEqual(a[0].RefList, b[0].RefList) {
		t.Fatalf("reference declaration order changed composition roles: %+v / %+v", a, b)
	}
}

func exchangeSource(formatName, ref string) []byte {
	if formatName == "dae" {
		return []byte(`<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1"><library_images><image><init_from>` + ref + `</init_from></image></library_images></COLLADA>`)
	}
	return exchange3DSChunk(0x4d4d, exchange3DSChunk(0x3d3d, exchange3DSChunk(0xafff, exchange3DSChunk(0xa200, exchange3DSChunk(0xa300, []byte(ref+"\x00"))))))
}
func TestExchangeModelClaimsOnlyExactDeclaredTextures(t *testing.T) {
	for _, formatName := range []string{"dae", "3ds"} {
		for _, ref := range []string{"textures/color.png", "textures/Color.PNG", "missing.png", "../outside.png", "https://example.test/color.png"} {
			t.Run(formatName+"/"+ref, func(t *testing.T) {
				primary := "models/model." + formatName
				input := DirectoryResolveInput{DirPath: "models", Files: []StorageFileRef{{Name: "model." + formatName, Path: primary, Size: 100}, {Name: "color.png", Path: "models/textures/color.png", Size: 25}, {Name: "other.png", Path: "models/other.png", Size: 30}}, Options: ResolveOptions{IncludeSingleResources: true}, ContentReader: refMapContentReader{content: map[string][]byte{primary: exchangeSource(formatName, ref)}}}
				result, err := (&commonDataItemResolver{}).ResolveItems(context.Background(), input)
				if err != nil {
					t.Fatal(err)
				}
				var model *DetectedItem
				for _, item := range result.Items {
					if item.PrimaryContentPath == primary {
						model = item
					}
				}
				if model == nil || model.DataType != datatype.Model3D || model.Format != formatName || model.Layout != format.LayoutSingle {
					t.Fatalf("model=%+v", model)
				}
				want := ref == "textures/color.png"
				if result.Claims["models/textures/color.png"] != want {
					t.Fatalf("claims=%v", result.Claims)
				}
				if want && (len(model.RefList) != 2 || model.SizeBytes == nil || *model.SizeBytes != 125) {
					t.Fatalf("refs and total=%+v", model)
				}
				// Single images can claim themselves, so check the model's refs rather than global claims.
				for _, related := range model.RefList {
					if related.Path == "models/other.png" {
						t.Fatal("unrelated image included")
					}
				}
			})
		}
	}
}
