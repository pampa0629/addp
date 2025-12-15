-- ============================================================
-- 01-basic/partition_bucket.sql
-- Doris 分区（Partition）和分桶（Bucket）实践
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 范围分区（Range Partition）
-- 用途：按时间或数字范围分区，便于数据管理和查询优化
-- 优势：分区裁剪加速查询、方便删除过期数据
-- ============================================================

CREATE TABLE IF NOT EXISTS logs_range_partition (
    log_time DATETIME COMMENT '日志时间',
    user_id BIGINT COMMENT '用户ID',
    action VARCHAR(100) COMMENT '操作',
    status INT COMMENT '状态码',
    message TEXT COMMENT '日志内容'
)
DUPLICATE KEY(log_time, user_id)
COMMENT '日志表 - 按日期范围分区'
PARTITION BY RANGE(log_time) (
    PARTITION p20250101 VALUES LESS THAN ("2025-01-02 00:00:00"),
    PARTITION p20250102 VALUES LESS THAN ("2025-01-03 00:00:00"),
    PARTITION p20250103 VALUES LESS THAN ("2025-01-04 00:00:00"),
    PARTITION p20250104 VALUES LESS THAN ("2025-01-05 00:00:00")
)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 插入不同日期的数据
INSERT INTO logs_range_partition VALUES
('2025-01-01 10:00:00', 1001, 'login', 200, 'User logged in'),
('2025-01-02 14:30:00', 1002, 'purchase', 200, 'Order created'),
('2025-01-03 09:15:00', 1003, 'logout', 200, 'User logged out');

-- 查看分区信息
SHOW PARTITIONS FROM logs_range_partition;

-- 查询时会自动进行分区裁剪（只扫描相关分区）
EXPLAIN SELECT * FROM logs_range_partition
WHERE log_time >= '2025-01-02' AND log_time < '2025-01-03';

-- 删除过期分区（实际生产中用于清理历史数据）
-- ALTER TABLE logs_range_partition DROP PARTITION p20250101;


-- ============================================================
-- 2. 动态分区（Dynamic Partition）
-- 用途：自动创建和删除分区，无需手动管理
-- 优势：自动化分区管理，适合时间序列数据
-- ============================================================

CREATE TABLE IF NOT EXISTS metrics_dynamic_partition (
    metric_time DATETIME COMMENT '指标时间',
    metric_name VARCHAR(100) COMMENT '指标名称',
    metric_value DOUBLE COMMENT '指标值',
    tags VARCHAR(500) COMMENT '标签'
)
DUPLICATE KEY(metric_time, metric_name)
COMMENT '监控指标表 - 动态分区'
PARTITION BY RANGE(metric_time) ()
DISTRIBUTED BY HASH(metric_name) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",
    "dynamic_partition.time_unit" = "DAY",
    "dynamic_partition.start" = "-7",      -- 保留最近 7 天
    "dynamic_partition.end" = "3",         -- 提前创建未来 3 天
    "dynamic_partition.prefix" = "p",
    "dynamic_partition.buckets" = "10",
    "dynamic_partition.create_history_partition" = "true"
);

-- 插入数据（分区会自动创建）
INSERT INTO metrics_dynamic_partition VALUES
(NOW(), 'cpu_usage', 75.5, 'host=server1'),
(NOW(), 'memory_usage', 82.3, 'host=server1'),
(NOW(), 'disk_usage', 65.8, 'host=server1');

-- 查看自动创建的分区
SHOW PARTITIONS FROM metrics_dynamic_partition;


-- ============================================================
-- 3. 列表分区（List Partition）
-- 用途：按枚举值分区（如地区、状态等）
-- 优势：适合离散值分区
-- ============================================================

CREATE TABLE IF NOT EXISTS orders_list_partition (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    region VARCHAR(50) COMMENT '地区',
    amount DECIMAL(10,2) COMMENT '金额',
    order_time DATETIME COMMENT '订单时间'
)
DUPLICATE KEY(order_id, user_id)
COMMENT '订单表 - 按地区列表分区'
PARTITION BY LIST(region) (
    PARTITION p_beijing VALUES IN ("Beijing", "Tianjin", "Hebei"),
    PARTITION p_shanghai VALUES IN ("Shanghai", "Jiangsu", "Zhejiang"),
    PARTITION p_guangdong VALUES IN ("Guangdong", "Shenzhen", "Guangzhou"),
    PARTITION p_other VALUES IN ()  -- 其他地区
)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 插入不同地区的数据
INSERT INTO orders_list_partition VALUES
(10001, 1001, 'Beijing', 299.99, '2025-01-01 10:00:00'),
(10002, 1002, 'Shanghai', 499.99, '2025-01-01 11:00:00'),
(10003, 1003, 'Guangdong', 199.99, '2025-01-01 12:00:00'),
(10004, 1004, 'Sichuan', 399.99, '2025-01-01 13:00:00');  -- 进入 p_other 分区

