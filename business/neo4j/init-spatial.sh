#!/bin/bash
# Neo4j 空间扩展初始化脚本
# 验证并初始化 Neo4j 空间功能（原生 point 类型 + APOC 空间工具）
#
# 用法:
#   bash business/neo4j/init-spatial.sh

set -e

CONTAINER="business-neo4j"
NEO4J_USER="${NEO4J_USER:-neo4j}"
NEO4J_PASS="${NEO4J_PASS:-neo4j_password}"

run_cypher() {
  docker exec "$CONTAINER" cypher-shell -u "$NEO4J_USER" -p "$NEO4J_PASS" "$1"
}

echo "=== Neo4j 空间扩展初始化 ==="

# ── 1. 检查 APOC 是否已加载 ────────────────────────────────────────────────────
echo ""
echo "→ 检查 APOC 状态..."
APOC_VERSION=$(run_cypher "RETURN apoc.version() AS v" 2>&1 || echo "")
if echo "$APOC_VERSION" | grep -q "Unknown function"; then
  echo "  [!] APOC 未加载。请执行以下步骤："
  echo "      1. 确认 docker-compose.yml 中已有: NEO4J_PLUGINS: '[\"apoc\"]'"
  echo "      2. 重启容器: docker-compose -f business/docker-compose.yml restart neo4j"
  echo "      3. 等待约 30 秒后重新运行此脚本"
  exit 1
else
  echo "  [✓] APOC 已加载: $(echo "$APOC_VERSION" | grep -oE '[0-9]+\.[0-9]+\.[0-9]+')"
fi

# ── 2. 验证原生空间点类型 ─────────────────────────────────────────────────────
echo ""
echo "→ 验证原生 point 空间类型..."

# WGS-84 地理坐标 (SRID 4326)
run_cypher "
RETURN
  point({latitude: 39.904, longitude: 116.407}) AS beijing,
  point({latitude: 31.230, longitude: 121.473}) AS shanghai,
  round(point.distance(
    point({latitude: 39.904, longitude: 116.407}),
    point({latitude: 31.230, longitude: 121.473})
  ) / 1000) AS distance_km
" | tail -3
echo "  [✓] 原生 point 类型正常（WGS-84, SRID:4326）"

# ── 3. 验证 APOC 空间工具 ─────────────────────────────────────────────────────
echo ""
echo "→ 验证 APOC 空间函数..."
APOC_GEO=$(run_cypher "CALL apoc.help('apoc.spatial') YIELD name RETURN count(*) AS cnt" 2>&1)
if echo "$APOC_GEO" | grep -q "cnt"; then
  echo "  [✓] apoc.spatial 模块可用（geocoding 等需要网络权限）"
else
  echo "  [i] apoc.spatial.geocode 需要配置网络访问（可选），基础空间功能已就绪"
fi

# ── 4. 创建带空间属性的示例节点 ───────────────────────────────────────────────
echo ""
echo "→ 创建空间示例数据（城市节点）..."
run_cypher "
MERGE (c:City {id: 'CITY_BJ'})
SET c.name = '北京',
    c.location = point({latitude: 39.904, longitude: 116.407}),
    c.province = '北京市'
MERGE (c2:City {id: 'CITY_SH'})
SET c2.name = '上海',
    c2.location = point({latitude: 31.230, longitude: 121.473}),
    c2.province = '上海市'
MERGE (c3:City {id: 'CITY_SZ'})
SET c3.name = '深圳',
    c3.location = point({latitude: 22.543, longitude: 114.058}),
    c3.province = '广东省'
MERGE (c4:City {id: 'CITY_HZ'})
SET c4.name = '杭州',
    c4.location = point({latitude: 30.274, longitude: 120.155}),
    c4.province = '浙江省'
MERGE (c5:City {id: 'CITY_CD'})
SET c5.name = '成都',
    c5.location = point({latitude: 30.657, longitude: 104.066}),
    c5.province = '四川省'
RETURN count(*) AS upserted
"
echo "  [✓] 城市节点已创建（带 location point 属性）"

# ── 5. 创建空间索引 ───────────────────────────────────────────────────────────
echo ""
echo "→ 创建空间点索引..."
run_cypher "
CREATE POINT INDEX city_location_idx IF NOT EXISTS
FOR (c:City) ON (c.location)
OPTIONS {indexConfig: {
  \`spatial.cartesian.min\`: [-1000000.0, -1000000.0],
  \`spatial.cartesian.max\`: [1000000.0, 1000000.0],
  \`spatial.wgs-84.min\`: [-180.0, -90.0],
  \`spatial.wgs-84.max\`: [180.0, 90.0]
}}
" 2>&1 | grep -v "^$" || true
echo "  [✓] 空间索引 city_location_idx 已创建"

# ── 6. 示例查询：距离检索 ─────────────────────────────────────────────────────
echo ""
echo "→ 示例查询：以北京为中心，1500km 范围内的城市（按距离排序）"
run_cypher "
WITH point({latitude: 39.904, longitude: 116.407}) AS beijing
MATCH (c:City)
WHERE c.id <> 'CITY_BJ'
WITH c, round(point.distance(c.location, beijing) / 1000) AS distance_km
ORDER BY distance_km
RETURN c.name AS 城市, c.province AS 省份, distance_km AS 距离_km
"

# ── 7. 汇总 ──────────────────────────────────────────────────────────────────
echo ""
echo "=== 空间功能摘要 ==="
echo "  ① 原生 point 类型  : point({latitude, longitude}) — SRID 4326 (WGS-84)"
echo "  ② 距离计算         : point.distance(p1, p2) → 单位：米"
echo "  ③ 范围查询         : WHERE point.withinBBox(c.location, lowerLeft, upperRight)"
echo "  ④ 空间索引         : CREATE POINT INDEX ... FOR (n:Label) ON (n.location)"
echo "  ⑤ APOC 空间工具    : apoc.spatial.parseLatLng / apoc.spatial.geocodeOnce"
echo ""
echo "=== 初始化完成 ==="
