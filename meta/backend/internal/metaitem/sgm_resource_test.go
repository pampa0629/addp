package metaitem

import (
	"context"
	"testing"

	"github.com/addp/common/datatype"
	"github.com/addp/common/format"
)

func TestSGMUsesGenericSingleResolverWithoutClaimingNearbyTextures(t *testing.T) {
	result, err := (&commonDataItemResolver{}).ResolveItems(context.Background(), DirectoryResolveInput{
		DirPath: "models",
		Files: []StorageFileRef{
			{Name: "compass.SGM", Path: "models/compass.SGM", Size: 100},
			{Name: "texture.png", Path: "models/texture.png", Size: 20},
		},
		Options: ResolveOptions{IncludeSingleResources: true},
	})
	if err != nil || len(result.Items) != 2 {
		t.Fatalf("SGM and nearby texture must remain separate: result=%+v err=%v", result, err)
	}
	var model *DetectedItem
	for _, item := range result.Items {
		if item.PrimaryContentPath == "models/compass.SGM" {
			model = item
		}
	}
	if model == nil || model.Format != string(format.FormatSGM) || model.DataType != datatype.Model3D || model.Layout != format.LayoutSingle {
		t.Fatalf("SGM identity must come from the descriptor: %+v", model)
	}
	if model.Model3D != nil || len(model.RefList) != 0 {
		t.Fatal("descriptor-only identification must not invent geometry or companion references")
	}
	if result.Claims["models/texture.png"] {
		t.Fatal("extension-only identity must not claim an unrelated texture")
	}
}
