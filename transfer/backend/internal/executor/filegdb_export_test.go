package executor

import (
	"testing"

	"github.com/addp/common/datatype"
)

func TestFileGDBUsesLiveXYAndPreservesCatalogCRS(t *testing.T) {
	catalog := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 3857, 0)
	catalog.GeometryColumns[0].CRSRef = "EPSG:3857"
	catalog.CRSDefinitions = []datatype.CRSDefinition{{ID: "EPSG:3857", DefinitionEncoding: "wkt", Definition: "fixture"}}
	current := datatype.NewSingleGeometrySpatialInfo("shape", "Point", 3857, 2)
	got, err := verifiedFileGDBSourceSpatial(catalog, current)
	if err != nil || got.PrimaryDimensionValue() != 2 ||
		len(got.CRSDefinitions) != 1 || got.CRSDefinitions[0] != catalog.CRSDefinitions[0] ||
		catalog.PrimaryDimensionValue() != 0 {
		t.Fatalf("FileGDB source spatial = %+v, %v", got, err)
	}

	for name, spatial := range map[string]*datatype.SpatialInfo{
		"untyped":    datatype.NewSingleGeometrySpatialInfo("shape", "Point", 3857, 0),
		"xyz":        datatype.NewSingleGeometrySpatialInfo("shape", "Point", 3857, 3),
		"new srid":   datatype.NewSingleGeometrySpatialInfo("shape", "Point", 4326, 2),
		"new name":   datatype.NewSingleGeometrySpatialInfo("shape2", "Point", 3857, 2),
		"new family": datatype.NewSingleGeometrySpatialInfo("shape", "LineString", 3857, 2),
		"missing":    nil,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := verifiedFileGDBSourceSpatial(catalog, spatial); err == nil {
				t.Fatal("accepted incompatible live geometry facts")
			}
		})
	}
}
