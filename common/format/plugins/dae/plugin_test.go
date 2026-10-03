package dae

import (
	"context"
	"strings"
	"testing"
)

const validDAE = `<COLLADA xmlns="http://www.collada.org/2005/11/COLLADASchema" version="1.4.1"><asset><unit meter="0.01"/><up_axis>Z_UP</up_axis></asset><library_images><image><init_from>textures/color.png</init_from></image></library_images><library_geometries><geometry><mesh/></geometry><geometry><mesh/></geometry></library_geometries><library_materials><material/></library_materials></COLLADA>`

func TestDAESummaryPreservesSourceFacts(t *testing.T) {
	result, err := NewPlugin().DescribeModel3D(context.Background(), strings.NewReader(validDAE), nil)
	if err != nil {
		t.Fatal(err)
	}
	if *result.Model3D.MeshCount != 2 || *result.Model3D.MaterialCount != 1 {
		t.Fatalf("summary=%+v", result.Model3D)
	}
	if result.FormatInfo["unit_meter"] != 0.01 || result.FormatInfo["up_axis"] != "Z_UP" || result.Model3D.Bounds3D != nil {
		t.Fatalf("source facts=%+v", result)
	}
	refs := result.FormatInfo["texture_refs"].([]string)
	if len(refs) != 1 || refs[0] != "textures/color.png" {
		t.Fatalf("refs=%v", refs)
	}
}
func TestDAERejectsInvalidAndOutOfScopeDocuments(t *testing.T) {
	for _, data := range []string{"", `<xml/>`, strings.Replace(validDAE, "1.4.1", "1.5.0", 1), strings.Replace(validDAE, `meter="0.01"`, `meter="NaN"`, 1), validDAE[:len(validDAE)-10], validDAE + validDAE} {
		if _, err := NewPlugin().DescribeModel3D(context.Background(), strings.NewReader(data), nil); err == nil {
			t.Errorf("accepted invalid document %.60s", data)
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewPlugin().DescribeModel3D(ctx, strings.NewReader(validDAE), nil); err != context.Canceled {
		t.Fatalf("err=%v", err)
	}
}
