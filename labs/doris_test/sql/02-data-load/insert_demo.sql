-- ============================================================
-- 02-data-load/insert_demo.sql
-- Doris INSERT 语句批量导入示例
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 创建测试表
-- ============================================================

CREATE TABLE IF NOT EXISTS user_behavior (
    user_id BIGINT COMMENT '用户ID',
    event_time DATETIME COMMENT '事件时间',
    event_type VARCHAR(50) COMMENT '事件类型：click, view, cart, purchase',
    product_id INT COMMENT '商品ID',
    category VARCHAR(100) COMMENT '商品类别',
    amount DECIMAL(10,2) COMMENT '金额'
)
DUPLICATE KEY(user_id, event_time)
COMMENT '用户行为表 - INSERT 导入示例'
PARTITION BY RANGE(event_time) (
    PARTITION p20250101 VALUES LESS THAN ("2025-01-02 00:00:00"),
    PARTITION p20250102 VALUES LESS THAN ("2025-01-03 00:00:00"),
    PARTITION p20250103 VALUES LESS THAN ("2025-01-04 00:00:00")
)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;


-- ============================================================
-- 2. 单条 INSERT（不推荐，性能差）
-- ============================================================

-- 插入单条数据
INSERT INTO user_behavior VALUES
(1001, '2025-01-01 10:00:00', 'view', 101, 'Electronics', 0.00);

-- 验证
SELECT * FROM user_behavior WHERE user_id = 1001;


-- ============================================================
-- 3. 批量 INSERT（推荐，性能提升 10-100 倍）
-- ============================================================

-- 一次插入多条数据（推荐方式）
INSERT INTO user_behavior VALUES
(1001, '2025-01-01 10:05:00', 'click', 101, 'Electronics', 0.00),
(1001, '2025-01-01 10:10:00', 'cart', 101, 'Electronics', 299.99),
(1002, '2025-01-01 11:00:00', 'view', 102, 'Books', 0.00),
(1002, '2025-01-01 11:05:00', 'purchase', 102, 'Books', 49.99),
(1003, '2025-01-01 12:00:00', 'view', 103, 'Clothing', 0.00),
(1003, '2025-01-01 12:10:00', 'click', 103, 'Clothing', 0.00),
(1003, '2025-01-01 12:15:00', 'cart', 103, 'Clothing', 199.99),
(1004, '2025-01-01 13:00:00', 'view', 104, 'Food', 0.00),
(1004, '2025-01-01 13:05:00', 'purchase', 104, 'Food', 19.99),
(1005, '2025-01-01 14:00:00', 'view', 105, 'Sports', 0.00);

-- 验证数据量
SELECT COUNT(*) as total_count FROM user_behavior;


-- ============================================================
-- 4. INSERT SELECT（从其他表导入数据）
-- ============================================================

-- 创建临时表
CREATE TABLE IF NOT EXISTS temp_user_behavior (
    user_id BIGINT,
    event_time DATETIME,
    event_type VARCHAR(50),
    product_id INT,
    category VARCHAR(100),
    amount DECIMAL(10,2)
)
DUPLICATE KEY(user_id, event_time)
DISTRIBUTED BY HASH(user_id) BUCKETS 4;

-- 插入临时数据
INSERT INTO temp_user_behavior VALUES
(2001, '2025-01-02 10:00:00', 'view', 201, 'Electronics', 0.00),
(2002, '2025-01-02 11:00:00', 'purchase', 202, 'Books', 99.99),
(2003, '2025-01-02 12:00:00', 'cart', 203, 'Clothing', 299.99);

-- 使用 INSERT SELECT 从临时表导入到主表
INSERT INTO user_behavior
SELECT * FROM temp_user_behavior;

-- 验证导入结果
SELECT * FROM user_behavior WHERE user_id >= 2001;


-- ============================================================
-- 5. 批量 INSERT 性能测试
-- ============================================================

-- 创建性能测试表
CREATE TABLE IF NOT EXISTS performance_test (
    id BIGINT,
    name VARCHAR(100),
    value INT,
    create_time DATETIME
)
DUPLICATE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 10;

-- 方式1：单条插入（不推荐，性能差）
-- INSERT INTO performance_test VALUES (1, 'test1', 100, NOW());
-- INSERT INTO performance_test VALUES (2, 'test2', 200, NOW());
-- ... 重复 1000 次

-- 方式2：批量插入（推荐，性能提升 10-100 倍）
INSERT INTO performance_test VALUES
(1, 'test1', 100, NOW()),
(2, 'test2', 200, NOW()),
(3, 'test3', 300, NOW()),
(4, 'test4', 400, NOW()),
(5, 'test5', 500, NOW()),
(6, 'test6', 600, NOW()),
(7, 'test7', 700, NOW()),
(8, 'test8', 800, NOW()),
(9, 'test9', 900, NOW()),
(10, 'test10', 1000, NOW());

