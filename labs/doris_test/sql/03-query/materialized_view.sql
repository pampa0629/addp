-- ============================================================
-- 03-query/materialized_view.sql
-- Doris 物化视图（Rollup）和查询加速
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 什么是物化视图（Materialized View）
-- ============================================================

/*
物化视图（在 Doris 中也称为 Rollup）是预聚合的数据副本：

**优势**：
- 查询自动路由到物化视图，加速聚合查询
- 透明优化，无需修改查询SQL
- 适合OLAP场景的预计算

**与传统视图的区别**：
- 传统视图：逻辑视图，每次查询都执行原始SQL
- 物化视图：物理存储预聚合结果，查询直接读取

**使用场景**：
- 频繁的聚合查询（SUM, COUNT, AVG）
- 固定维度的报表查询
- 大表的预过滤和预聚合
*/


-- ============================================================
-- 2. 创建基础表
-- ============================================================

-- 创建详细的用户行为日志表
CREATE TABLE IF NOT EXISTS user_activity_log (
    log_time DATETIME COMMENT '日志时间',
    user_id BIGINT COMMENT '用户ID',
    page_url VARCHAR(200) COMMENT '页面URL',
    action VARCHAR(50) COMMENT '操作类型',
    device VARCHAR(50) COMMENT '设备类型',
    city VARCHAR(50) COMMENT '城市',
    duration INT COMMENT '停留时长（秒）'
)
DUPLICATE KEY(log_time, user_id)
COMMENT '用户行为日志表（基础表）'
PARTITION BY RANGE(log_time) (
    PARTITION p20250101 VALUES LESS THAN ("2025-01-02 00:00:00"),
    PARTITION p20250102 VALUES LESS THAN ("2025-01-03 00:00:00"),
    PARTITION p20250103 VALUES LESS THAN ("2025-01-04 00:00:00")
)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 插入测试数据
INSERT INTO user_activity_log VALUES
('2025-01-01 10:00:00', 1001, '/home', 'view', 'iOS', 'Beijing', 30),
('2025-01-01 10:05:00', 1001, '/product/1', 'click', 'iOS', 'Beijing', 45),
('2025-01-01 10:10:00', 1002, '/home', 'view', 'Android', 'Shanghai', 20),
('2025-01-01 11:00:00', 1002, '/product/2', 'click', 'Android', 'Shanghai', 60),
('2025-01-01 12:00:00', 1003, '/home', 'view', 'Web', 'Guangzhou', 15),
('2025-01-01 12:05:00', 1003, '/cart', 'add', 'Web', 'Guangzhou', 10),
('2025-01-02 10:00:00', 1001, '/home', 'view', 'iOS', 'Beijing', 25),
('2025-01-02 10:30:00', 1002, '/checkout', 'purchase', 'Android', 'Shanghai', 120),
('2025-01-02 11:00:00', 1004, '/home', 'view', 'Web', 'Shenzhen', 40),
('2025-01-02 11:30:00', 1005, '/product/3', 'click', 'iOS', 'Beijing', 50);

-- 查看原始数据
SELECT * FROM user_activity_log LIMIT 10;


-- ============================================================
-- 3. 创建 Rollup（物化视图）
-- ============================================================

-- Rollup 1: 按日期 + 城市聚合（用于地域分析）
ALTER TABLE user_activity_log
ADD ROLLUP rollup_daily_city (
    log_time,
    city,
    user_id,
    duration
);

-- Rollup 2: 按日期 + 设备聚合（用于设备分析）
ALTER TABLE user_activity_log
ADD ROLLUP rollup_daily_device (
    log_time,
    device,
    user_id,
    duration
);

-- Rollup 3: 按日期 + 操作类型聚合（用于行为分析）
ALTER TABLE user_activity_log
ADD ROLLUP rollup_daily_action (
    log_time,
    action,
    user_id
);

-- 查看 Rollup 创建状态
SHOW ALTER TABLE ROLLUP FROM learning_db\G

