-- ============================================================
-- 05-integration/transfer_task.sql
-- ADDP Transfer 模块 Doris 数据传输任务示例
-- ============================================================

USE addp_test_db;

-- ============================================================
-- 1. 数据传输场景概述
-- ============================================================

/*
ADDP Transfer 模块支持的 Doris 数据传输场景：

**场景1：从 Doris 导出数据**
- 导出到 CSV 文件
- 导出到 JSON 文件
- 导出到其他数据库（PostgreSQL, MySQL等）

**场景2：向 Doris 导入数据**
- 从 CSV 文件导入
- 从 JSON 文件导入
- 从其他数据库导入
- 使用 Stream Load API（推荐）

**场景3：Doris 间数据同步**
- 表级别同步
- 增量同步
- 定时同步
*/


-- ============================================================
-- 2. 准备源数据表（导出场景）
-- ============================================================

-- 创建要导出的用户订单汇总表
CREATE TABLE IF NOT EXISTS export_user_orders (
    user_id BIGINT COMMENT '用户ID',
    username VARCHAR(100) COMMENT '用户名',
    email VARCHAR(255) COMMENT '邮箱',
    city VARCHAR(50) COMMENT '城市',
    order_count INT COMMENT '订单数',
    total_amount DECIMAL(18,2) COMMENT '总消费金额',
    first_order_date DATE COMMENT '首单日期',
    last_order_date DATE COMMENT '末单日期'
)
DUPLICATE KEY(user_id)
COMMENT 'Transfer 导出用户订单汇总表'
DISTRIBUTED BY HASH(user_id) BUCKETS 4;

-- 插入汇总数据
INSERT INTO export_user_orders
SELECT
    u.user_id,
    u.username,
    u.email,
    u.city,
    COUNT(o.order_id) as order_count,
    SUM(o.amount) as total_amount,
    MIN(DATE(o.order_time)) as first_order_date,
    MAX(DATE(o.order_time)) as last_order_date
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id AND o.status = 'completed'
GROUP BY u.user_id, u.username, u.email, u.city;

-- 查看导出数据
SELECT * FROM export_user_orders ORDER BY total_amount DESC;


-- ============================================================
-- 3. 场景1：从 Doris 导出数据到 CSV
-- ============================================================

/*
**Transfer 任务配置**：

任务名称：导出用户订单汇总到CSV
任务类型：导出任务
源配置：
  - 资源类型：Doris
  - 资源ID：1 (doris_business)
  - 数据库：addp_test_db
  - 查询SQL：
*/

-- 导出 SQL（Transfer 会执行此查询并导出结果）
SELECT
    user_id as "用户ID",
    username as "用户名",
    email as "邮箱",
    city as "城市",
    order_count as "订单数",
    total_amount as "总消费金额",
    first_order_date as "首单日期",
    last_order_date as "末单日期"
FROM addp_test_db.export_user_orders
ORDER BY total_amount DESC;

/*
目标配置：
  - 目标类型：CSV 文件
  - 文件路径：/data/exports/user_orders_{YYYYMMDD}.csv
  - 列分隔符：,
  - 包含表头：是
  - 字符编码：UTF-8

预期输出文件内容：
```csv
用户ID,用户名,邮箱,城市,订单数,总消费金额,首单日期,末单日期
1001,alice,alice@addp.com,Beijing,3,18998.00,2025-01-05,2025-01-10
1002,bob,bob@addp.com,Shanghai,1,89.00,2025-01-06,2025-01-06
1003,charlie,charlie@addp.com,Guangzhou,0,0.00,NULL,NULL
```
*/


-- ============================================================
-- 4. 场景2：从 CSV 导入数据到 Doris
-- ============================================================

