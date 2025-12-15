-- ============================================================
-- 01-basic/table_models.sql
-- Doris 四种表模型示例：Duplicate、Aggregate、Unique、Primary Key
-- ============================================================

-- 创建学习数据库
CREATE DATABASE IF NOT EXISTS learning_db;
USE learning_db;

-- ============================================================
-- 1. Duplicate 模型（明细模型）
-- 用途：保留所有明细数据，不进行聚合
-- 适用场景：日志、明细查询、需要保留所有原始数据
-- ============================================================

CREATE TABLE IF NOT EXISTS user_events_duplicate (
    event_time DATETIME COMMENT '事件时间',
    user_id BIGINT COMMENT '用户ID',
    event_type VARCHAR(50) COMMENT '事件类型：click, view, purchase',
    page_url VARCHAR(200) COMMENT '页面URL',
    device VARCHAR(50) COMMENT '设备类型',
    duration INT COMMENT '停留时长（秒）'
)
DUPLICATE KEY(event_time, user_id)
COMMENT '用户行为明细表 - Duplicate 模型'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 插入示例数据
INSERT INTO user_events_duplicate VALUES
('2025-01-01 10:00:00', 1001, 'click', '/home', 'mobile', 30),
('2025-01-01 10:05:00', 1001, 'view', '/product/1', 'mobile', 45),
('2025-01-01 10:10:00', 1002, 'click', '/home', 'desktop', 20);

-- 查询：所有明细数据都保留
SELECT * FROM user_events_duplicate ORDER BY event_time;


-- ============================================================
-- 2. Aggregate 模型（聚合模型）
-- 用途：自动预聚合，节省存储空间
-- 适用场景：报表、指标统计、只需要聚合结果
-- ============================================================

CREATE TABLE IF NOT EXISTS sales_aggregate (
    date DATE COMMENT '日期',
    product_id INT COMMENT '产品ID',
    category VARCHAR(50) COMMENT '产品分类',
    sales_amount DECIMAL(10,2) SUM COMMENT '销售额（自动求和）',
    order_count INT SUM COMMENT '订单数（自动求和）',
    max_price DECIMAL(10,2) MAX COMMENT '最高价（自动取最大值）',
    min_price DECIMAL(10,2) MIN COMMENT '最低价（自动取最小值）'
)
AGGREGATE KEY(date, product_id, category)
COMMENT '销售聚合表 - Aggregate 模型'
DISTRIBUTED BY HASH(product_id) BUCKETS 10;

-- 插入示例数据（相同 Key 会自动聚合）
INSERT INTO sales_aggregate VALUES
('2025-01-01', 101, 'Electronics', 1000.00, 5, 300.00, 100.00),
('2025-01-01', 101, 'Electronics', 500.00, 2, 200.00, 150.00);  -- 会与上一行聚合

-- 查询：自动聚合后的结果
-- 销售额：1000 + 500 = 1500
-- 订单数：5 + 2 = 7
-- 最高价：MAX(300, 200) = 300
-- 最低价：MIN(100, 150) = 100
SELECT * FROM sales_aggregate;


-- ============================================================
-- 3. Unique 模型（去重模型）
-- 用途：主键唯一，自动去重（保留最新数据）
-- 适用场景：维度表、用户画像、需要去重的场景
-- ============================================================

CREATE TABLE IF NOT EXISTS user_profile_unique (
    user_id BIGINT COMMENT '用户ID（主键）',
    name VARCHAR(100) COMMENT '用户名',
    age INT COMMENT '年龄',
    city VARCHAR(50) COMMENT '城市',
    register_time DATETIME COMMENT '注册时间',
    last_login_time DATETIME COMMENT '最后登录时间'
)
UNIQUE KEY(user_id)
COMMENT '用户画像表 - Unique 模型'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 插入示例数据
INSERT INTO user_profile_unique VALUES
(1001, 'Alice', 25, 'Beijing', '2024-01-01 10:00:00', '2025-01-01 08:00:00');

-- 再次插入相同 user_id（会替换旧数据）
INSERT INTO user_profile_unique VALUES
(1001, 'Alice', 26, 'Shanghai', '2024-01-01 10:00:00', '2025-01-15 09:00:00');

-- 查询：只保留最新的数据
SELECT * FROM user_profile_unique WHERE user_id = 1001;


-- ============================================================
-- 4. Primary Key 模型（主键模型，支持更新删除）
-- 用途：支持行级更新和删除
-- 适用场景：订单、库存等需要更新的场景
-- 注意：性能略低于 Unique 模型，但功能更强大
-- ============================================================

CREATE TABLE IF NOT EXISTS orders_primary (
    order_id BIGINT COMMENT '订单ID（主键）',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '产品ID',
    status VARCHAR(20) COMMENT '订单状态：pending, paid, shipped, completed',
    amount DECIMAL(10,2) COMMENT '订单金额',
    create_time DATETIME COMMENT '创建时间',
    update_time DATETIME COMMENT '更新时间'
)
PRIMARY KEY(order_id)
COMMENT '订单表 - Primary Key 模型'
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 插入示例数据
INSERT INTO orders_primary VALUES
(10001, 1001, 101, 'pending', 299.99, '2025-01-01 10:00:00', '2025-01-01 10:00:00');

-- 更新订单状态（Primary Key 模型支持 UPDATE）
UPDATE orders_primary
SET status = 'paid', update_time = '2025-01-01 11:00:00'
WHERE order_id = 10001;

-- 查询：可以看到更新后的数据
SELECT * FROM orders_primary WHERE order_id = 10001;

-- 删除订单（Primary Key 模型支持 DELETE）
-- DELETE FROM orders_primary WHERE order_id = 10001;


-- ============================================================
-- 对比总结
-- ============================================================

-- 查看所有表
SHOW TABLES;

-- 查看表结构
DESC user_events_duplicate;
DESC sales_aggregate;
DESC user_profile_unique;
DESC orders_primary;

/*
表模型对比：

| 模型 | 特点 | 适用场景 | 是否去重 | 是否支持 UPDATE/DELETE |
|------|------|---------|---------|----------------------|
| Duplicate | 保留所有明细 | 日志、明细查询 | 否 | 否 |
| Aggregate | 自动预聚合 | 报表、指标统计 | 是（聚合） | 否 |
| Unique | 主键去重 | 维度表、用户画像 | 是（保留最新） | 否（仅替换） |
| Primary Key | 支持更新删除 | 订单、库存 | 是（保留最新） | 是 |

选择建议：
1. 需要明细数据 → Duplicate
2. 只需要聚合结果 → Aggregate
3. 需要去重但不更新 → Unique
4. 需要更新删除 → Primary Key
*/