-- 等待 Rollup 创建完成（状态变为 FINISHED）
-- 注意：Rollup 创建是异步的，需要等待一段时间


-- ============================================================
-- 4. 验证 Rollup 自动路由
-- ============================================================

-- 查询1：按城市统计（会自动路由到 rollup_daily_city）
EXPLAIN
SELECT
    city,
    COUNT(DISTINCT user_id) as user_count,
    COUNT(*) as pv,
    SUM(duration) as total_duration
FROM user_activity_log
GROUP BY city;

-- 实际执行查询
SELECT
    city,
    COUNT(DISTINCT user_id) as user_count,
    COUNT(*) as pv,
    SUM(duration) as total_duration
FROM user_activity_log
GROUP BY city
ORDER BY pv DESC;

-- 查询2：按设备统计（会自动路由到 rollup_daily_device）
EXPLAIN
SELECT
    device,
    COUNT(DISTINCT user_id) as user_count,
    COUNT(*) as pv
FROM user_activity_log
GROUP BY device;

-- 查询3：按操作类型统计（会自动路由到 rollup_daily_action）
SELECT
    action,
    COUNT(DISTINCT user_id) as user_count,
    COUNT(*) as pv
FROM user_activity_log
GROUP BY action
ORDER BY pv DESC;


-- ============================================================
-- 5. Aggregate 表模型 + Rollup（更强大）
-- ============================================================

-- 创建 Aggregate 表（自动预聚合）
CREATE TABLE IF NOT EXISTS page_stats (
    stat_date DATE COMMENT '统计日期',
    page_url VARCHAR(200) COMMENT '页面URL',
    city VARCHAR(50) COMMENT '城市',
    pv BIGINT SUM COMMENT 'PV（自动求和）',
    uv_bitmap BITMAP BITMAP_UNION COMMENT 'UV（Bitmap去重）',
    total_duration BIGINT SUM COMMENT '总停留时长'
)
AGGREGATE KEY(stat_date, page_url, city)
COMMENT '页面统计表（Aggregate 模型）'
DISTRIBUTED BY HASH(page_url) BUCKETS 10;

-- 插入数据（相同 Key 会自动聚合）
INSERT INTO page_stats VALUES
('2025-01-01', '/home', 'Beijing', 100, TO_BITMAP(1001), 3000),
('2025-01-01', '/home', 'Beijing', 50, TO_BITMAP(1002), 1500),  -- 会与上一行聚合
('2025-01-01', '/home', 'Shanghai', 80, TO_BITMAP(2001), 2400),
('2025-01-01', '/product/1', 'Beijing', 60, TO_BITMAP(1001), 1800);

-- 查询聚合后的结果
SELECT
    stat_date,
    page_url,
    city,
    pv,
    BITMAP_COUNT(uv_bitmap) as uv,  -- 精确去重
    total_duration,
    ROUND(total_duration * 1.0 / pv, 2) as avg_duration
FROM page_stats
ORDER BY stat_date, pv DESC;


-- ============================================================
-- 6. 查看 Rollup 信息
-- ============================================================

-- 查看表的所有 Rollup
SHOW ALTER TABLE ROLLUP FROM learning_db;

-- 查看表结构（包括 Rollup）
SHOW CREATE TABLE user_activity_log\G

-- 查看 Rollup 的详细信息
DESC user_activity_log ALL\G


-- ============================================================
-- 7. Rollup 最佳实践
-- ============================================================

/*
Rollup 设计原则：

1. **选择高频查询维度**：
   - 分析常用的 GROUP BY 字段
   - 选择查询频率高的维度组合

2. **列的顺序很重要**：
   - Rollup 列顺序遵循前缀匹配原则
   - 常用过滤字段放在前面
   - 聚合字段放在后面

3. **Rollup 数量控制**：
   - 不要创建过多 Rollup（会占用存储空间）
   - 建议每个表 Rollup 数量 < 10 个
   - 权衡查询性能和存储成本

4. **Rollup vs Aggregate 表**：
   - Rollup：基于基础表，自动路由
   - Aggregate 表：独立表，手动写入
   - 推荐：Rollup 用于查询优化，Aggregate 用于预聚合
*/


