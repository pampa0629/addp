#!/bin/bash

# ============================================================================
# Apache Spark + Sedona 测试数据初始化脚本
# ============================================================================
# 功能：
# 1. 在 PostgreSQL 业务库创建测试表
# 2. 插入空间测试数据（中国主要城市 POI）
# 3. 验证 Apache Spark 可以通过 JDBC 访问
# 4. 提供示例 SQL 查询
# ============================================================================

set -e

# 颜色定义
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# PostgreSQL 配置
POSTGRES_HOST="${POSTGRES_HOST:-localhost}"
POSTGRES_PORT="${POSTGRES_PORT:-5433}"
POSTGRES_DB="${POSTGRES_DB:-business}"
POSTGRES_USER="${POSTGRES_USER:-business}"
POSTGRES_PASSWORD="${POSTGRES_PASSWORD:-business_password}"

# Spark Thrift Server 配置
SPARK_HOST="${SPARK_HOST:-localhost}"
SPARK_PORT="${SPARK_PORT:-10000}"

echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}Apache Spark 测试数据初始化${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""

# ============================================================================
# 1. 检查 PostgreSQL 连接
# ============================================================================
echo -e "${YELLOW}[1/5] 检查 PostgreSQL 连接...${NC}"
if ! PGPASSWORD=$POSTGRES_PASSWORD psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -c "SELECT 1" > /dev/null 2>&1; then
    echo -e "${RED}❌ PostgreSQL 连接失败！${NC}"
    echo -e "${YELLOW}请先启动业务数据库：${NC}"
    echo -e "  cd business && docker-compose up -d postgres"
    exit 1
fi
echo -e "${GREEN}✅ PostgreSQL 连接成功${NC}"
echo ""

# ============================================================================
# 2. 创建测试表
# ============================================================================
echo -e "${YELLOW}[2/5] 创建测试表...${NC}"
PGPASSWORD=$POSTGRES_PASSWORD psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB <<EOF
-- 删除旧表（如果存在）
DROP TABLE IF EXISTS china_cities CASCADE;
DROP TABLE IF EXISTS china_pois CASCADE;

-- 创建城市表
CREATE TABLE china_cities (
    city_id SERIAL PRIMARY KEY,
    city_name VARCHAR(50) NOT NULL,
    province VARCHAR(50) NOT NULL,
    longitude DOUBLE PRECISION NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    population INTEGER,
    gdp_billion DECIMAL(10, 2),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建 POI 表
CREATE TABLE china_pois (
    poi_id SERIAL PRIMARY KEY,
    poi_name VARCHAR(100) NOT NULL,
    poi_type VARCHAR(50) NOT NULL,
    city_id INTEGER REFERENCES china_cities(city_id),
    longitude DOUBLE PRECISION NOT NULL,
    latitude DOUBLE PRECISION NOT NULL,
    address VARCHAR(200),
    rating DECIMAL(2, 1),
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

-- 创建索引
CREATE INDEX idx_cities_location ON china_cities(longitude, latitude);
CREATE INDEX idx_pois_location ON china_pois(longitude, latitude);
CREATE INDEX idx_pois_city ON china_pois(city_id);
CREATE INDEX idx_pois_type ON china_pois(poi_type);

EOF

echo -e "${GREEN}✅ 测试表创建成功${NC}"
echo ""

# ============================================================================
# 3. 插入测试数据
# ============================================================================
echo -e "${YELLOW}[3/5] 插入测试数据...${NC}"
PGPASSWORD=$POSTGRES_PASSWORD psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB <<EOF
-- 插入中国主要城市数据
INSERT INTO china_cities (city_name, province, longitude, latitude, population, gdp_billion) VALUES
    ('北京', '北京市', 116.4074, 39.9042, 21893000, 4610.00),
    ('上海', '上海市', 121.4737, 31.2304, 24870000, 4720.00),
    ('广州', '广东省', 113.2644, 23.1291, 18676000, 3010.00),
    ('深圳', '广东省', 114.0579, 22.5431, 17561000, 3387.00),
    ('成都', '四川省', 104.0668, 30.5728, 21050000, 2167.00),
    ('杭州', '浙江省', 120.1551, 30.2741, 12280000, 1911.00),
    ('武汉', '湖北省', 114.3055, 30.5931, 13731000, 1990.00),
    ('西安', '陕西省', 108.9398, 34.3416, 12953000, 1152.00),
    ('重庆', '重庆市', 106.5516, 29.5630, 32054000, 3010.00),
    ('南京', '江苏省', 118.7969, 32.0603, 9314000, 1729.00);

-- 插入 POI 数据（每个城市 10 个 POI）
-- 北京 POI
INSERT INTO china_pois (poi_name, poi_type, city_id, longitude, latitude, address, rating) VALUES
    ('天安门广场', '景点', 1, 116.3973, 39.9087, '北京市东城区长安街', 4.8),
    ('故宫博物院', '景点', 1, 116.3972, 39.9163, '北京市东城区景山前街4号', 4.9),
    ('北京大学', '高校', 1, 116.3105, 39.9925, '北京市海淀区颐和园路5号', 4.7),
    ('鸟巢', '体育场馆', 1, 116.3883, 39.9928, '北京市朝阳区国家体育场南路1号', 4.6),
    ('三里屯太古里', '购物中心', 1, 116.4551, 39.9368, '北京市朝阳区三里屯路19号', 4.5),
    ('王府井大街', '商业街', 1, 116.4106, 39.9147, '北京市东城区王府井大街', 4.4),
    ('北京南站', '交通枢纽', 1, 116.3786, 39.8654, '北京市丰台区永外大街12号', 4.3),
    ('798艺术区', '文化创意', 1, 116.4976, 39.9843, '北京市朝阳区酒仙桥路4号', 4.6),
    ('颐和园', '景点', 1, 116.2733, 39.9997, '北京市海淀区新建宫门路19号', 4.8),
    ('国贸CBD', '商务区', 1, 116.4611, 39.9081, '北京市朝阳区建国门外大街1号', 4.5);

-- 上海 POI
INSERT INTO china_pois (poi_name, poi_type, city_id, longitude, latitude, address, rating) VALUES
    ('东方明珠', '景点', 2, 121.4995, 31.2397, '上海市浦东新区世纪大道1号', 4.7),
    ('外滩', '景点', 2, 121.4904, 31.2397, '上海市黄浦区中山东一路', 4.9),
    ('迪士尼乐园', '主题乐园', 2, 121.6656, 31.1434, '上海市浦东新区川沙新镇', 4.8),
    ('南京路步行街', '商业街', 2, 121.4809, 31.2378, '上海市黄浦区南京东路', 4.6),
    ('豫园', '景点', 2, 121.4922, 31.2277, '上海市黄浦区安仁街137号', 4.5),
    ('上海中心大厦', '地标建筑', 2, 121.5056, 31.2336, '上海市浦东新区银城中路501号', 4.7),
    ('虹桥机场', '交通枢纽', 2, 121.3361, 31.1979, '上海市长宁区虹桥路2550号', 4.4),
    ('田子坊', '文化创意', 2, 121.4701, 31.2101, '上海市黄浦区泰康路', 4.5),
    ('世纪公园', '公园', 2, 121.5525, 31.2187, '上海市浦东新区锦绣路1001号', 4.6),
    ('陆家嘴金融区', '商务区', 2, 121.5050, 31.2398, '上海市浦东新区陆家嘴环路', 4.7);

-- 深圳 POI
INSERT INTO china_pois (poi_name, poi_type, city_id, longitude, latitude, address, rating) VALUES
    ('世界之窗', '主题乐园', 4, 113.9761, 22.5358, '深圳市南山区深南大道9037号', 4.5),
    ('欢乐谷', '主题乐园', 4, 114.0030, 22.5468, '深圳市南山区侨城西街1号', 4.6),
    ('深圳湾公园', '公园', 4, 113.9563, 22.5122, '深圳市南山区滨海大道', 4.7),
    ('华强北商业街', '商业街', 4, 114.0889, 22.5467, '深圳市福田区华强北路', 4.4),
    ('平安金融中心', '地标建筑', 4, 114.1140, 22.5395, '深圳市福田区益田路5033号', 4.6),
    ('深圳北站', '交通枢纽', 4, 114.0304, 22.6091, '深圳市龙华区民治街道', 4.3),
    ('南山科技园', '科技园区', 4, 113.9470, 22.5375, '深圳市南山区高新南一道', 4.5),
    ('大梅沙海滨公园', '景点', 4, 114.3158, 22.5965, '深圳市盐田区盐梅路33号', 4.6),
    ('莲花山公园', '公园', 4, 114.0626, 22.5559, '深圳市福田区红荔路6030号', 4.7),
    ('深圳湾口岸', '口岸', 4, 113.9422, 22.4944, '深圳市南山区东滨路', 4.4);

-- 成都 POI
INSERT INTO china_pois (poi_name, poi_type, city_id, longitude, latitude, address, rating) VALUES
    ('宽窄巷子', '景点', 5, 104.0577, 30.6732, '成都市青羊区金河路口', 4.6),
    ('锦里', '景点', 5, 104.0499, 30.6508, '成都市武侯区武侯祠大街231号', 4.5),
    ('春熙路', '商业街', 5, 104.0811, 30.6590, '成都市锦江区春熙路', 4.5),
    ('大熊猫基地', '景点', 5, 104.1478, 30.7338, '成都市成华区外北三环熊猫大道1375号', 4.9),
    ('IFS国际金融中心', '购物中心', 5, 104.0832, 30.6631, '成都市锦江区红星路三段1号', 4.7),
    ('成都东站', '交通枢纽', 5, 104.1472, 30.6142, '成都市成华区邛崃山路333号', 4.4),
    ('天府广场', '地标', 5, 104.0728, 30.6600, '成都市青羊区人民中路一段86号', 4.5),
    ('武侯祠', '景点', 5, 104.0464, 30.6449, '成都市武侯区武侯祠大街231号', 4.7),
    ('杜甫草堂', '景点', 5, 104.0234, 30.6641, '成都市青羊区青华路37号', 4.6),
    ('高新区', '科技园区', 5, 104.0659, 30.5604, '成都市武侯区天府大道', 4.6);

-- 杭州 POI
INSERT INTO china_pois (poi_name, poi_type, city_id, longitude, latitude, address, rating) VALUES
    ('西湖', '景点', 6, 120.1489, 30.2594, '杭州市西湖区龙井路1号', 4.9),
    ('灵隐寺', '宗教场所', 6, 120.0972, 30.2425, '杭州市西湖区灵隐路法云弄1号', 4.7),
    ('雷峰塔', '景点', 6, 120.1484, 30.2318, '杭州市西湖区南山路15号', 4.6),
    ('河坊街', '商业街', 6, 120.1721, 30.2491, '杭州市上城区河坊街', 4.5),
    ('西溪湿地', '景点', 6, 120.0533, 30.2733, '杭州市西湖区天目山路518号', 4.7),
    ('杭州东站', '交通枢纽', 6, 120.2137, 30.2903, '杭州市江干区天城路1号', 4.4),
    ('阿里巴巴西溪园区', '科技园区', 6, 120.0358, 30.2806, '杭州市余杭区文一西路969号', 4.6),
    ('钱江新城', '商务区', 6, 120.2050, 30.2563, '杭州市江干区富春路', 4.5),
    ('宋城', '主题乐园', 6, 120.1191, 30.2071, '杭州市西湖区之江路148号', 4.6),
    ('西湖文化广场', '文化场馆', 6, 120.1660, 30.2862, '杭州市下城区中山北路', 4.5);

EOF

# 统计数据量
CITY_COUNT=$(PGPASSWORD=$POSTGRES_PASSWORD psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -t -c "SELECT COUNT(*) FROM china_cities;" | tr -d ' ')
POI_COUNT=$(PGPASSWORD=$POSTGRES_PASSWORD psql -h $POSTGRES_HOST -p $POSTGRES_PORT -U $POSTGRES_USER -d $POSTGRES_DB -t -c "SELECT COUNT(*) FROM china_pois;" | tr -d ' ')

echo -e "${GREEN}✅ 测试数据插入成功${NC}"
echo -e "   - 城市数据: ${CITY_COUNT} 条"
echo -e "   - POI 数据: ${POI_COUNT} 条"
echo ""

# ============================================================================
# 4. 验证 Apache Spark 连接（可选）
# ============================================================================
echo -e "${YELLOW}[4/5] 验证 Apache Spark 连接...${NC}"
if command -v nc &> /dev/null; then
    if nc -z $SPARK_HOST $SPARK_PORT 2>/dev/null; then
        echo -e "${GREEN}✅ Spark Thrift Server 可访问 (${SPARK_HOST}:${SPARK_PORT})${NC}"
    else
        echo -e "${YELLOW}⚠️  Spark Thrift Server 不可访问${NC}"
        echo -e "${YELLOW}   请先启动 Spark: cd business && docker-compose up -d spark-master${NC}"
    fi
else
    echo -e "${YELLOW}⚠️  未安装 nc 命令，跳过 Spark 连接检查${NC}"
fi
echo ""

# ============================================================================
# 5. 输出示例 SQL
# ============================================================================
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}🎉 初始化完成！${NC}"
echo -e "${BLUE}======================================${NC}"
echo ""
echo -e "${YELLOW}📝 示例 SQL 查询：${NC}"
echo ""

cat <<'SQLEXAMPLE'
-- 1. 通过 Apache Spark 查询城市表（JDBC 外部表）
CREATE TABLE spark_cities
USING jdbc
OPTIONS (
  url "jdbc:postgresql://host.docker.internal:5433/business",
  dbtable "china_cities",
  user "business",
  password "business_password",
  driver "org.postgresql.Driver"
);

SELECT * FROM spark_cities LIMIT 10;

-- 2. 创建空间点并计算距离
SELECT
  city_name,
  ST_Point(longitude, latitude) AS geom,
  ST_Distance(
    ST_Point(longitude, latitude),
    ST_Point(116.4074, 39.9042)  -- 北京坐标
  ) AS distance_to_beijing_degrees
FROM spark_cities
ORDER BY distance_to_beijing_degrees ASC;

-- 3. 计算城市间实际距离（米）
SELECT
  city_name,
  ST_Distance(
    ST_Transform(ST_Point(longitude, latitude), 4326, 3857),
    ST_Transform(ST_Point(116.4074, 39.9042), 4326, 3857)
  ) / 1000 AS distance_to_beijing_km
FROM spark_cities
ORDER BY distance_to_beijing_km ASC;

-- 4. 查询 POI 表并关联城市
CREATE TABLE spark_pois
USING jdbc
OPTIONS (
  url "jdbc:postgresql://host.docker.internal:5433/business",
  dbtable "china_pois",
  user "business",
  password "business_password",
  driver "org.postgresql.Driver"
);

SELECT
  p.poi_name,
  p.poi_type,
  c.city_name,
  p.rating
FROM spark_pois p
JOIN spark_cities c ON p.city_id = c.city_id
WHERE p.rating >= 4.5
ORDER BY p.rating DESC
LIMIT 20;

-- 5. 空间聚合：按城市统计 POI 数量
SELECT
  c.city_name,
  COUNT(p.poi_id) AS poi_count,
  AVG(p.rating) AS avg_rating,
  ST_Centroid(ST_Collect(ST_Point(p.longitude, p.latitude))) AS centroid
FROM spark_pois p
JOIN spark_cities c ON p.city_id = c.city_id
GROUP BY c.city_name
ORDER BY poi_count DESC;

-- 6. 查找某个 POI 附近的其他 POI（例如：天安门广场周边 1km）
WITH target AS (
  SELECT ST_Point(116.3973, 39.9087) AS target_point
)
SELECT
  p.poi_name,
  p.poi_type,
  ST_Distance(
    ST_Transform(ST_Point(p.longitude, p.latitude), 4326, 3857),
    ST_Transform(target.target_point, 4326, 3857)
  ) AS distance_meters
FROM spark_pois p, target
WHERE ST_Distance(
  ST_Transform(ST_Point(p.longitude, p.latitude), 4326, 3857),
  ST_Transform(target.target_point, 4326, 3857)
) < 1000  -- 1000米范围内
ORDER BY distance_meters ASC;

SQLEXAMPLE

echo ""
echo -e "${GREEN}✨ 提示：${NC}"
echo -e "   1. 登录 ADDP Portal → System 模块 → 注册 Apache Spark 资源"
echo -e "   2. 进入 Develop 模块 → SQL 工作台"
echo -e "   3. 选择 Apache Spark 数据源，粘贴上述 SQL 执行"
echo ""
echo -e "${BLUE}======================================${NC}"
echo -e "${BLUE}详细文档: docs/spark-user-guide.md${NC}"
echo -e "${BLUE}======================================${NC}"
