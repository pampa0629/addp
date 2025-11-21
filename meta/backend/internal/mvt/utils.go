package mvt

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"math"
)

// lonLatToTileXY 将经纬度转换为瓦片坐标
// lng: 经度 (-180 to 180)
// lat: 纬度 (-85.0511 to 85.0511, Web Mercator 范围)
// zoom: 缩放级别 (0-22)
// 返回: x, y 瓦片坐标
func lonLatToTileXY(lng, lat float64, zoom int) (int, int) {
	// 限制纬度范围（Web Mercator 投影范围）
	if lat > 85.0511 {
		lat = 85.0511
	}
	if lat < -85.0511 {
		lat = -85.0511
	}

	// 计算 x 坐标
	n := math.Pow(2, float64(zoom))
	x := int(math.Floor((lng + 180.0) / 360.0 * n))

	// 计算 y 坐标
	latRad := lat * math.Pi / 180.0
	y := int(math.Floor((1.0 - math.Log(math.Tan(latRad)+1.0/math.Cos(latRad))/math.Pi) / 2.0 * n))

	// 边界检查
	maxTile := int(n) - 1
	if x < 0 {
		x = 0
	}
	if x > maxTile {
		x = maxTile
	}
	if y < 0 {
		y = 0
	}
	if y > maxTile {
		y = maxTile
	}

	return x, y
}

// tileXYToLonLat 将瓦片坐标转换为经纬度（瓦片左上角）
func tileXYToLonLat(x, y, zoom int) (float64, float64) {
	n := math.Pow(2, float64(zoom))
	lng := float64(x)/n*360.0 - 180.0
	latRad := math.Atan(math.Sinh(math.Pi * (1 - 2*float64(y)/n)))
	lat := latRad * 180.0 / math.Pi
	return lng, lat
}

// calculateTileBounds 计算给定范围在指定缩放级别的瓦片边界
// extent: [minLng, minLat, maxLng, maxLat]
// zoom: 缩放级别
// 返回: minX, minY, maxX, maxY
func calculateTileBounds(extent []float64, zoom int) (int, int, int, int) {
	if len(extent) != 4 {
		return 0, 0, 0, 0
	}

	minX, maxY := lonLatToTileXY(extent[0], extent[1], zoom) // 左下角
	maxX, minY := lonLatToTileXY(extent[2], extent[3], zoom) // 右上角

	return minX, minY, maxX, maxY
}

// calculateTotalTiles 计算给定范围在指定缩放级别的瓦片总数
func calculateTotalTiles(extent []float64, zoom int) int {
	minX, minY, maxX, maxY := calculateTileBounds(extent, zoom)
	return (maxX - minX + 1) * (maxY - minY + 1)
}

// gzipCompress 压缩数据为 gzip 格式
func gzipCompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)

	if _, err := gz.Write(data); err != nil {
		return nil, fmt.Errorf("gzip write error: %w", err)
	}

	if err := gz.Close(); err != nil {
		return nil, fmt.Errorf("gzip close error: %w", err)
	}

	return buf.Bytes(), nil
}

// gzipDecompress 解压缩 gzip 数据
func gzipDecompress(data []byte) ([]byte, error) {
	if len(data) == 0 {
		return []byte{}, nil
	}

	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("gzip reader error: %w", err)
	}
	defer gz.Close()

	var buf bytes.Buffer
	if _, err := buf.ReadFrom(gz); err != nil {
		return nil, fmt.Errorf("gzip read error: %w", err)
	}

	return buf.Bytes(), nil
}

// bytesToKB 字节转换为 KB
func bytesToKB(bytes int64) float64 {
	return float64(bytes) / 1024.0
}

// msToSeconds 毫秒转换为秒
func msToSeconds(ms float64) float64 {
	return ms / 1000.0
}
