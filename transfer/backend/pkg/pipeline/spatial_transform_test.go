package pipeline

import (
	"context"
	"encoding/hex"
	"testing"

	"github.com/twpayne/go-geom"
	"github.com/twpayne/go-geom/encoding/wkb"
	"github.com/twpayne/go-geom/encoding/wkt"
)

func TestSpatialTransform_WKBToWKT(t *testing.T) {
	// 创建一个 Point(1, 2) 的 WKB 数据
	point := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{1, 2})
	wkbData, err := wkb.Marshal(point, wkb.NDR)
	if err != nil {
		t.Fatalf("Failed to marshal WKB: %v", err)
	}

	// 创建转换器配置
	config := map[string]interface{}{
		"geometry_fields": []interface{}{"geom"},
		"source_format":   "wkb",
		"target_format":   "wkt",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	// 创建测试批次
	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": wkbData},
			{"id": 2, "geom": wkbData},
		},
	}

	// 应用转换
	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// 验证结果
	if len(result.Rows) != 2 {
		t.Errorf("Expected 2 rows, got %d", len(result.Rows))
	}

	// 验证第一行
	geomValue, exists := result.Rows[0]["geom"]
	if !exists {
		t.Error("geom field not found in result")
	}

	wktStr, ok := geomValue.(string)
	if !ok {
		t.Errorf("Expected WKT string, got %T", geomValue)
	}

	if wktStr != "POINT (1 2)" {
		t.Errorf("Expected 'POINT (1 2)', got '%s'", wktStr)
	}
}

func TestSpatialTransform_WKTToGeoJSON(t *testing.T) {
	config := map[string]interface{}{
		"geometry_fields": []interface{}{"geom"},
		"source_format":   "wkt",
		"target_format":   "geojson",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": "POINT (10 20)"},
		},
	}

	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// 验证 GeoJSON 输出
	geomValue, exists := result.Rows[0]["geom"]
	if !exists {
		t.Error("geom field not found")
	}

	geojsonMap, ok := geomValue.(map[string]interface{})
	if !ok {
		t.Errorf("Expected GeoJSON map, got %T", geomValue)
	}

	if geojsonMap["type"] != "Point" {
		t.Errorf("Expected type 'Point', got '%v'", geojsonMap["type"])
	}
}

func TestSpatialTransform_HexWKBToWKT(t *testing.T) {
	// 创建 Point(3, 4)
	point := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{3, 4})
	wkbData, _ := wkb.Marshal(point, wkb.NDR)
	hexWKB := hex.EncodeToString(wkbData)

	config := map[string]interface{}{
		"geometry_fields": []interface{}{"geom"},
		"source_format":   "hexwkb",
		"target_format":   "wkt",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": hexWKB},
		},
	}

	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	wktStr := result.Rows[0]["geom"].(string)
	if wktStr != "POINT (3 4)" {
		t.Errorf("Expected 'POINT (3 4)', got '%s'", wktStr)
	}
}

func TestSpatialTransform_LineString(t *testing.T) {
	// 创建 LineString
	lineCoords := []geom.Coord{{0, 0}, {1, 1}, {2, 0}}
	line := geom.NewLineString(geom.XY).MustSetCoords(lineCoords)
	wktStr, _ := wkt.Marshal(line)

	config := map[string]interface{}{
		"geometry_fields": []interface{}{"route"},
		"source_format":   "wkt",
		"target_format":   "geojson",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "route": wktStr},
		},
	}

	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	geojson := result.Rows[0]["route"].(map[string]interface{})
	if geojson["type"] != "LineString" {
		t.Errorf("Expected type 'LineString', got '%v'", geojson["type"])
	}
}

func TestSpatialTransform_MultipleFields(t *testing.T) {
	point1 := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{1, 2})
	point2 := geom.NewPoint(geom.XY).MustSetCoords(geom.Coord{3, 4})

	wkb1, _ := wkb.Marshal(point1, wkb.NDR)
	wkb2, _ := wkb.Marshal(point2, wkb.NDR)

	config := map[string]interface{}{
		"geometry_fields": []interface{}{"start_point", "end_point"},
		"source_format":   "wkb",
		"target_format":   "wkt",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{
				"id":          1,
				"start_point": wkb1,
				"end_point":   wkb2,
			},
		},
	}

	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// 验证两个字段都被转换
	row := result.Rows[0]
	if row["start_point"] != "POINT (1 2)" {
		t.Errorf("start_point conversion failed")
	}
	if row["end_point"] != "POINT (3 4)" {
		t.Errorf("end_point conversion failed")
	}
}

func TestSpatialTransform_NullHandling(t *testing.T) {
	config := map[string]interface{}{
		"geometry_fields": []interface{}{"geom"},
		"source_format":   "wkt",
		"target_format":   "geojson",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": nil},                    // NULL 值
			{"id": 2, "name": "test"},                 // 缺少 geom 字段
			{"id": 3, "geom": "POINT (5 6)"},          // 正常值
		},
	}

	result, err := transform.Apply(context.Background(), batch)
	if err != nil {
		t.Fatalf("Transform failed: %v", err)
	}

	// NULL 值和缺失字段应该被保留
	if result.Rows[0]["geom"] != nil {
		t.Error("NULL value should be preserved")
	}

	if _, exists := result.Rows[1]["geom"]; exists {
		t.Error("Missing field should remain missing")
	}

	// 正常值应该被转换
	if result.Rows[2]["geom"] == nil {
		t.Error("Valid geometry should be converted")
	}
}

func TestSpatialTransform_InvalidConfig(t *testing.T) {
	// 缺少 geometry_fields
	config := map[string]interface{}{
		"source_format": "wkt",
		"target_format": "geojson",
	}

	_, err := NewSpatialTransform(config)
	if err == nil {
		t.Error("Expected error for missing geometry_fields")
	}
}

func TestSpatialTransform_InvalidGeometry(t *testing.T) {
	config := map[string]interface{}{
		"geometry_fields": []interface{}{"geom"},
		"source_format":   "wkt",
		"target_format":   "geojson",
	}

	transform, err := NewSpatialTransform(config)
	if err != nil {
		t.Fatalf("Failed to create SpatialTransform: %v", err)
	}

	batch := &DataBatch{
		Rows: []map[string]interface{}{
			{"id": 1, "geom": "INVALID WKT"},
		},
	}

	_, err = transform.Apply(context.Background(), batch)
	if err == nil {
		t.Error("Expected error for invalid WKT")
	}
}
