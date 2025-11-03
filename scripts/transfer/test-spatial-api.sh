#!/bin/bash

# Transfer 模块 Spatial Transform API 测试脚本
# 使用方法: ./test-spatial-api.sh

set -e

BASE_URL="http://localhost:8083/api"
TOKEN="YOUR_JWT_TOKEN_HERE"  # 替换为实际 JWT token

echo "======================================"
echo "Spatial Transform API 测试"
echo "======================================"
echo ""

# 1. 列出所有转换器
echo "1️⃣  列出所有可用转换器..."
curl -s -X GET \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/transforms" | jq .

echo ""
echo ""

# 2. 获取 spatial 转换器详情
echo "2️⃣  获取 spatial 转换器详情..."
curl -s -X GET \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/transforms/spatial" | jq .

echo ""
echo ""

# 3. 验证配置
echo "3️⃣  验证转换器配置..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "geometry_fields": ["geom"],
      "source_format": "wkb",
      "target_format": "wkt"
    }
  }' \
  "$BASE_URL/transforms/spatial/validate" | jq .

echo ""
echo ""

# 4. 测试 WKT → GeoJSON 转换
echo "4️⃣  测试 WKT → GeoJSON 转换..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "geometry_fields": ["location"],
      "source_format": "wkt",
      "target_format": "geojson"
    },
    "sample": [
      {
        "id": 1,
        "name": "Beijing",
        "location": "POINT (116.4074 39.9042)"
      },
      {
        "id": 2,
        "name": "Shanghai",
        "location": "POINT (121.4737 31.2304)"
      }
    ]
  }' \
  "$BASE_URL/transforms/spatial/test" | jq .

echo ""
echo ""

# 5. 测试 LineString 转换
echo "5️⃣  测试 LineString WKT → GeoJSON..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "geometry_fields": ["route"],
      "source_format": "wkt",
      "target_format": "geojson"
    },
    "sample": [
      {
        "id": 1,
        "name": "Route A",
        "route": "LINESTRING (0 0, 1 1, 2 0)"
      }
    ]
  }' \
  "$BASE_URL/transforms/spatial/test" | jq .

echo ""
echo ""

# 6. 测试多字段转换
echo "6️⃣  测试多字段转换..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "geometry_fields": ["start_point", "end_point"],
      "source_format": "wkt",
      "target_format": "geojson"
    },
    "sample": [
      {
        "id": 1,
        "name": "Trip 1",
        "start_point": "POINT (0 0)",
        "end_point": "POINT (10 10)"
      }
    ]
  }' \
  "$BASE_URL/transforms/spatial/test" | jq .

echo ""
echo ""

# 7. 获取转换器统计
echo "7️⃣  获取转换器统计信息..."
curl -s -X GET \
  -H "Authorization: Bearer $TOKEN" \
  "$BASE_URL/transforms/stats" | jq .

echo ""
echo ""

# 8. 测试无效配置（应该返回错误）
echo "8️⃣  测试无效配置（预期失败）..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "source_format": "wkt",
      "target_format": "geojson"
    }
  }' \
  "$BASE_URL/transforms/spatial/validate" | jq .

echo ""
echo ""

# 9. 测试无效几何数据（应该返回错误）
echo "9️⃣  测试无效几何数据（预期失败）..."
curl -s -X POST \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "config": {
      "geometry_fields": ["geom"],
      "source_format": "wkt",
      "target_format": "geojson"
    },
    "sample": [
      {
        "id": 1,
        "geom": "INVALID WKT DATA"
      }
    ]
  }' \
  "$BASE_URL/transforms/spatial/test" | jq .

echo ""
echo "======================================"
echo "测试完成！"
echo "======================================"

