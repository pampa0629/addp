package mysql

import (
	"testing"

	"github.com/twpayne/go-geom"
)

func TestMySQLGeometryCentroidUsesHighestDimensionInCollection(t *testing.T) {
	collection := geom.NewGeometryCollection().MustPush(
		geom.NewPointFlat(geom.XY, []float64{100, 100}),
		geom.NewPolygonFlat(geom.XY, []float64{0, 0, 0, 2, 2, 2, 2, 0, 0, 0}, []int{10}),
	)
	centroid, err := mysqlGeometryCentroid(collection)
	if err != nil {
		t.Fatal(err)
	}
	if centroid.X() != 1 || centroid.Y() != 1 {
		t.Fatalf("centroid = %v, want [1 1] from polygon dimension", centroid)
	}
}
