-- ============================================================
-- 04-advanced/dynamic_partition.sql
-- Doris 动态分区自动管理
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 动态分区概述
-- ============================================================

/*
动态分区（Dynamic Partition）是 Doris 的自动分区管理特性：

**核心功能**：
- 自动创建：根据时间自动创建未来分区
- 自动删除：自动删除过期历史分区
- 无需人工：完全自动化管理

**适用场景**：
- 时间序列数据（日志、监控、订单）
- 需要定期清理历史数据
- 分区按时间规律增长

**优势**：
- 自动化运维，降低管理成本
- 避免写入失败（提前创建分区）
- 自动清理，控制存储成本
*/


-- ============================================================
-- 2. 创建动态分区表
-- ============================================================

-- 示例1：按天动态分区（最常用）
CREATE TABLE IF NOT EXISTS logs_dynamic (
    log_time DATETIME COMMENT '日志时间',
    log_level VARCHAR(20) COMMENT '日志级别：INFO/WARN/ERROR',
    service VARCHAR(100) COMMENT '服务名称',
    message TEXT COMMENT '日志内容'
)
DUPLICATE KEY(log_time)
COMMENT '日志表 - 动态分区（按天）'
PARTITION BY RANGE(log_time) ()  -- 空的分区定义
DISTRIBUTED BY HASH(log_time) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",              -- 启用动态分区
    "dynamic_partition.time_unit" = "DAY",            -- 时间单位：DAY/WEEK/MONTH
    "dynamic_partition.start" = "-7",                 -- 保留最近 7 天
    "dynamic_partition.end" = "3",                    -- 提前创建未来 3 天
    "dynamic_partition.prefix" = "p",                 -- 分区名前缀
    "dynamic_partition.buckets" = "10",               -- 分桶数
    "dynamic_partition.create_history_partition" = "true",  -- 创建历史分区
    "dynamic_partition.history_partition_num" = "7",  -- 历史分区数量
    "replication_num" = "1"                           -- 副本数
);

-- 插入测试数据
INSERT INTO logs_dynamic VALUES
(NOW(), 'INFO', 'api-service', 'Request received'),
(NOW(), 'WARN', 'db-service', 'Connection timeout'),
(DATE_SUB(NOW(), INTERVAL 1 DAY), 'ERROR', 'cache-service', 'Cache miss'),
(DATE_SUB(NOW(), INTERVAL 2 DAY), 'INFO', 'api-service', 'Response sent'),
(DATE_ADD(NOW(), INTERVAL 1 DAY), 'INFO', 'api-service', 'Future log');  -- 自动创建未来分区

-- 查看自动创建的分区
SHOW PARTITIONS FROM logs_dynamic;


-- ============================================================
-- 3. 按周动态分区
-- ============================================================

CREATE TABLE IF NOT EXISTS weekly_reports (
    report_time DATETIME COMMENT '报表时间',
    metric_name VARCHAR(100) COMMENT '指标名称',
    metric_value DOUBLE COMMENT '指标值'
)
DUPLICATE KEY(report_time)
COMMENT '周报表 - 动态分区（按周）'
PARTITION BY RANGE(report_time) ()
DISTRIBUTED BY HASH(metric_name) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",
    "dynamic_partition.time_unit" = "WEEK",           -- 按周分区
    "dynamic_partition.start" = "-4",                 -- 保留最近 4 周
    "dynamic_partition.end" = "2",                    -- 提前创建未来 2 周
    "dynamic_partition.prefix" = "p",
    "dynamic_partition.buckets" = "10",
    "dynamic_partition.start_day_of_week" = "1"       -- 周一作为一周开始
);

-- 查看分区
SHOW PARTITIONS FROM weekly_reports;


-- ============================================================
-- 4. 按月动态分区
-- ============================================================

CREATE TABLE IF NOT EXISTS monthly_sales (
    sale_time DATETIME COMMENT '销售时间',
    order_id BIGINT COMMENT '订单ID',
    amount DECIMAL(10,2) COMMENT '金额'
)
DUPLICATE KEY(sale_time, order_id)
COMMENT '月度销售表 - 动态分区（按月）'
PARTITION BY RANGE(sale_time) ()
DISTRIBUTED BY HASH(order_id) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",
    "dynamic_partition.time_unit" = "MONTH",          -- 按月分区
    "dynamic_partition.start" = "-12",                -- 保留最近 12 个月
    "dynamic_partition.end" = "3",                    -- 提前创建未来 3 个月
    "dynamic_partition.prefix" = "p",
    "dynamic_partition.buckets" = "10"
);

