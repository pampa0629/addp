package spatial

import "testing"

func TestCalculateMinZoomFromCGCS20003DegreeGaussKrugerExtent(t *testing.T) {
	t.Parallel()

	extent := []float64{570841.0277000004, 3404864.0396999996, 598936.5142999999, 3434951.8803000003}

	if got := CalculateMinZoomFromExtent(extent, 4549); got != 9 {
		t.Fatalf("CalculateMinZoomFromExtent() = %d, want 9 for CGCS2000 3-degree GK extent", got)
	}
	if !CanEstimateZoomFromExtentSRID(4549) {
		t.Fatal("CanEstimateZoomFromExtentSRID(4549) = false, want true")
	}
}
