package common

import (
	"fmt"
	"math"

	"github.com/addp/service/internal/models"
	"gorm.io/gorm"
)

// TileBBox 瓦片边界框（Web Mercator 坐标）
type TileBBox struct {
	MinX float64
	MinY float64
	MaxX float64
	MaxY float64
	SRID int
}

// TileToBBox 将瓦片坐标转换为 Web Mercator 边界框
// z: 缩放级别 (0-22)
// x: 瓦片 X 坐标
// y: 瓦片 Y 坐标
// 返回值为 EPSG:3857 (Web Mercator) 坐标系下的边界框
func TileToBBox(z, x, y int) *TileBBox {
	n := math.Pow(2, float64(z))

	// 计算经纬度范围
	lonMin := float64(x)/n*360.0 - 180.0
	lonMax := float64(x+1)/n*360.0 - 180.0

	// 计算纬度（使用墨卡托投影公式）
	latMin := math.Atan(math.Sinh(math.Pi*(1-2*float64(y+1)/n))) * 180.0 / math.Pi
	latMax := math.Atan(math.Sinh(math.Pi*(1-2*float64(y)/n))) * 180.0 / math.Pi

	// 转换为 Web Mercator 米制坐标
	// 公式: x_meters = lon * 20037508.34 / 180
	minX := lonMin * 20037508.34 / 180.0
	maxX := lonMax * 20037508.34 / 180.0

	// 公式: y_meters = ln(tan((90+lat)*pi/360)) * 20037508.34 / pi
	minY := math.Log(math.Tan((90+latMin)*math.Pi/360.0)) / (math.Pi/180.0) * 20037508.34 / 180.0
	maxY := math.Log(math.Tan((90+latMax)*math.Pi/360.0)) / (math.Pi/180.0) * 20037508.34 / 180.0

	return &TileBBox{
		MinX: minX,
		MinY: minY,
		MaxX: maxX,
		MaxY: maxY,
		SRID: 3857, // Web Mercator
	}
}

// GenerateMVTTile 生成 MVT 矢量瓦片
// 使用 PostGIS ST_AsMVT 函数动态生成瓦片数据
func GenerateMVTTile(
	db *gorm.DB,
	layer *models.InternalServiceLayer,
	z, x, y int,
) ([]byte, error) {
	// 检查图层是否有几何列
	if layer.GeometryColumn == "" {
		return nil, fmt.Errorf("layer does not have geometry column")
	}

	// 计算瓦片边界框
	bbox := TileToBBox(z, x, y)

	// 获取 MVT 配置参数
	mvtExtent := 4096
	if layer.MVTExtent > 0 {
		mvtExtent = layer.MVTExtent
	}

	mvtBuffer := 256
	if layer.MVTBuffer >= 0 {
		mvtBuffer = layer.MVTBuffer
	}

	// 构建属性列表（从 FilterColumns）
	columns := layer.FilterColumns

	// 构建 SELECT 列表（排除几何列）
	var selectCols string
	if len(columns) > 0 {
		selectCols = "id"
		for _, col := range columns {
			if col != layer.GeometryColumn && col != "id" {
				selectCols += fmt.Sprintf(`, "%s"`, col)
			}
		}
	} else {
		// 默认返回 id（避免返回所有列导致性能问题）
		selectCols = "id"
	}

	// PostGIS ST_AsMVT 查询
	// 参考: https://postgis.net/docs/ST_AsMVT.html
	sql := fmt.Sprintf(`
		SELECT ST_AsMVT(tile, $1, $2, 'geom')
		FROM (
			SELECT
				%s,
				ST_AsMVTGeom(
					ST_Transform("%s", 3857),
					ST_MakeEnvelope($3, $4, $5, $6, 3857),
					$2,
					$7,
					true
				) AS geom
			FROM "%s"."%s"
			WHERE ST_Transform("%s", 3857) && ST_MakeEnvelope($3, $4, $5, $6, 3857)
			  AND ST_IsValid("%s")
		) AS tile
		WHERE geom IS NOT NULL
	`,
		selectCols,
		layer.GeometryColumn,
		layer.SchemaName, layer.DBTableName,
		layer.GeometryColumn,
		layer.GeometryColumn,
	)

	// 执行查询
	var mvtData []byte
	err := db.Raw(sql,
		layer.LayerName,        // $1: layer name
		mvtExtent,              // $2: extent
		bbox.MinX, bbox.MinY,   // $3, $4: min coordinates
		bbox.MaxX, bbox.MaxY,   // $5, $6: max coordinates
		mvtBuffer,              // $7: buffer
	).Scan(&mvtData).Error

	if err != nil {
		return nil, fmt.Errorf("failed to generate MVT tile: %w", err)
	}

	// 空瓦片返回 nil（无数据）
	if len(mvtData) == 0 {
		return nil, nil
	}

	return mvtData, nil
}
