package spatial

import (
	"math"
)

// CalculateMaxZoomByRecordCount 根据记录数计算最大缩放级别
// 目标：确保在 maxZoom 层级，每个瓦片的平均记录数低于 targetRecordsPerTile
//
// 参数：
//   - recordCount: 表的总记录数
//   - targetRecordsPerTile: 单个瓦片的目标记录数（阈值），推荐 1000
//   - extent: 数据范围 [minX, minY, maxX, maxY]
//   - srid: 空间参考系 ID
//
// 返回：
//   - 计算出的最大缩放级别（最大不超过 18）
func CalculateMaxZoomByRecordCount(recordCount int64, targetRecordsPerTile int, extent [4]float64, srid int) int {
	// 1. 边界情况
	if recordCount <= 0 {
		return 18 // 无数据或未知，返回默认最大值
	}

	if recordCount < int64(targetRecordsPerTile) {
		// 数据量很小，直接返回最大 zoom
		return 18
	}

	// 2. 计算数据范围的面积（平方度）
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]

	// 验证 extent 有效性
	if minX >= maxX || minY >= maxY {
		return 18 // 无效范围，返回默认值
	}

	// 3. 计算起始 zoom（从 minZoom 开始更合理）
	minZoom := CalculateMinZoomFromExtent(extent[:], srid)

	// 4. 从 minZoom 开始，逐层计算每瓦片平均记录数
	for z := minZoom; z <= 18; z++ {
		// 计算该层级覆盖数据范围的瓦片数
		tileCount := calculateTileCountAtZoom(extent, z, srid)

		if tileCount == 0 {
			continue // 跳过无效层级
		}

		// 估算每个瓦片的平均记录数
		avgRecordsPerTile := float64(recordCount) / float64(tileCount)

		// 如果平均记录数低于阈值，这就是 maxZoom
		if avgRecordsPerTile < float64(targetRecordsPerTile) {
			return z
		}
	}

	// 5. 如果所有层级都超过阈值，返回最大值
	return 18
}

// calculateTileCountAtZoom 计算某层级覆盖 extent 的瓦片数量
func calculateTileCountAtZoom(extent [4]float64, z int, srid int) int {
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]

	// 转换为 Web Mercator（如果需要）
	if srid == 4326 {
		minX, minY = lonLatToWebMercator(minX, minY)
		maxX, maxY = lonLatToWebMercator(maxX, maxY)
	}

	// 转换为瓦片坐标
	minTileX, maxTileY := webMercatorToTile(minX, minY, z)
	maxTileX, minTileY := webMercatorToTile(maxX, maxY, z)

	// 计算瓦片数量（包含边界）
	tileCountX := maxTileX - minTileX + 1
	tileCountY := maxTileY - minTileY + 1

	if tileCountX <= 0 || tileCountY <= 0 {
		return 0
	}

	return tileCountX * tileCountY
}

// lonLatToWebMercator 将经纬度转换为 Web Mercator 坐标
func lonLatToWebMercator(lon, lat float64) (x, y float64) {
	const earthRadius = 20037508.34 // Web Mercator 半径（米）

	x = (lon * earthRadius) / 180.0

	latRad := lat * math.Pi / 180.0
	y = math.Log(math.Tan((math.Pi/4.0)+(latRad/2.0))) * earthRadius / math.Pi

	return x, y
}

// webMercatorToTile 将 Web Mercator 坐标转换为瓦片坐标
func webMercatorToTile(x, y float64, z int) (tileX, tileY int) {
	const worldSize = 20037508.34 * 2.0 // Web Mercator 世界大小

	// 归一化到 [0, 1]
	normX := (x + 20037508.34) / worldSize
	normY := (20037508.34 - y) / worldSize

	// 转换为瓦片坐标
	numTiles := float64(int(1) << uint(z)) // 2^z
	tileX = int(math.Floor(normX * numTiles))
	tileY = int(math.Floor(normY * numTiles))

	// 边界检查
	maxTile := (int(1) << uint(z)) - 1
	if tileX < 0 {
		tileX = 0
	}
	if tileX > maxTile {
		tileX = maxTile
	}
	if tileY < 0 {
		tileY = 0
	}
	if tileY > maxTile {
		tileY = maxTile
	}

	return tileX, tileY
}

// lonLatToTile 将经纬度直接转换为瓦片坐标（便捷函数）
func lonLatToTile(lon, lat float64, z int) (tileX, tileY int) {
	x, y := lonLatToWebMercator(lon, lat)
	return webMercatorToTile(x, y, z)
}

// tileToLonLatBounds 将瓦片坐标转换为经纬度范围
func tileToLonLatBounds(tileX, tileY, z int) [4]float64 {
	numTiles := float64(int(1) << uint(z))

	// 左上角
	normX1 := float64(tileX) / numTiles
	normY1 := float64(tileY) / numTiles

	// 右下角
	normX2 := float64(tileX+1) / numTiles
	normY2 := float64(tileY+1) / numTiles

	// Web Mercator 坐标
	const worldSize = 20037508.34 * 2.0
	webX1 := normX1*worldSize - 20037508.34
	webY1 := 20037508.34 - normY1*worldSize
	webX2 := normX2*worldSize - 20037508.34
	webY2 := 20037508.34 - normY2*worldSize

	// 转换为经纬度
	minLon := webX1 * 180.0 / 20037508.34
	maxLon := webX2 * 180.0 / 20037508.34

	minLat := (2.0*math.Atan(math.Exp(webY2*math.Pi/20037508.34)) - math.Pi/2.0) * 180.0 / math.Pi
	maxLat := (2.0*math.Atan(math.Exp(webY1*math.Pi/20037508.34)) - math.Pi/2.0) * 180.0 / math.Pi

	return [4]float64{minLon, minLat, maxLon, maxLat}
}

// boundsIntersect 判断两个矩形边界是否有交集
func boundsIntersect(bounds1, bounds2 [4]float64) bool {
	// bounds: [minX, minY, maxX, maxY]
	return !(bounds1[2] < bounds2[0] || // bounds1 在 bounds2 左边
		bounds1[0] > bounds2[2] || // bounds1 在 bounds2 右边
		bounds1[3] < bounds2[1] || // bounds1 在 bounds2 下边
		bounds1[1] > bounds2[3]) // bounds1 在 bounds2 上边
}

// CalculateTilesForZoom 计算某层级需要生成的瓦片列表（带交集检测）
func CalculateTilesForZoom(z int, extent [4]float64, srid int) [][3]int {
	minX, minY, maxX, maxY := extent[0], extent[1], extent[2], extent[3]

	// 转换为瓦片坐标
	minTileX, maxTileY := lonLatToTile(minX, minY, z)
	maxTileX, minTileY := lonLatToTile(maxX, maxY, z)

	tiles := make([][3]int, 0)

	// 遍历所有可能的瓦片（包含边界）
	for x := minTileX; x <= maxTileX; x++ {
		for y := minTileY; y <= maxTileY; y++ {
			// 计算瓦片的地理范围
			tileBounds := tileToLonLatBounds(x, y, z)

			// 判断瓦片与数据 extent 是否有交集
			if boundsIntersect(tileBounds, extent) {
				tiles = append(tiles, [3]int{z, x, y})
			}
		}
	}

	return tiles
}
