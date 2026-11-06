package planner

import (
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestFileGDBArchiveUsesExactInfraObjectAndNativeCRS(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.ParentLocator = "addp-infra://minio/manager/tenant_7/export/session?type=prefix"
	spec.Target.Format = format.FormatFileGDB
	spec.Target.Name = "roads.gdb.zip"
	spatial := datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 3857, 2)
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{{"name": "route_shape", "type": "geometry"}}, datatype.SpatialInfoPayload(spatial))
	resolver := NewHybridEngineResolver(StaticEngineResolver{1: nativeTableSourceBinding("postgresql")},
		NewInfraEngineResolver(InfraEngineConfig{MinioEndpoint: "localhost:9000"}))
	build, err := BuildTableTransferPlan(spec, resolver)
	if err != nil {
		t.Fatal(err)
	}
	if got := build.Plan.Target.Path.StringPath(); got != "manager/tenant_7/export/session/roads.gdb.zip" {
		t.Fatalf("target = %q", got)
	}
	hints := build.Plan.Source.ReadOptions
	if hints[engineplugin.TableReadHintGeometryEncoding] != "ewkb" ||
		hints[engineplugin.TableReadHintGeometryTargetSRID] != nil || len(build.Plan.Transforms) != 0 {
		t.Fatalf("FileGDB must read original EWKB: hints=%v transforms=%v", hints, build.Plan.Transforms)
	}
	spec.Target.Name = "roads.gdb"
	if _, err := BuildTableTransferPlan(spec, resolver); err == nil {
		t.Fatal("accepted FileGDB directory for an Infra archive")
	}
}