-- 插入测试数据
INSERT INTO monthly_sales VALUES
(NOW(), 1001, 599.99),
(DATE_SUB(NOW(), INTERVAL 1 MONTH), 1002, 799.99),
(DATE_SUB(NOW(), INTERVAL 2 MONTH), 1003, 999.99);

-- 查看分区
SHOW PARTITIONS FROM monthly_sales;


-- ============================================================
-- 5. 动态分区配置参数详解
-- ============================================================

/*
动态分区核心参数：

1. **dynamic_partition.enable** (required)
   - 值：true/false
   - 说明：是否启用动态分区

2. **dynamic_partition.time_unit** (required)
   - 值：DAY/WEEK/MONTH/YEAR
   - 说明：分区时间单位

3. **dynamic_partition.start** (required)
   - 值：负整数
   - 说明：保留多少个历史分区（-7 表示保留最近 7 天）

4. **dynamic_partition.end** (required)
   - 值：正整数
   - 说明：提前创建多少个未来分区（3 表示提前创建未来 3 天）

5. **dynamic_partition.prefix** (optional)
   - 值：字符串（默认 "p"）
   - 说明：分区名前缀

6. **dynamic_partition.buckets** (optional)
   - 值：整数
   - 说明：动态分区的分桶数（默认与表相同）

7. **dynamic_partition.create_history_partition** (optional)
   - 值：true/false（默认 false）
   - 说明：是否创建历史分区

8. **dynamic_partition.history_partition_num** (optional)
   - 值：整数
   - 说明：创建多少个历史分区

9. **dynamic_partition.start_day_of_week** (optional, for WEEK)
   - 值：1-7（1=周一，7=周日）
   - 说明：周分区的起始日

10. **dynamic_partition.start_day_of_month** (optional, for MONTH)
    - 值：1-28
    - 说明：月分区的起始日
*/


-- ============================================================
-- 6. 修改动态分区配置
-- ============================================================

-- 启用/禁用动态分区
ALTER TABLE logs_dynamic SET (
    "dynamic_partition.enable" = "false"  -- 禁用
);

ALTER TABLE logs_dynamic SET (
    "dynamic_partition.enable" = "true"   -- 启用
);

-- 修改保留时间
ALTER TABLE logs_dynamic SET (
    "dynamic_partition.start" = "-30"     -- 改为保留 30 天
);

-- 修改提前创建时间
ALTER TABLE logs_dynamic SET (
    "dynamic_partition.end" = "7"         -- 改为提前创建 7 天
);


-- ============================================================
-- 7. 查看动态分区信息
-- ============================================================

-- 查看表的动态分区配置
SHOW CREATE TABLE logs_dynamic\G

-- 查看当前分区情况
SHOW PARTITIONS FROM logs_dynamic;

-- 查看动态分区调度状态
SHOW DYNAMIC PARTITION TABLES FROM learning_db;


-- ============================================================
-- 8. 动态分区 vs 手动分区
-- ============================================================

-- 手动分区表（需要人工管理）
CREATE TABLE IF NOT EXISTS logs_manual (
    log_time DATETIME,
    message TEXT
)
DUPLICATE KEY(log_time)
PARTITION BY RANGE(log_time) (
    PARTITION p20250101 VALUES LESS THAN ("2025-01-02 00:00:00"),
    PARTITION p20250102 VALUES LESS THAN ("2025-01-03 00:00:00"),
    PARTITION p20250103 VALUES LESS THAN ("2025-01-04 00:00:00")
    -- 需要手动添加新分区：
    -- ALTER TABLE logs_manual ADD PARTITION p20250104 VALUES LESS THAN ("2025-01-05 00:00:00");
)
DISTRIBUTED BY HASH(log_time) BUCKETS 10;

-- 动态分区表（自动管理）
-- logs_dynamic（已创建，见上文）

/*
对比：

| 特性 | 手动分区 | 动态分区 |
|------|---------|---------|
| **创建分区** | 手动 SQL | 自动创建 |
| **删除分区** | 手动删除 | 自动删除 |
| **运维成本** | 高 | 低 |
| **适用场景** | 固定分区 | 时间序列数据 |
| **灵活性** | 高 | 中等 |

推荐：时间序列数据优先使用动态分区
*/