-- 验证
SELECT COUNT(*) FROM performance_test;


-- ============================================================
-- 6. 使用事务（仅 Primary Key 表支持）
-- ============================================================

CREATE TABLE IF NOT EXISTS orders_transactional (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    amount DECIMAL(10,2) COMMENT '金额',
    status VARCHAR(20) COMMENT '状态',
    create_time DATETIME COMMENT '创建时间'
)
PRIMARY KEY(order_id)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 开启事务
BEGIN;

-- 插入订单数据
INSERT INTO orders_transactional VALUES
(10001, 1001, 299.99, 'pending', NOW()),
(10002, 1002, 199.99, 'pending', NOW());

-- 提交事务
COMMIT;

-- 验证
SELECT * FROM orders_transactional;


-- ============================================================
-- 7. 条件 INSERT（INSERT ... ON DUPLICATE KEY UPDATE）
-- ============================================================

CREATE TABLE IF NOT EXISTS user_stats (
    user_id BIGINT COMMENT '用户ID',
    total_amount DECIMAL(10,2) COMMENT '总消费金额',
    order_count INT COMMENT '订单数',
    last_update DATETIME COMMENT '最后更新时间'
)
UNIQUE KEY(user_id)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 首次插入
INSERT INTO user_stats VALUES
(1001, 299.99, 1, NOW());

-- 重复插入时更新（Unique 表自动替换）
INSERT INTO user_stats VALUES
(1001, 599.98, 2, NOW());

-- 验证（只保留最新数据）
SELECT * FROM user_stats WHERE user_id = 1001;


-- ============================================================
-- 8. 使用 CTE（公用表表达式）插入数据
-- ============================================================

-- 使用 WITH 子句生成数据
INSERT INTO performance_test
WITH generated_data AS (
    SELECT
        100 + id as id,
        CONCAT('generated_', id) as name,
        id * 10 as value,
        NOW() as create_time
    FROM (
        SELECT 1 as id UNION ALL
        SELECT 2 UNION ALL
        SELECT 3 UNION ALL
        SELECT 4 UNION ALL
        SELECT 5
    ) t
)
SELECT * FROM generated_data;

-- 验证
SELECT * FROM performance_test WHERE id >= 100;


-- ============================================================
-- 9. 分区插入优化
-- ============================================================

-- 插入数据到指定分区（自动路由）
INSERT INTO user_behavior VALUES
('2025-01-01 15:00:00', 3001, 'view', 301, 'Home', 0.00),       -- 进入 p20250101 分区
('2025-01-02 10:00:00', 3002, 'purchase', 302, 'Garden', 99.99), -- 进入 p20250102 分区
('2025-01-03 08:00:00', 3003, 'view', 303, 'Kitchen', 0.00);    -- 进入 p20250103 分区

-- 验证分区分布
SELECT
    CONCAT('p', DATE_FORMAT(event_time, '%Y%m%d')) as partition_name,
    COUNT(*) as count
FROM user_behavior
GROUP BY CONCAT('p', DATE_FORMAT(event_time, '%Y%m%d'));


-- ============================================================
-- 10. 数据导入最佳实践总结
-- ============================================================

/*
INSERT 导入性能优化建议：

1. **批量插入**：
   - ✅ 推荐：一次插入 1000-10000 行
   - ❌ 避免：单条单条插入（性能差 10-100 倍）

2. **事务控制**：
   - Primary Key 表支持事务（BEGIN/COMMIT）
   - 其他表模型不支持事务

3. **分区优化**：
   - 确保数据按分区字段分布
   - 避免跨分区大量写入

4. **性能对比**：
   - INSERT 单条：约 100-500 行/秒
   - INSERT 批量：约 1万-5万 行/秒
   - Stream Load：约 10万-50万 行/秒（推荐用于大批量导入）

5. **适用场景**：
   - INSERT：小批量数据（< 1万行）
   - Stream Load：大批量数据（> 10万行）
   - Broker Load：超大批量数据（> 100万行）

6. **注意事项**：
   - 避免频繁小批量 INSERT（影响性能）
   - 大数据量导入优先使用 Stream Load
   - 生产环境建议使用事务确保数据一致性（仅 Primary Key 表）
*/


-- ============================================================
-- 查看导入统计
-- ============================================================

-- 查看表数据量
SELECT COUNT(*) as total_rows FROM user_behavior;

-- 查看各事件类型分布
SELECT event_type, COUNT(*) as count
FROM user_behavior
GROUP BY event_type
ORDER BY count DESC;

-- 查看用户行为统计
SELECT
    user_id,
    COUNT(*) as event_count,
    SUM(CASE WHEN event_type = 'purchase' THEN amount ELSE 0 END) as total_amount
FROM user_behavior
GROUP BY user_id
ORDER BY total_amount DESC
LIMIT 10;
