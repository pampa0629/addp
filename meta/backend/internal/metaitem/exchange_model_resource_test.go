package metaitem

import (
	"context"
	"encoding/binary"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func exchange3DSChunk(id uint16, body []byte) []byte {
	head := make([]byte, 6)
	binary.LittleEndian.PutUint16(head, id)
	binary.LittleEndian.PutUint32(head[2:], uint32(6+len(body)))
	return append(head, body...)
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
