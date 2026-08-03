#!/bin/bash
# MySQL 测试数据脚本
# 创建多个测试表并插入示例数据，便于验证元数据扫描、预览、空间和基础查询能力

set -e

CONTAINER="${MYSQL_CONTAINER:-business-mysql}"
MYSQL_USER="${MYSQL_USER:-root}"
MYSQL_PASSWORD="${MYSQL_ROOT_PASSWORD:-password}"
MYSQL_DATABASE="${MYSQL_DATABASE:-business}"

run_sql() {
  docker exec -i "$CONTAINER" mysql \
    -h127.0.0.1 \
    -u"$MYSQL_USER" \
    -p"$MYSQL_PASSWORD" \
    --default-character-set=utf8mb4 \
    "$MYSQL_DATABASE"
}

query_scalar() {
  docker exec -i "$CONTAINER" mysql \
    -h127.0.0.1 \
    -u"$MYSQL_USER" \
    -p"$MYSQL_PASSWORD" \
    --default-character-set=utf8mb4 \
    --batch \
    --skip-column-names \
    "$MYSQL_DATABASE"
}

echo "=== MySQL 测试数据初始化开始 ==="

run_sql <<'EOSQL'
SET NAMES utf8mb4;

DROP TABLE IF EXISTS order_items;
DROP TABLE IF EXISTS orders;
DROP TABLE IF EXISTS products;
DROP TABLE IF EXISTS customers;
DROP TABLE IF EXISTS spatial_multi_columns;
DROP TABLE IF EXISTS projected_locations;
DROP TABLE IF EXISTS mixed_features;
DROP TABLE IF EXISTS spatial_collections;
DROP TABLE IF EXISTS market_regions;
DROP TABLE IF EXISTS transport_networks;
DROP TABLE IF EXISTS regional_facilities;
DROP TABLE IF EXISTS service_areas;
DROP TABLE IF EXISTS delivery_routes;
DROP TABLE IF EXISTS store_locations;