-- 创建目标导入表
CREATE TABLE IF NOT EXISTS import_products (
    product_id INT COMMENT '商品ID',
    product_name VARCHAR(200) COMMENT '商品名称',
    category VARCHAR(100) COMMENT '类别',
    brand VARCHAR(100) COMMENT '品牌',
    price DECIMAL(10,2) COMMENT '价格',
    stock INT COMMENT '库存',
    import_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '导入时间'
)
UNIQUE KEY(product_id)
COMMENT 'Transfer 导入商品表'
DISTRIBUTED BY HASH(product_id) BUCKETS 4;

/*
**Transfer 任务配置**：

任务名称：从CSV导入商品数据
任务类型：导入任务
源配置：
  - 源类型：CSV 文件
  - 文件路径：/data/imports/products.csv
  - 列分隔符：,
  - 跳过表头行：1
  - 字符编码：UTF-8

目标配置：
  - 资源类型：Doris
  - 资源ID：1 (doris_business)
  - 数据库：addp_test_db
  - 表名：import_products

字段映射：
  CSV列 -> Doris字段
  product_id -> product_id
  product_name -> product_name
  category -> category
  brand -> brand
  price -> price
  stock -> stock

Transfer 模块会使用 Stream Load API 导入数据：
curl -u root: \
  -H "label:import_products_{timestamp}" \
  -H "format: csv" \
  -H "column_separator: ," \
  -H "skip_header: 1" \
  -T /data/imports/products.csv \
  http://doris-fe:8030/api/addp_test_db/import_products/_stream_load
*/

-- 验证导入结果
SELECT * FROM import_products ORDER BY product_id;
SELECT COUNT(*) as imported_count FROM import_products;


-- ============================================================
-- 5. 场景3：从 PostgreSQL 同步数据到 Doris
-- ============================================================

/*
**场景描述**：
从 ADDP 的 PostgreSQL 业务库同步用户数据到 Doris 进行分析

**Transfer 任务配置**：

任务名称：同步用户数据 PostgreSQL -> Doris
任务类型：同步任务
源配置：
  - 资源类型：PostgreSQL
  - 资源ID：2 (postgres_business)
  - 数据库：business_db
  - 表名：app_users

目标配置：
  - 资源类型：Doris
  - 资源ID：1 (doris_business)
  - 数据库：addp_test_db
  - 表名：sync_users

字段映射：
  PostgreSQL -> Doris
  id -> user_id
  name -> username
  email -> email
  phone_number -> phone
  city -> city
  created_at -> register_date
  is_active -> is_active
  last_login -> last_login_time

同步策略：
  - 同步类型：增量同步
  - 增量字段：updated_at
  - 调度周期：每小时
  - 冲突处理：REPLACE（更新已存在记录）
*/

-- 创建同步目标表
CREATE TABLE IF NOT EXISTS sync_users (
    user_id BIGINT COMMENT '用户ID',
    username VARCHAR(100) COMMENT '用户名',
    email VARCHAR(255) COMMENT '邮箱',
    phone VARCHAR(20) COMMENT '手机号',
    city VARCHAR(50) COMMENT '城市',
    register_date DATE COMMENT '注册日期',
    is_active BOOLEAN COMMENT '是否激活',
    last_login_time DATETIME COMMENT '最后登录时间',
    sync_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '同步时间'
)
UNIQUE KEY(user_id)
COMMENT 'Transfer 同步用户表（来自 PostgreSQL）'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 模拟同步后的数据查询
SELECT
    user_id,
    username,
    email,
    city,
    register_date,
    CASE WHEN is_active THEN '激活' ELSE '未激活' END as status,
    last_login_time,
    sync_time
FROM sync_users
ORDER BY sync_time DESC
LIMIT 10;


-- ============================================================
-- 6. 场景4：Doris 表间数据转换
-- ============================================================

/*
**场景描述**：
从明细订单表聚合生成用户消费统计表

**Transfer 任务配置**：

任务名称：聚合用户消费统计
任务类型：转换任务
源配置：
  - 资源类型：Doris
  - 资源ID：1 (doris_business)
  - 数据库：addp_test_db
  - 查询SQL：（见下文）

目标配置：
  - 资源类型：Doris（同一个实例）
  - 数据库：addp_test_db
  - 表名：user_consumption_stats

执行策略：
  - 执行方式：TRUNCATE + INSERT（全量替换）
  - 调度周期：每天 00:00
*/

