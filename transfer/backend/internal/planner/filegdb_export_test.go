package planner

import (
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestMySQLSourceCanPlanFileGDBArchiveWithoutReprojection(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Source.Locator = tableLocator(1, "business", "projected_locations")
	spec.Target.ParentLocator = "addp-infra://object/manager/tenant_7/export/session?type=prefix"
	spec.Target.Format = format.FormatFileGDB
	spec.Target.Name = "locations.gdb.zip"
	spatial := datatype.NewSingleGeometrySpatialInfo("location", "Point", 3857, 2)
	spatial.GeometryColumns[0].CRSRef = "EPSG:3857"
	spec.Source.Attributes = tableSourceAttributes("single", "table", "business/projected_locations", nil,
		[]map[string]interface{}{{"name": "location", "type": "geometry"}}, datatype.SpatialInfoPayload(spatial))
	resolver := NewHybridEngineResolver(StaticEngineResolver{1: nativeTableSourceBinding("mysql")},
		NewInfraEngineResolver(InfraEngineConfig{ObjectArea: testInfraArea(t)}))

	build, err := BuildTableTransferPlan(spec, resolver)
	if err != nil {
		t.Fatal(err)
	}
	source := build.Plan.Source
	segments := source.Path.Segments
	if len(segments) != 3 || segments[1].Term != engineplugin.EngineCatalogTermDatabase ||
		segments[1].Name != "business" || segments[2].Name != "projected_locations" {
		t.Fatalf("MySQL source must address the exact database and table: %#v", source.Path)
	}
	if source.SpatialInfo == nil || source.SpatialInfo.PrimaryGeometryName() != "location" ||
		source.SpatialInfo.PrimarySRIDValue() != 3857 || source.SpatialInfo.PrimaryCRSRef() != "EPSG:3857" ||
		source.ReadOptions[engineplugin.TableReadHintGeometryEncoding] != "ewkb" ||
		source.ReadOptions[engineplugin.TableReadHintGeometryTargetSRID] != nil ||
		len(build.Plan.Transforms) != 0 || !build.Plan.Target.IsFileGDBArchive() {
		t.Fatalf("FileGDB must plan the source geometry and CRS unchanged: %+v", build.Plan)
	}
}

func TestFileGDBArchiveUsesExactInfraObjectAndNativeCRS(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.ParentLocator = "addp-infra://object/manager/tenant_7/export/session?type=prefix"
	spec.Target.Format = format.FormatFileGDB
	spec.Target.Name = "roads.gdb.zip"
	spatial := datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 3857, 2)
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{{"name": "route_shape", "type": "geometry"}}, datatype.SpatialInfoPayload(spatial))
	resolver := NewHybridEngineResolver(StaticEngineResolver{1: nativeTableSourceBinding("postgresql")},
		NewInfraEngineResolver(InfraEngineConfig{ObjectArea: testInfraArea(t)}))
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