-- ============================================================
-- 9. 实战案例：监控指标表
-- ============================================================

CREATE TABLE IF NOT EXISTS metrics_history (
    metric_time DATETIME COMMENT '指标时间',
    host VARCHAR(100) COMMENT '主机名',
    metric_type VARCHAR(50) COMMENT '指标类型：cpu/memory/disk',
    metric_value DOUBLE COMMENT '指标值'
)
DUPLICATE KEY(metric_time, host)
COMMENT '监控指标历史表'
PARTITION BY RANGE(metric_time) ()
DISTRIBUTED BY HASH(host) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",
    "dynamic_partition.time_unit" = "DAY",
    "dynamic_partition.start" = "-30",                -- 保留 30 天
    "dynamic_partition.end" = "1",                    -- 提前创建 1 天
    "dynamic_partition.prefix" = "p",
    "dynamic_partition.buckets" = "10",
    "dynamic_partition.create_history_partition" = "true",
    "dynamic_partition.history_partition_num" = "30"
);

-- 插入监控数据
INSERT INTO metrics_history VALUES
(NOW(), 'server1', 'cpu', 75.5),
(NOW(), 'server1', 'memory', 82.3),
(NOW(), 'server2', 'cpu', 68.9),
(DATE_SUB(NOW(), INTERVAL 1 DAY), 'server1', 'disk', 55.0);

-- 查询最近 7 天的 CPU 指标
SELECT
    DATE(metric_time) as metric_date,
    host,
    AVG(metric_value) as avg_cpu
FROM metrics_history
WHERE metric_time >= DATE_SUB(NOW(), INTERVAL 7 DAY)
  AND metric_type = 'cpu'
GROUP BY DATE(metric_time), host
ORDER BY metric_date DESC, host;

-- 查看分区情况
SHOW PARTITIONS FROM metrics_history;


-- ============================================================
-- 10. 动态分区最佳实践
-- ============================================================

/*
动态分区使用建议：

1. **合理设置保留时间**：
   - 日志数据：7-30 天
   - 监控数据：30-90 天
   - 业务数据：根据法规要求（如 3-7 年）

2. **提前创建分区**：
   - 建议 end = 3-7（提前 3-7 天）
   - 避免写入时分区不存在导致失败

3. **选择合适的时间单位**：
   - 高频数据（日志、监控）：DAY
   - 中频数据（订单）：WEEK
   - 低频数据（报表）：MONTH

4. **监控动态分区**：
   - 定期查看分区数量：SHOW PARTITIONS
   - 检查分区调度状态：SHOW DYNAMIC PARTITION TABLES
   - 关注存储空间变化

5. **与其他特性结合**：
   - 动态分区 + Rollup：加速聚合查询
   - 动态分区 + Bitmap：用户行为分析
   - 动态分区 + 冷热分离：降低存储成本

6. **注意事项**：
   - 动态分区调度器每小时执行一次
   - 删除分区不可恢复，务必确认保留时间
   - 不要在动态分区表上手动添加/删除分区
*/


-- ============================================================
-- 11. 故障排查
-- ============================================================

-- 问题1：动态分区未生效
-- 解决：检查 FE 日志，确认动态分区调度器是否运行

-- 问题2：历史分区未自动删除
-- 解决：检查 start 参数是否正确，等待下一次调度

-- 问题3：未来分区未创建
-- 解决：检查 end 参数是否正确，手动触发调度

-- 查看动态分区调度状态
SHOW DYNAMIC PARTITION TABLES FROM learning_db;

-- 查看表属性
SHOW CREATE TABLE logs_dynamic\G


/*
动态分区总结：

1. **核心价值**：
   - 自动化：无需人工管理分区
   - 降低成本：自动清理历史数据
   - 避免故障：提前创建未来分区

2. **配置要点**：
   - time_unit：DAY/WEEK/MONTH
   - start：保留多少个历史分区（负数）
   - end：提前创建多少个未来分区（正数）

3. **适用场景**：
   - 时间序列数据（日志、监控、订单）
   - 需要定期清理的数据
   - 分区按时间规律增长

4. **最佳实践**：
   - 日志数据：保留 7-30 天
   - 监控数据：保留 30-90 天
   - 业务数据：根据法规要求
   - 提前创建：3-7 个分区

5. **与手动分区对比**：
   - 动态分区：自动化，运维成本低，适合时间序列数据
   - 手动分区：灵活，适合固定分区需求
   - 推荐：优先使用动态分区
*/