-- 创建用户消费统计目标表
CREATE TABLE IF NOT EXISTS user_consumption_stats (
    stat_date DATE COMMENT '统计日期',
    user_id BIGINT COMMENT '用户ID',
    username VARCHAR(100) COMMENT '用户名',
    city VARCHAR(50) COMMENT '城市',
    order_count INT COMMENT '订单数',
    total_amount DECIMAL(18,2) COMMENT '总消费金额',
    avg_amount DECIMAL(10,2) COMMENT '平均订单金额',
    max_amount DECIMAL(10,2) COMMENT '最大订单金额',
    first_order_date DATE COMMENT '首单日期',
    last_order_date DATE COMMENT '末单日期',
    user_segment VARCHAR(50) COMMENT '用户分群'
)
DUPLICATE KEY(stat_date, user_id)
COMMENT 'Transfer 用户消费统计表'
PARTITION BY RANGE(stat_date) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01")
)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- Transfer 执行的聚合查询 SQL
INSERT INTO user_consumption_stats
SELECT
    CURRENT_DATE() as stat_date,
    u.user_id,
    u.username,
    u.city,
    COUNT(o.order_id) as order_count,
    COALESCE(SUM(o.amount), 0) as total_amount,
    COALESCE(AVG(o.amount), 0) as avg_amount,
    COALESCE(MAX(o.amount), 0) as max_amount,
    MIN(DATE(o.order_time)) as first_order_date,
    MAX(DATE(o.order_time)) as last_order_date,
    CASE
        WHEN COUNT(o.order_id) = 0 THEN '未消费用户'
        WHEN COUNT(o.order_id) <= 2 THEN '低频用户'
        WHEN COUNT(o.order_id) <= 5 THEN '中频用户'
        ELSE '高频用户'
    END as user_segment
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id AND o.status = 'completed'
GROUP BY u.user_id, u.username, u.city;

-- 查看统计结果
SELECT * FROM user_consumption_stats
ORDER BY total_amount DESC;


-- ============================================================
-- 7. Stream Load 导入性能测试
-- ============================================================

-- 创建大批量导入测试表
CREATE TABLE IF NOT EXISTS stream_load_test (
    id BIGINT COMMENT 'ID',
    data VARCHAR(200) COMMENT '数据',
    value INT COMMENT '值',
    create_time DATETIME COMMENT '创建时间'
)
DUPLICATE KEY(id)
COMMENT 'Stream Load 性能测试表'
DISTRIBUTED BY HASH(id) BUCKETS 10;

/*
**性能测试脚本**（Transfer 模块执行）：

#!/bin/bash
# 生成 10 万行测试数据
for i in {1..100000}; do
    echo "$i,test_data_$i,$((RANDOM % 1000)),$(date '+%Y-%m-%d %H:%M:%S')"
done > /tmp/stream_load_test.csv

# 使用 Stream Load 导入
time curl -u root: \
  -H "label:stream_load_test_$(date +%s)" \
  -H "format: csv" \
  -H "column_separator: ," \
  -T /tmp/stream_load_test.csv \
  http://doris-fe:8030/api/addp_test_db/stream_load_test/_stream_load

预期性能：
- 10 万行数据
- 导入时间：1-3 秒
- 导入速度：3-10 万行/秒
*/

-- 验证导入结果
SELECT COUNT(*) as total_rows FROM stream_load_test;
SELECT MIN(id) as min_id, MAX(id) as max_id FROM stream_load_test;


-- ============================================================
-- 8. Transfer 任务监控和验证
-- ============================================================

-- 验证1：检查数据完整性
SELECT
    'Source Count' as check_type,
    COUNT(*) as count
FROM export_user_orders

UNION ALL

SELECT
    'Target Count' as check_type,
    COUNT(*) as count
FROM user_consumption_stats;

