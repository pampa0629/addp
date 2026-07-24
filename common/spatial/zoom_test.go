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

func TestRecommendMaxZoomByTileBudgetForFarmlandExtent(t *testing.T) {
	t.Parallel()

	extent := [4]float64{108.55648171959794, 24.52585476646484, 114.3433679860587, 30.244050172136756}
	maxZoom, tileCount, ok := RecommendMaxZoomByTileBudget(extent, SRIDWGS84, 4, 18, 10_000)
	if !ok {
		t.Fatal("RecommendMaxZoomByTileBudget() ok = false, want true")
	}
	if maxZoom != 12 {
		t.Fatalf("RecommendMaxZoomByTileBudget() maxZoom = %d, want 12", maxZoom)
	}
	if tileCount != 6_751 {
		t.Fatalf("RecommendMaxZoomByTileBudget() tileCount = %d, want 6751", tileCount)
	}
}

func TestEstimateWebMercatorQuadTileCountForFarmlandExtent(t *testing.T) {
	t.Parallel()

	extent := [4]float64{108.55648171959794, 24.52585476646484, 114.3433679860587, 30.244050172136756}
	tileCount, ok := EstimateWebMercatorQuadTileCount(extent, SRIDWGS84, 13)
	if !ok {
		t.Fatal("EstimateWebMercatorQuadTileCount() ok = false, want true")
	}
	if tileCount != 19_536 {
		t.Fatalf("EstimateWebMercatorQuadTileCount() = %d, want 19536", tileCount)
	}
}
