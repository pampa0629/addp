package models

import (
	"testing"

	"github.com/addp/common/datatype"
	commonJSON "github.com/addp/common/jsonmap"
)

func TestQueryServiceReadsSpatialFactsOnlyFromSourceSnapshot(t *testing.T) {
	t.Parallel()

	srid := 32650
	snapshot := &QueryServiceDependencySnapshot{
		Table: &datatype.TableInfo{PrimaryKey: []string{"gid"}},
		Spatial: &datatype.SpatialInfo{
			GeometryColumns: []datatype.GeometryColumnInfo{{
				Name:   "custom_shape",
				SRID:   &srid,
				CRSRef: "EPSG:32650",
			}},
			PrimaryGeometryColumn: "custom_shape",
		},
	}
	service := &QueryService{DataConfig: JSONB{
		QueryServiceSourceSnapshotKey: commonJSON.MapFromStruct(snapshot),
		"stable_key":                  []interface{}{"gid"},
	}}

	if !service.HasGeometry() || service.GetGeometryColumn() != "custom_shape" || service.GetSRID() != 32650 {
		t.Fatalf("spatial helpers returned unexpected values")
	}
	stableKey := service.GetStableKey()
	if len(stableKey) != 1 || stableKey[0] != "gid" {
		t.Fatalf("stable key = %#v, want [gid]", stableKey)
	}
}

func TestQueryServiceDoesNotReadLegacyGeometryField(t *testing.T) {
	t.Parallel()

	service := &QueryService{DataConfig: JSONB{
		"geometry": map[string]interface{}{"has_geometry": true, "column": "geom", "srid": 4326},
	}}
	if service.HasGeometry() || service.GetGeometryColumn() != "" || service.GetSRID() != 0 {
		t.Fatal("legacy geometry field must not be consumed")
	}
}