CREATE TABLE customers (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    customer_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(80) NOT NULL,
    gender ENUM('male', 'female', 'unknown') NOT NULL DEFAULT 'unknown',
    email VARCHAR(120) NOT NULL,
    phone VARCHAR(32),
    city VARCHAR(64),
    membership_level VARCHAR(32) NOT NULL DEFAULT 'standard',
    points INT NOT NULL DEFAULT 0,
    active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL,
    updated_at DATETIME NOT NULL,
    INDEX idx_customers_city (city),
    INDEX idx_customers_membership (membership_level)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE products (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    sku VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    category VARCHAR(64) NOT NULL,
    price DECIMAL(12, 2) NOT NULL,
    stock INT NOT NULL DEFAULT 0,
    attributes JSON,
    on_sale BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL,
    INDEX idx_products_category (category)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE orders (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_no VARCHAR(40) NOT NULL UNIQUE,
    customer_id BIGINT NOT NULL,
    status VARCHAR(32) NOT NULL,
    total_amount DECIMAL(12, 2) NOT NULL,
    payment_method VARCHAR(32),
    ordered_at DATETIME NOT NULL,
    shipped_at DATETIME NULL,
    shipping_address VARCHAR(255),
    FOREIGN KEY (customer_id) REFERENCES customers(id),
    INDEX idx_orders_customer (customer_id),
    INDEX idx_orders_status (status),
    INDEX idx_orders_ordered_at (ordered_at)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE order_items (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    order_id BIGINT NOT NULL,
    product_id BIGINT NOT NULL,
    quantity INT NOT NULL,
    unit_price DECIMAL(12, 2) NOT NULL,
    discount_amount DECIMAL(12, 2) NOT NULL DEFAULT 0,
    FOREIGN KEY (order_id) REFERENCES orders(id),
    FOREIGN KEY (product_id) REFERENCES products(id),
    INDEX idx_order_items_order (order_id),
    INDEX idx_order_items_product (product_id)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE store_locations (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    store_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    city VARCHAR(64) NOT NULL,
    address VARCHAR(255) NOT NULL,
    longitude DECIMAL(10, 6) NOT NULL,
    latitude DECIMAL(10, 6) NOT NULL,
    geom POINT SRID 4326 NOT NULL,
    opened_on DATE NOT NULL,
    SPATIAL INDEX idx_store_locations_geom (geom),
    INDEX idx_store_locations_city (city)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE delivery_routes (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    route_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom LINESTRING SRID 4326 NOT NULL,
    SPATIAL INDEX idx_delivery_routes_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE service_areas (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    area_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom POLYGON SRID 4326 NOT NULL,
    SPATIAL INDEX idx_service_areas_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE regional_facilities (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    region_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom MULTIPOINT SRID 4326 NOT NULL,
    SPATIAL INDEX idx_regional_facilities_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE transport_networks (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    network_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom MULTILINESTRING SRID 4326 NOT NULL,
    SPATIAL INDEX idx_transport_networks_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE market_regions (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    region_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom MULTIPOLYGON SRID 4326 NOT NULL,
    SPATIAL INDEX idx_market_regions_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE spatial_collections (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    collection_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom GEOMETRYCOLLECTION SRID 4326 NOT NULL,
    SPATIAL INDEX idx_spatial_collections_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE mixed_features (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    feature_code VARCHAR(32) NOT NULL UNIQUE,
    feature_kind VARCHAR(32) NOT NULL,
    geom GEOMETRY SRID 4326 NOT NULL,
    SPATIAL INDEX idx_mixed_features_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE projected_locations (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    location_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    geom POINT SRID 3857 NOT NULL,
    SPATIAL INDEX idx_projected_locations_geom (geom)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

CREATE TABLE spatial_multi_columns (
    id BIGINT PRIMARY KEY AUTO_INCREMENT,
    site_code VARCHAR(32) NOT NULL UNIQUE,
    name VARCHAR(120) NOT NULL,
    center POINT SRID 4326 NOT NULL,
    coverage POLYGON SRID 4326 NULL,
    SPATIAL INDEX idx_spatial_multi_columns_center (center)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COLLATE=utf8mb4_unicode_ci;

INSERT INTO customers (customer_code, name, gender, email, phone, city, membership_level, points, active, created_at, updated_at) VALUES
    ('CUST-10001', '王小丽', 'female', 'alice.wang@example.com', '13800010001', '上海', 'gold', 5800, TRUE, '2024-01-15 10:00:00', '2026-04-20 09:30:00'),
    ('CUST-10002', '陈大明', 'male', 'bob.chen@example.com', '13800010002', '北京', 'platinum', 12500, TRUE, '2023-06-20 11:20:00', '2026-04-18 16:45:00'),
    ('CUST-10003', '李佳佳', 'female', 'carol.li@example.com', '13800010003', '深圳', 'silver', 2300, TRUE, '2024-11-01 14:10:00', '2026-04-15 12:00:00'),
    ('CUST-10004', '赵一鸣', 'male', 'zhao.yiming@example.com', '13800010004', '杭州', 'standard', 860, FALSE, '2025-03-08 08:40:00', '2026-03-30 19:10:00'),
    ('CUST-10005', '孙晓雨', 'female', 'sun.xiaoyu@example.com', '13800010005', '成都', 'gold', 7200, TRUE, '2025-08-12 17:25:00', '2026-04-21 10:05:00');

INSERT INTO products (sku, name, category, price, stock, attributes, on_sale, created_at) VALUES
    ('SKU-MBP-16', 'MacBook Pro 16', '笔记本电脑', 16999.00, 45, JSON_OBJECT('cpu', 'M3 Max', 'ram', '64GB', 'storage', '2TB SSD'), TRUE, '2025-12-01 09:00:00'),
    ('SKU-KEY-87', 'Magic Keyboard', '键盘', 1299.00, 180, JSON_OBJECT('layout', '87键', 'wireless', true, 'backlight', false), TRUE, '2025-12-05 09:00:00'),
    ('SKU-MOU-01', 'Magic Mouse 2', '鼠标', 799.00, 130, JSON_OBJECT('dpi', 4000, 'wireless', true, 'charging', 'Lightning'), TRUE, '2025-12-10 09:00:00'),
    ('SKU-HUB-C8', 'USB-C Hub 8合1', '扩展坞', 399.00, 260, JSON_OBJECT('ports', 8, 'hdmi', true, 'ethernet', true), TRUE, '2026-01-15 09:00:00'),
    ('SKU-MON-27', 'Studio Display 27', '显示器', 11499.00, 28, JSON_OBJECT('size', '27英寸', 'resolution', '5K'), TRUE, '2026-02-01 09:00:00');

INSERT INTO orders (order_no, customer_id, status, total_amount, payment_method, ordered_at, shipped_at, shipping_address) VALUES
    ('ORD-20260420-001', 1, 'delivered', 2897.00, 'alipay', '2026-04-20 10:12:00', '2026-04-21 15:30:00', '上海市浦东新区张江高科技园区'),
    ('ORD-20260421-002', 2, 'processing', 16999.00, 'wechat', '2026-04-21 14:05:00', NULL, '北京市海淀区中关村软件园'),
    ('ORD-20260422-003', 3, 'paid', 798.00, 'credit_card', '2026-04-22 18:35:00', NULL, '深圳市南山区科技园'),
    ('ORD-20260423-004', 5, 'delivered', 11499.00, 'alipay', '2026-04-23 09:18:00', '2026-04-24 11:40:00', '成都市高新区天府大道');

INSERT INTO order_items (order_id, product_id, quantity, unit_price, discount_amount) VALUES
    (1, 2, 1, 1299.00, 0.00),
    (1, 3, 2, 799.00, 0.00),
    (2, 1, 1, 16999.00, 0.00),
    (3, 4, 2, 399.00, 0.00),
    (4, 5, 1, 11499.00, 0.00);

INSERT INTO store_locations (store_code, name, city, address, longitude, latitude, geom, opened_on) VALUES
    ('STORE-SH-001', '上海张江体验店', '上海', '上海市浦东新区张江高科技园区', 121.599960, 31.204270, ST_GeomFromText('POINT(121.599960 31.204270)', 4326, 'axis-order=long-lat'), '2024-05-01'),
    ('STORE-BJ-001', '北京中关村体验店', '北京', '北京市海淀区中关村大街', 116.316200, 39.983200, ST_GeomFromText('POINT(116.316200 39.983200)', 4326, 'axis-order=long-lat'), '2023-10-15'),
    ('STORE-SZ-001', '深圳南山体验店', '深圳', '深圳市南山区科技园', 113.944820, 22.540880, ST_GeomFromText('POINT(113.944820 22.540880)', 4326, 'axis-order=long-lat'), '2025-03-20'),
    ('STORE-CD-001', '成都高新体验店', '成都', '成都市高新区天府大道', 104.066540, 30.572270, ST_GeomFromText('POINT(104.066540 30.572270)', 4326, 'axis-order=long-lat'), '2025-09-10');

INSERT INTO delivery_routes (route_code, name, geom) VALUES
    ('ROUTE-SH-01', '上海城区配送线', ST_GeomFromText('LINESTRING(121.4737 31.2304,121.5200 31.2350,121.59996 31.20427)', 4326, 'axis-order=long-lat')),
    ('ROUTE-BJ-01', '北京城区配送线', ST_GeomFromText('LINESTRING(116.3974 39.9093,116.3500 39.9500,116.3162 39.9832)', 4326, 'axis-order=long-lat'));

INSERT INTO service_areas (area_code, name, geom) VALUES
    ('AREA-SH-01', '上海核心服务区', ST_GeomFromText('POLYGON((121.40 31.10,121.75 31.10,121.75 31.35,121.40 31.35,121.40 31.10),(121.54 31.19,121.61 31.19,121.61 31.24,121.54 31.24,121.54 31.19))', 4326, 'axis-order=long-lat')),
    ('AREA-BJ-01', '北京核心服务区', ST_GeomFromText('POLYGON((116.20 39.80,116.55 39.80,116.55 40.10,116.20 40.10,116.20 39.80))', 4326, 'axis-order=long-lat'));

INSERT INTO regional_facilities (region_code, name, geom) VALUES
    ('FAC-EAST-01', '华东设施组', ST_GeomFromText('MULTIPOINT((121.4737 31.2304),(120.1551 30.2741),(118.7969 32.0603))', 4326, 'axis-order=long-lat'));

INSERT INTO transport_networks (network_code, name, geom) VALUES
    ('NET-YRD-01', '长三角运输网络', ST_GeomFromText('MULTILINESTRING((121.4737 31.2304,120.1551 30.2741),(121.4737 31.2304,118.7969 32.0603))', 4326, 'axis-order=long-lat'));

INSERT INTO market_regions (region_code, name, geom) VALUES
    ('MARKET-SOUTH-01', '华南市场片区', ST_GeomFromText('MULTIPOLYGON(((113.80 22.40,114.15 22.40,114.15 22.70,113.80 22.70,113.80 22.40)),((112.90 22.90,113.20 22.90,113.20 23.20,112.90 23.20,112.90 22.90)))', 4326, 'axis-order=long-lat'));

INSERT INTO spatial_collections (collection_code, name, geom) VALUES
    ('COLL-SH-01', '上海混合空间集合', ST_GeomFromText('GEOMETRYCOLLECTION(POINT(121.4737 31.2304),LINESTRING(121.45 31.20,121.55 31.25),POLYGON((121.40 31.15,121.60 31.15,121.60 31.30,121.40 31.30,121.40 31.15)))', 4326, 'axis-order=long-lat'));

INSERT INTO mixed_features (feature_code, feature_kind, geom) VALUES
    ('MIXED-POINT-01', 'point', ST_GeomFromText('POINT(121.4737 31.2304)', 4326, 'axis-order=long-lat')),
    ('MIXED-LINE-01', 'linestring', ST_GeomFromText('LINESTRING(121.40 31.20,121.60 31.25)', 4326, 'axis-order=long-lat')),
    ('MIXED-POLYGON-01', 'polygon', ST_GeomFromText('POLYGON((121.40 31.15,121.60 31.15,121.60 31.30,121.40 31.30,121.40 31.15))', 4326, 'axis-order=long-lat'));

INSERT INTO projected_locations (location_code, name, geom) VALUES
    ('PROJECTED-SH-01', '上海 Web Mercator 点', ST_Transform(ST_GeomFromText('POINT(121.4737 31.2304)', 4326, 'axis-order=long-lat'), 3857));

INSERT INTO spatial_multi_columns (site_code, name, center, coverage) VALUES
    ('SITE-SH-01', '上海复合空间站点',
     ST_GeomFromText('POINT(121.4737 31.2304)', 4326, 'axis-order=long-lat'),
     ST_GeomFromText('POLYGON((121.43 31.19,121.52 31.19,121.52 31.27,121.43 31.27,121.43 31.19))', 4326, 'axis-order=long-lat')),
    ('SITE-BJ-01', '北京无覆盖站点',
     ST_GeomFromText('POINT(116.3974 39.9093)', 4326, 'axis-order=long-lat'),
     NULL);

SELECT 'customers' AS table_name, COUNT(*) AS row_count FROM customers
UNION ALL
SELECT 'products', COUNT(*) FROM products
UNION ALL
SELECT 'orders', COUNT(*) FROM orders
UNION ALL
SELECT 'order_items', COUNT(*) FROM order_items
UNION ALL
SELECT 'store_locations', COUNT(*) FROM store_locations;
EOSQL

SPATIAL_TYPES="$(query_scalar <<'EOSQL'
SELECT GROUP_CONCAT(DISTINCT CASE
    WHEN data_type = 'geomcollection' THEN 'GEOMETRYCOLLECTION'
    ELSE UPPER(data_type)
END ORDER BY CASE WHEN data_type = 'geomcollection' THEN 'GEOMETRYCOLLECTION' ELSE UPPER(data_type) END SEPARATOR ',')
FROM information_schema.columns
WHERE table_schema = DATABASE()
  AND data_type IN ('geometry', 'point', 'linestring', 'polygon', 'multipoint', 'multilinestring', 'multipolygon', 'geomcollection');
EOSQL
)"
EXPECTED_SPATIAL_TYPES="GEOMETRY,GEOMETRYCOLLECTION,LINESTRING,MULTILINESTRING,MULTIPOINT,MULTIPOLYGON,POINT,POLYGON"
if [ "$SPATIAL_TYPES" != "$EXPECTED_SPATIAL_TYPES" ]; then
  echo "MySQL Spatial 类型校验失败: $SPATIAL_TYPES" >&2
  exit 1
fi

INVALID_GEOMETRIES="$(query_scalar <<'EOSQL'
SELECT SUM(invalid_count) FROM (
    SELECT SUM(NOT ST_IsValid(geom)) AS invalid_count FROM store_locations
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM delivery_routes
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM service_areas
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM regional_facilities
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM transport_networks
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM market_regions
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM spatial_collections
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM mixed_features
    UNION ALL SELECT SUM(NOT ST_IsValid(geom)) FROM projected_locations
    UNION ALL SELECT SUM(NOT ST_IsValid(center)) + SUM(coverage IS NOT NULL AND NOT ST_IsValid(coverage)) FROM spatial_multi_columns
) validation;
EOSQL
)"
if [ "$INVALID_GEOMETRIES" != "0" ]; then
  echo "MySQL Spatial 几何有效性校验失败: $INVALID_GEOMETRIES" >&2
  exit 1
fi

SPATIAL_INDEX_COUNT="$(query_scalar <<'EOSQL'
SELECT COUNT(DISTINCT table_name, index_name)
FROM information_schema.statistics
WHERE table_schema = DATABASE() AND index_type = 'SPATIAL';
EOSQL
)"
if [ "$SPATIAL_INDEX_COUNT" -lt 10 ]; then
  echo "MySQL Spatial 索引校验失败: $SPATIAL_INDEX_COUNT" >&2
  exit 1
fi

echo "=== MySQL 测试数据初始化完成 ==="