-- ============================================================
-- 8. 删除 Rollup
-- ============================================================

-- 如果 Rollup 不再需要，可以删除
-- ALTER TABLE user_activity_log DROP ROLLUP rollup_daily_city;
-- ALTER TABLE user_activity_log DROP ROLLUP rollup_daily_device;
-- ALTER TABLE user_activity_log DROP ROLLUP rollup_daily_action;


-- ============================================================
-- 9. 实战案例：电商订单分析
-- ============================================================

-- 创建订单明细表
CREATE TABLE IF NOT EXISTS order_details (
    order_time DATETIME COMMENT '订单时间',
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '商品ID',
    category VARCHAR(100) COMMENT '类别',
    amount DECIMAL(10,2) COMMENT '金额',
    quantity INT COMMENT '数量'
)
DUPLICATE KEY(order_time, order_id)
COMMENT '订单明细表'
PARTITION BY RANGE(order_time) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01 00:00:00"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01 00:00:00")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 创建 Rollup：按日期 + 类别聚合
ALTER TABLE order_details
ADD ROLLUP rollup_daily_category (
    order_time,
    category,
    user_id,
    amount,
    quantity
);

-- 插入测试数据
INSERT INTO order_details VALUES
('2025-01-01 10:00:00', 1001, 101, 201, 'Electronics', 5999.00, 1),
('2025-01-01 11:00:00', 1002, 102, 202, 'Books', 49.99, 2),
('2025-01-01 12:00:00', 1003, 103, 203, 'Clothing', 599.00, 1),
('2025-01-02 10:00:00', 1004, 104, 204, 'Electronics', 12999.00, 1),
('2025-01-02 11:00:00', 1005, 105, 205, 'Books', 79.99, 3);

-- 查询：按类别统计销售额（自动路由到 Rollup）
SELECT
    DATE(order_time) as order_date,
    category,
    COUNT(DISTINCT user_id) as buyer_count,
    SUM(amount) as total_revenue,
    SUM(quantity) as total_quantity
FROM order_details
WHERE order_time >= '2025-01-01'
GROUP BY DATE(order_time), category
ORDER BY order_date, total_revenue DESC;


-- ============================================================
-- 10. 性能对比测试
-- ============================================================

-- 测试1：基础表全表扫描（无 Rollup）
-- 创建测试表（无 Rollup）
CREATE TABLE IF NOT EXISTS test_no_rollup (
    log_time DATETIME,
    user_id BIGINT,
    city VARCHAR(50),
    action VARCHAR(50)
)
DUPLICATE KEY(log_time, user_id)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 测试2：使用 Rollup 的表
-- user_activity_log（已创建 Rollup）

-- 对比查询性能（使用 EXPLAIN 查看执行计划）
EXPLAIN
SELECT city, COUNT(*) as pv
FROM user_activity_log
GROUP BY city;


/*
物化视图总结：

1. **Rollup 原理**：
   - 预聚合：提前计算聚合结果
   - 自动路由：查询自动选择最优 Rollup
   - 透明优化：无需修改查询 SQL

2. **性能提升**：
   - 聚合查询：10-100 倍加速
   - 扫描量减少：只扫描 Rollup（而非全表）
   - 适合 OLAP：预计算报表查询

3. **使用建议**：
   - 分析高频查询，创建对应 Rollup
   - 控制 Rollup 数量（< 10 个/表）
   - 使用 EXPLAIN 验证 Rollup 生效

4. **Aggregate 表 vs Rollup**：
   - Aggregate 表：独立表，手动维护
   - Rollup：依赖基础表，自动维护
   - 推荐：Rollup 优先，复杂场景用 Aggregate 表

5. **注意事项**：
   - Rollup 创建是异步的，需要等待完成
   - 占用额外存储空间
   - 写入性能略有下降（需要更新 Rollup）
*/
