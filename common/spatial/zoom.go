package spatial

import "math"

// CalculateMinZoomFromExtent 根据地理范围计算最小可见 zoom 层级
// extent: [minX, minY, maxX, maxY]
// extentSRID: extent 的坐标系 SRID（如 4326, 3857, 2360 等）
//             如果为 0，默认为 4326（WGS84）
// 返回合适的最小 zoom 层级，使得整个范围能在单个视口内显示
//
// 算法说明：
// 1. 如果 extentSRID 为 0，默认为 4326
// 2. 如果是投影坐标系（如 3857、2360），先转换为地理坐标系（度）
// 3. 计算范围的宽度和高度
// 4. 基于 "数据占据视口 50%" 的目标计算合适的 zoom
// 5. Web Mercator 在 zoom 0 时全球宽度为 360 度，每增加一级 zoom 减半
//
// 示例：
// - 广西省（~5度）→ MinZoom = 7
// - 全国（~60度）→ MinZoom = 3
// - 城市（~0.5度）→ MinZoom = 9
func CalculateMinZoomFromExtent(extent []float64, extentSRID int) int {
	if len(extent) != 4 {
		return 6 // 默认值
	}

	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]

	// 0. 如果 extentSRID 为 0 或未指定，默认为 4326
	if extentSRID == 0 {
		extentSRID = 4326
	}

	// 1. 坐标系转换：将投影坐标转换为地理坐标（度）
	if extentSRID != 4326 {
		minX, minY, maxX, maxY = transformToGeographic(minX, minY, maxX, maxY, extentSRID)
	}

	// 2. 计算范围的宽度和高度（度）
	width := maxX - minX
	height := maxY - minY

	// 使用较大的维度来确定 zoom
	maxDim := math.Max(width, height)

	if maxDim <= 0 {
		return 6
	}

	// 3. 计算 zoom 层级
	// 目标：数据范围占据视口 50% 左右
	// 视口宽度在 zoom z 时为 360/2^z 度
	// 求解：maxDim / (360/2^z) ≈ 0.5
	// 即：2^z ≈ 360 / (2 * maxDim)
	// 即：z ≈ log2(360 / (2 * maxDim))
	targetRatio := 0.5
	zoom := math.Log2(360 / (maxDim / targetRatio))

	// 向下取整，然后 -2，让用户在更低层级也能看到数据
	minZoom := int(math.Floor(zoom)) - 2

	// 4. 限制范围：3-18
	// 下限3：避免过小的zoom导致全球视图
	// 上限18：MVT瓦片系统的最大zoom层级
	if minZoom < 3 {
		minZoom = 3
	}
	if minZoom > 18 {
		minZoom = 18
	}

	return minZoom
}

// transformToGeographic 将投影坐标转换为地理坐标（度）
// 支持常见的投影坐标系：Web Mercator (3857)、中国常用投影（如 2360）
func transformToGeographic(minX, minY, maxX, maxY float64, srid int) (float64, float64, float64, float64) {
	switch srid {
	case 3857:
		// Web Mercator → WGS84
		// 公式：lon = x / 20037508.34 * 180
		//      lat = (atan(exp(y / 20037508.34 * PI)) - PI/4) * 2 / PI * 180
		const R = 20037508.34 // Web Mercator 半径
		minLon := minX / R * 180
		maxLon := maxX / R * 180
		minLat := (math.Atan(math.Exp(minY/R*math.Pi)) - math.Pi/4) * 2 / math.Pi * 180
		maxLat := (math.Atan(math.Exp(maxY/R*math.Pi)) - math.Pi/4) * 2 / math.Pi * 180
		return minLon, minLat, maxLon, maxLat

	case 2360, 2361, 2362, 2363, 2364, 2365, 2366, 2367, 2368, 2369, 2370:
		// 中国常用投影坐标系（单位：米）
		// 简化处理：假设 1 度 ≈ 111,000 米（赤道附近）
		// 注意：这是粗略估算，精确转换需要完整的投影参数
		const metersPerDegree = 111000.0
		minLon := minX / metersPerDegree
		maxLon := maxX / metersPerDegree
		minLat := minY / metersPerDegree
		maxLat := maxY / metersPerDegree
		return minLon, minLat, maxLon, maxLat

	default:
		// 未知坐标系，假设单位是度（可能不准确，但不会崩溃）
		return minX, minY, maxX, maxY
	}
}

// ShouldGenerateTileAtZoom 判断指定 zoom 层级是否应该生成瓦片
// 用于前端和后端统一验证
func ShouldGenerateTileAtZoom(z int, extent []float64, extentSRID int) bool {
	minZoom := CalculateMinZoomFromExtent(extent, extentSRID)
	maxZoom := 18 // 固定最大 zoom

	return z >= minZoom && z <= maxZoom
}