-- 查看分区信息
SHOW PARTITIONS FROM orders_list_partition;


-- ============================================================
-- 4. 分桶（Bucket）策略
-- 用途：数据在分区内进一步按哈希分桶，均匀分布
-- 建议：分桶数 = BE 节点数 × 2-4
-- ============================================================

-- 小表：少量分桶
CREATE TABLE IF NOT EXISTS small_table (
    id INT,
    name VARCHAR(100)
)
DUPLICATE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 4;  -- 小表 4 个桶

-- 中等表：适中分桶
CREATE TABLE IF NOT EXISTS medium_table (
    id BIGINT,
    data VARCHAR(500)
)
DUPLICATE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 10;  -- 中等表 10 个桶

-- 大表：较多分桶
CREATE TABLE IF NOT EXISTS large_table (
    id BIGINT,
    bigdata TEXT
)
DUPLICATE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 32;  -- 大表 32 个桶


-- ============================================================
-- 5. 分区和分桶最佳实践
-- ============================================================

-- 示例：订单表（按月分区 + 按订单ID分桶）
CREATE TABLE IF NOT EXISTS orders_best_practice (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '产品ID',
    status VARCHAR(20) COMMENT '订单状态',
    amount DECIMAL(10,2) COMMENT '订单金额',
    order_date DATE COMMENT '订单日期',
    create_time DATETIME COMMENT '创建时间'
)
DUPLICATE KEY(order_id, user_id)
COMMENT '订单表 - 最佳实践示例'
PARTITION BY RANGE(order_date) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01"),
    PARTITION p202503 VALUES LESS THAN ("2025-04-01")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 16
PROPERTIES (
    "replication_num" = "1",  -- 副本数（单节点设置为 1）
    "storage_medium" = "SSD"   -- 存储介质
);


-- ============================================================
-- 分区管理操作
-- ============================================================

-- 添加新分区
ALTER TABLE logs_range_partition
ADD PARTITION p20250105 VALUES LESS THAN ("2025-01-06 00:00:00");

-- 删除分区
-- ALTER TABLE logs_range_partition DROP PARTITION p20250101;

-- 查看分区详情
SHOW PARTITIONS FROM logs_range_partition\G

-- 查看表的分区和分桶配置
SHOW CREATE TABLE logs_range_partition\G


-- ============================================================
-- 性能对比测试
-- ============================================================

-- 无分区表
CREATE TABLE IF NOT EXISTS logs_no_partition (
    log_time DATETIME,
    user_id BIGINT,
    message TEXT
)
DUPLICATE KEY(log_time, user_id)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 查询对比（有分区 vs 无分区）
-- 有分区：只扫描 1 个分区
EXPLAIN SELECT COUNT(*) FROM logs_range_partition
WHERE log_time >= '2025-01-02' AND log_time < '2025-01-03';

-- 无分区：扫描全表
-- EXPLAIN SELECT COUNT(*) FROM logs_no_partition
-- WHERE log_time >= '2025-01-02' AND log_time < '2025-01-03';


/*
分区和分桶总结：

1. **分区（Partition）策略**：
   - 范围分区：按时间/数字范围（最常用）
   - 列表分区：按枚举值（地区、状态等）
   - 动态分区：自动管理（推荐用于时间序列数据）

2. **分桶（Bucket）策略**：
   - 小表：4-8 个桶
   - 中等表：10-16 个桶
   - 大表：32-64 个桶
   - 经验公式：分桶数 = BE 节点数 × 2-4

3. **最佳实践**：
   - 时间序列数据：使用动态分区
   - 按分区字段查询：确保 WHERE 条件包含分区列
   - 定期清理：删除过期分区
   - 均匀分布：选择合适的分桶键（高基数列）

4. **性能优势**：
   - 分区裁剪：只扫描相关分区，加速查询
   - 并行处理：多个分区/分桶并行处理
   - 数据管理：方便删除过期数据
*/
