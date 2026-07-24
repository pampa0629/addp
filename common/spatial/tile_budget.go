package spatial

import "math"

const maxTileEstimateZoom = 30

// EstimateWebMercatorQuadTileCount returns the rectangular WebMercatorQuad
// candidate tile count covered by extent at a single zoom level.
func EstimateWebMercatorQuadTileCount(extent [4]float64, extentSRID, zoom int) (int64, bool) {
	if zoom < 0 || zoom > maxTileEstimateZoom || !validTileEstimateExtent(extent) {
		return 0, false
	}
	if extentSRID == 0 {
		extentSRID = SRIDWGS84
	}
	if !CanEstimateZoomFromExtentSRID(extentSRID) {
		return 0, false
	}

	minLon, minLat, maxLon, maxLat := extent[0], extent[1], extent[2], extent[3]
	if extentSRID != SRIDWGS84 {
		minLon, minLat, maxLon, maxLat = transformToGeographic(minLon, minLat, maxLon, maxLat, extentSRID)
	}
	if !validTileEstimateExtent([4]float64{minLon, minLat, maxLon, maxLat}) {
		return 0, false
	}

	minTileX, maxTileY := lonLatToTile(minLon, minLat, zoom)
	maxTileX, minTileY := lonLatToTile(maxLon, maxLat, zoom)
	countX := int64(maxTileX - minTileX + 1)
	countY := int64(maxTileY - minTileY + 1)
	if countX <= 0 || countY <= 0 {
		return 0, false
	}
	return countX * countY, true
}

// EstimateWebMercatorQuadTileRange returns the cumulative candidate tile count
// for every zoom level in the inclusive range.
func EstimateWebMercatorQuadTileRange(extent [4]float64, extentSRID, minZoom, maxZoom int) (int64, bool) {
	if minZoom < 0 || maxZoom < minZoom || maxZoom > maxTileEstimateZoom {
		return 0, false
	}
	var total int64
	for zoom := minZoom; zoom <= maxZoom; zoom++ {
		count, ok := EstimateWebMercatorQuadTileCount(extent, extentSRID, zoom)
		if !ok {
			return 0, false
		}
		total += count
	}
	return total, true
}

// RecommendMaxZoomByTileBudget selects the highest zoom whose cumulative
// rectangular candidate tile count stays within budget. When the minimum zoom
// alone exceeds budget, it remains the recommendation because lowering it
// would no longer honor the requested visible range.
func RecommendMaxZoomByTileBudget(extent [4]float64, extentSRID, minZoom, maxZoom int, budget int64) (int, int64, bool) {
	if budget <= 0 || minZoom < 0 || maxZoom < minZoom || maxZoom > maxTileEstimateZoom {
		return 0, 0, false
	}

	recommended := minZoom
	var total int64
	for zoom := minZoom; zoom <= maxZoom; zoom++ {
		count, ok := EstimateWebMercatorQuadTileCount(extent, extentSRID, zoom)
		if !ok {
			return 0, 0, false
		}
		if zoom > minZoom && total+count > budget {
			break
		}
		total += count
		recommended = zoom
		if total > budget {
			break
		}
	}
	return recommended, total, true
}

func validTileEstimateExtent(extent [4]float64) bool {
	for _, value := range extent {
		if math.IsNaN(value) || math.IsInf(value, 0) {
			return false
		}
	}
	return extent[0] < extent[2] && extent[1] < extent[3]
}