-- 验证2：检查数据一致性（样本对比）
SELECT
    e.user_id,
    e.total_amount as export_amount,
    s.total_amount as stats_amount,
    ABS(e.total_amount - s.total_amount) as diff
FROM export_user_orders e
LEFT JOIN user_consumption_stats s ON e.user_id = s.user_id
WHERE ABS(e.total_amount - s.total_amount) > 0.01;

-- 验证3：检查增量同步状态
SELECT
    MAX(sync_time) as last_sync_time,
    COUNT(*) as synced_records,
    COUNT(DISTINCT user_id) as unique_users
FROM sync_users;


-- ============================================================
-- 9. Transfer 任务失败处理
-- ============================================================

/*
**常见失败场景和处理策略**：

1. **源数据库连接失败**
   - 重试策略：3 次，间隔 30 秒
   - 失败后：发送告警通知

2. **数据格式错误**
   - 容错策略：跳过错误行，记录日志
   - 最大错误率：1%

3. **目标表不存在**
   - 自动创建表：根据源表结构
   - 或者：终止任务并告警

4. **数据冲突**
   - REPLACE 模式：更新已存在记录
   - IGNORE 模式：跳过重复记录
   - ERROR 模式：终止任务

5. **Stream Load 失败**
   - 查看失败原因：解析返回的 JSON
   - 重试策略：3 次
   - 分批导入：大文件拆分成小批次
*/

-- 模拟错误数据检测
SELECT
    user_id,
    total_amount,
    CASE
        WHEN total_amount < 0 THEN 'Invalid: Negative Amount'
        WHEN total_amount > 1000000 THEN 'Warning: Suspicious High Amount'
        ELSE 'OK'
    END as validation_status
FROM user_consumption_stats
WHERE total_amount < 0 OR total_amount > 1000000;


-- ============================================================
-- 10. Transfer 最佳实践
-- ============================================================

/*
**Transfer 模块使用建议**：

1. **数据导入优先级**：
   - 首选：Stream Load（10-50 万行/秒）
   - 备选：Broker Load（大文件 > 1GB）
   - 避免：频繁 INSERT（性能差）

2. **批量大小**：
   - Stream Load：每批 100MB - 1GB
   - INSERT：每批 1000-10000 行

3. **并发控制**：
   - Stream Load：并发数 = CPU 核心数
   - 避免过高并发导致资源竞争

4. **增量同步**：
   - 使用时间戳字段（updated_at）
   - 使用自增 ID 字段
   - 记录最后同步位置

5. **错误处理**：
   - 设置合理的重试次数和间隔
   - 记录详细的失败日志
   - 发送告警通知

6. **性能优化**：
   - 合理设置分区和分桶
   - 使用 Unique 表的 REPLACE 模式
   - 避免全表扫描

7. **监控指标**：
   - 导入速度（行/秒）
   - 失败率（%）
   - 数据延迟（分钟）
   - 资源使用（CPU, Memory）
*/


/*
Transfer 集成总结：

1. **支持的数据源**：
   - 文件：CSV, JSON, Parquet
   - 数据库：PostgreSQL, MySQL, Doris
   - 流式：Kafka（未来支持）

2. **导入方式**：
   - Stream Load：推荐，高性能
   - Broker Load：大文件
   - INSERT：小批量

3. **任务类型**：
   - 导出任务：Doris -> 文件/其他数据库
   - 导入任务：文件/其他数据库 -> Doris
   - 同步任务：定期增量同步
   - 转换任务：Doris 内部数据转换

4. **调度策略**：
   - 一次性执行
   - 定时调度（Cron 表达式）
   - 触发式执行（事件驱动）

5. **性能基准**：
   - Stream Load：10-50 万行/秒
   - Broker Load：TB 级数据小时级导入
   - INSERT：千-万 行/秒

6. **与其他数据库对比**：
   - vs MySQL：性能提升 10-100 倍
   - vs PostgreSQL：OLAP 场景更优
   - vs Hive：实时性更好，运维更简单
*/
