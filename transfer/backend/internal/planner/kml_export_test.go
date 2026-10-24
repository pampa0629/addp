package planner

import (
	"fmt"
	"testing"

	"github.com/addp/common/datatype"
	engineplugin "github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestKMLExportUsesFormatCRSRequirement(t *testing.T) {
	for _, srid := range []int{4326, 3857, 0} {
		for _, transform := range []bool{true, false} {
			name := "native"
			if transform {
				name = "native-transform"
			}
			t.Run(fmt.Sprintf("%s-%d", name, srid), func(t *testing.T) {
				spec := minimalNativeToEncodedSpec()
				spec.Target.Format = format.FormatKML
				setFileTarget(&spec, 2, "exports/roads")
				spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{{"name": "route_shape", "type": "geometry"}}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", srid, 2)))
				source := nativeTableSourceBinding("native_table_source")
				if transform {
					source = EngineBinding{Type: "postgresql", Capabilities: &engineplugin.EngineCapabilities{SchemaVersion: engineplugin.CapabilitiesSchemaVersion, EngineType: "postgresql", Storage: &engineplugin.StorageCapabilities{Store: &engineplugin.StoreCapability{TableReadSpatialTransform: true}}}}
				}
				build, err := BuildTableTransferPlan(spec, StaticEngineResolver{1: source, 2: {Type: "nfs"}})
				if srid == 0 || (srid != 4326 && !transform) {
					if err == nil {
						t.Fatal("missing CRS or transform accepted")
					}
					return
				}
				if err != nil {
					t.Fatal(err)
				}
				if build.Plan.Target.Path.StringPath() != "exports/roads.kml" {
					t.Fatal(build.Plan.Target.Path)
				}
				hints := build.Plan.Source.ReadOptions
				if hints[engineplugin.TableReadHintGeometryEncoding] != "ewkb" {
					t.Fatal(hints)
				}
				if transform && (hints[engineplugin.TableReadHintGeometryTargetSRID] != 4326 || hints[engineplugin.TableReadHintGeometryField] != "route_shape") {
					t.Fatal(hints)
				}
				if len(build.Plan.Transforms) != 0 {
					t.Fatal(build.Plan.Transforms)
				}
			})
		}
	}
}
func TestKMLEncodedReprojectionUsesSharedPlan(t *testing.T) {
	spec := minimalEncodedToEncodedSpec()
	spec.Target.Format = format.FormatKML
	setFileTarget(&spec, 2, "exports/roads")
	spec.Source.Format = format.FormatGeoJSON
	spec.Source.Attributes = tableSourceAttributes("single", "geojson", "imports/roads.geojson", nil, []map[string]interface{}{{"name": "route_shape", "type": "geometry"}}, datatype.SpatialInfoPayload(datatype.NewSingleGeometrySpatialInfo("route_shape", "Point", 3857, 2)))
	build, err := BuildTableTransferPlan(spec, StaticEngineResolver{1: {Type: "nfs"}, 2: {Type: "nfs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(build.Plan.Transforms) != 1 || build.Plan.Transforms[0].SpatialReproject.SourceCRS != "EPSG:3857" || build.Plan.Transforms[0].SpatialReproject.TargetCRS != "EPSG:4326" {
		t.Fatal(build.Plan.Transforms)
	}
	if build.Plan.Source.ParseOptions.GeometryEncoding != format.GeometryEncodingEWKB {
		t.Fatal(build.Plan.Source.ParseOptions)
	}
}

func TestKMLNativeCRS84DoesNotRequireCoordinateTransform(t *testing.T) {
	spec := minimalNativeToEncodedSpec()
	spec.Target.Format = format.FormatKML
	setFileTarget(&spec, 2, "exports/roads")
	info := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 0, 2)
	info.GeometryColumns[0].CRSRef = "OGC:CRS84"
	spec.Source.Attributes = tableSourceAttributes("single", "table", "public/roads", nil, []map[string]interface{}{{"name": "shape", "type": "geometry"}}, datatype.SpatialInfoPayload(info))
	build, err := BuildTableTransferPlan(spec, StaticEngineResolver{1: nativeTableSourceBinding("native_table_source"), 2: {Type: "nfs"}})
	if err != nil {
		t.Fatal(err)
	}
	if len(build.Plan.Transforms) != 0 {
		t.Fatal(build.Plan.Transforms)
	}
}
