package envi

import (
	"context"
	"strings"
	"testing"

	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/workflowaccess"
	"github.com/addp/common/format"
)

type inspectionFixture struct {
	engineplugin.WorkflowRuntimeProvider
	info map[string]interface{}
}

func (r inspectionFixture) InvokeOperator(_ context.Context, _ engineplugin.ConnectionInfo, operator string, request engineplugin.OperatorInvokeRequest) (*engineplugin.OperatorInvokeResult, error) {
	if operator != "raster_inspect" || request.TenantID != 7 {
		panic("wrong ENVI inspection contract")
	}
	return &engineplugin.OperatorInvokeResult{Status: "success", Result: map[string]interface{}{"result": map[string]interface{}{"schema_version": "gdal.raster.inspect/v1", "format": "envi", "description": map[string]interface{}{"media": map[string]interface{}{"kind": "image", "encoding": "ENVI", "width": 4, "height": 3}, "format_info": r.info}}}}, nil
}

func TestENVIIdentityNeverClaimsARawExtensionOrHeaderAlone(t *testing.T) {
	d := (&Plugin{}).Descriptor()
	if len(d.Identification.Extensions) != 0 || len(d.Identification.ContentSignatures) != 0 || len(d.Layouts) != 1 || d.Layouts[0] != format.LayoutMulti {
		t.Fatal("ENVI advertised incomplete file detection", d)
	}
}

func TestENVIInspectionRequiresTheSameCompleteHeader(t *testing.T) {
	for _, fault := range []string{"none", "unproven", "wrong profile", "wrong header", "changed header"} {
		t.Run(fault, func(t *testing.T) {
			sha := strings.Repeat("a", 64)
			info := map[string]interface{}{"scan_complete": true, "composition_complete": true, "profile": Profile, "header_name": "grid.HDR", "header_sha256": sha}
			switch fault {
			case "unproven":
				info["composition_complete"] = false
			case "wrong profile":
				info["profile"] = "other"
			case "wrong header":
				info["header_name"] = "another.HDR"
			case "changed header":
				info["header_sha256"] = strings.Repeat("b", 64)
			}
			plan, err := workflowaccess.NewSourcePlan(workflowaccess.Source{Kind: workflowaccess.KindFile, Format: "envi", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/grid.img"}, RelatedFiles: []workflowaccess.RelatedFile{{Role: "header", Access: workflowaccess.Access{Method: workflowaccess.MethodMountedPath, Path: "/data/grid.HDR"}}}, Metadata: map[string]interface{}{"expected_header_sha256": sha}})
			if err != nil {
				t.Fatal(err)
			}
			bound, err := (&Plugin{}).BindMediaInfoProvider(inspectionFixture{info: info}, engineplugin.ConnectionInfo{"host": "runtime"}, 7, plan)
			if err != nil {
				t.Fatal(err)
			}
			_, err = bound.DescribeMedia(t.Context())
			if (err == nil) != (fault == "none") {
				t.Fatalf("wrong inspection acceptance %s: %v", fault, err)
			}
		})
	}
}
