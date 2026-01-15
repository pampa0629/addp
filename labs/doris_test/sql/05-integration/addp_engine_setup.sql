-- ============================================================
-- 05-integration/addp_engine_setup.sql
-- ADDP 系统集成 Doris 引擎配置示例
-- ============================================================

/*
本文件展示如何在 ADDP 系统中配置和使用 Doris 数据库引擎。

前置条件：
1. Doris 集群已部署（business/docker-compose.yml）
2. ADDP System 模块已支持 Doris 资源类型
3. ADDP Meta 模块已支持 Doris 元数据扫描
*/

USE learning_db;

-- ============================================================
-- 1. 创建 ADDP 测试数据库和表
-- ============================================================

-- 创建 ADDP 测试数据库
CREATE DATABASE IF NOT EXISTS addp_test_db
COMMENT 'ADDP 集成测试数据库';

USE addp_test_db;

-- 用户表（Unique 模型）
CREATE TABLE IF NOT EXISTS users (
    user_id BIGINT COMMENT '用户ID',
    username VARCHAR(100) COMMENT '用户名',
    email VARCHAR(255) COMMENT '邮箱',
    phone VARCHAR(20) COMMENT '手机号',
    city VARCHAR(50) COMMENT '城市',
    register_date DATE COMMENT '注册日期',
    is_active BOOLEAN COMMENT '是否激活',
    last_login_time DATETIME COMMENT '最后登录时间'
)
UNIQUE KEY(user_id)
COMMENT 'ADDP 用户表'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 订单表（Duplicate 模型 + 分区）
CREATE TABLE IF NOT EXISTS orders (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_name VARCHAR(200) COMMENT '商品名称',
    category VARCHAR(100) COMMENT '商品类别',
    amount DECIMAL(10,2) COMMENT '订单金额',
    quantity INT COMMENT '数量',
    status VARCHAR(20) COMMENT '订单状态：pending/paid/shipped/completed/cancelled',
    order_time DATETIME COMMENT '下单时间',
    update_time DATETIME COMMENT '更新时间'
)
DUPLICATE KEY(order_id, user_id)
COMMENT 'ADDP 订单表'
PARTITION BY RANGE(order_time) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01 00:00:00"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01 00:00:00"),
    PARTITION p202503 VALUES LESS THAN ("2025-04-01 00:00:00")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 用户行为日志表（Duplicate 模型 + 动态分区）
CREATE TABLE IF NOT EXISTS user_activity_log (
    log_time DATETIME COMMENT '日志时间',
    user_id BIGINT COMMENT '用户ID',
    event_type VARCHAR(50) COMMENT '事件类型：login/view/click/purchase/logout',
    page_url VARCHAR(200) COMMENT '页面URL',
    ip_address VARCHAR(50) COMMENT 'IP地址',
    device VARCHAR(50) COMMENT '设备类型',
    duration INT COMMENT '停留时长（秒）'
)
DUPLICATE KEY(log_time, user_id)
COMMENT 'ADDP 用户行为日志表'
PARTITION BY RANGE(log_time) ()
DISTRIBUTED BY HASH(user_id) BUCKETS 10
PROPERTIES (
    "dynamic_partition.enable" = "true",
    "dynamic_partition.time_unit" = "DAY",
    "dynamic_partition.start" = "-7",
    "dynamic_partition.end" = "3",
    "dynamic_partition.prefix" = "p",
    "dynamic_partition.buckets" = "10"
);

-- 用户统计表（Aggregate 模型）
CREATE TABLE IF NOT EXISTS user_statistics (
    stat_date DATE COMMENT '统计日期',
    user_id BIGINT COMMENT '用户ID',
    login_count INT SUM COMMENT '登录次数',
    page_views BIGINT SUM COMMENT '页面浏览量',
    total_duration BIGINT SUM COMMENT '总停留时长（秒）',
    order_count INT SUM COMMENT '下单次数',
    total_amount DECIMAL(18,2) SUM COMMENT '总消费金额'
)
AGGREGATE KEY(stat_date, user_id)
COMMENT 'ADDP 用户统计表'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;


-- ============================================================
-- 2. 插入测试数据
-- ============================================================

-- 插入用户数据
INSERT INTO users VALUES
(1001, 'alice', 'alice@addp.com', '13800001001', 'Beijing', '2024-01-15', TRUE, '2025-01-10 10:30:00'),
(1002, 'bob', 'bob@addp.com', '13800001002', 'Shanghai', '2024-02-20', TRUE, '2025-01-09 14:20:00'),
(1003, 'charlie', 'charlie@addp.com', '13800001003', 'Guangzhou', '2024-03-10', TRUE, '2025-01-08 09:15:00'),
(1004, 'david', 'david@addp.com', '13800001004', 'Shenzhen', '2024-04-05', FALSE, '2024-12-25 16:45:00'),
(1005, 'eve', 'eve@addp.com', '13800001005', 'Beijing', '2024-05-12', TRUE, '2025-01-11 11:00:00');

-- 插入订单数据
INSERT INTO orders VALUES
(100001, 1001, 'MacBook Pro 16', 'Electronics', 16999.00, 1, 'completed', '2025-01-05 10:00:00', '2025-01-06 15:30:00'),
(100002, 1001, 'AirPods Pro', 'Electronics', 1999.00, 1, 'completed', '2025-01-05 10:30:00', '2025-01-06 15:30:00'),
(100003, 1002, 'Python编程书籍', 'Books', 89.00, 2, 'completed', '2025-01-06 14:00:00', '2025-01-07 10:00:00'),
(100004, 1003, 'Nike运动鞋', 'Clothing', 899.00, 1, 'shipped', '2025-01-07 09:00:00', '2025-01-08 11:00:00'),
(100005, 1004, 'iPad Air', 'Electronics', 4799.00, 1, 'paid', '2025-01-08 16:00:00', '2025-01-08 16:05:00'),
(100006, 1005, 'Adidas外套', 'Clothing', 599.00, 1, 'pending', '2025-01-09 11:00:00', '2025-01-09 11:00:00'),
(100007, 1001, 'iPhone 15 Pro', 'Electronics', 7999.00, 1, 'completed', '2025-01-10 10:00:00', '2025-01-11 14:00:00');

-- 插入用户行为日志
INSERT INTO user_activity_log VALUES
('2025-01-10 10:00:00', 1001, 'login', '/login', '192.168.1.100', 'Chrome/Mac', 0),
('2025-01-10 10:05:00', 1001, 'view', '/products', '192.168.1.100', 'Chrome/Mac', 120),
('2025-01-10 10:10:00', 1001, 'click', '/product/iphone15', '192.168.1.100', 'Chrome/Mac', 180),
('2025-01-10 10:15:00', 1001, 'purchase', '/checkout', '192.168.1.100', 'Chrome/Mac', 300),
('2025-01-10 11:00:00', 1002, 'login', '/login', '192.168.1.101', 'Safari/iOS', 0),
('2025-01-10 11:05:00', 1002, 'view', '/books', '192.168.1.101', 'Safari/iOS', 90),
('2025-01-10 11:30:00', 1003, 'login', '/login', '192.168.1.102', 'Chrome/Windows', 0),
('2025-01-10 11:35:00', 1003, 'view', '/clothing', '192.168.1.102', 'Chrome/Windows', 150);

-- 插入用户统计数据
INSERT INTO user_statistics VALUES
('2025-01-10', 1001, 3, 25, 1800, 2, 18998.00),
('2025-01-10', 1002, 2, 15, 900, 1, 89.00),
('2025-01-10', 1003, 1, 10, 600, 0, 0.00);


-- ============================================================
-- 3. ADDP System 模块资源注册示例
-- ============================================================

/*
在 ADDP System 前端创建 Doris 资源配置：

**资源基本信息**：
- 资源名称: doris_business
- 显示名称: ADDP 业务 Doris 数据库
- 资源类型: doris
- 描述: 用于 ADDP 业务数据分析的 Doris 集群

**连接信息** (connection_info JSON)：
{
  "host": "business-doris-fe",        // 或 "127.0.0.1"（开发环境）
  "port": "9030",                      // MySQL 协议端口
  "user": "root",
  "password": "",                      // 默认为空
  "database": "addp_test_db"
}

**扫描配置** (scan_config JSON)：
{
  "schedule_type": "manual",           // manual/immediate/daily/weekly
  "enabled": true,
  "cron_expression": ""                // 定时扫描表达式（可选）
}

**API 请求示例**：
POST http://localhost:8180/api/engines
Content-Type: application/json
Authorization: Bearer <JWT_TOKEN>

{
  "name": "doris_business",
  "display_name": "ADDP 业务 Doris 数据库",
  "engine_type": "doris",
  "description": "用于 ADDP 业务数据分析的 Doris 集群",
  "connection_info": {
    "host": "127.0.0.1",
    "port": "9030",
    "user": "root",
    "password": "",
    "database": "addp_test_db"
  },
  "scan_config": {
    "schedule_type": "manual",
    "enabled": true
  }
}
*/


-- ============================================================
-- 4. ADDP Meta 模块元数据扫描验证
-- ============================================================

/*
在 ADDP Meta 前端触发元数据扫描后，验证扫描结果：

**扫描内容**：
- 数据库列表：addp_test_db, learning_db
- 表列表：users, orders, user_activity_log, user_statistics
- 字段信息：字段名、数据类型、注释

**验证查询**（在 Doris 中执行）：
*/

-- 查看所有数据库
SHOW DATABASES;

-- 查看测试数据库的所有表
SHOW TABLES FROM addp_test_db;

-- 查看表结构（Meta 模块会扫描这些信息）
DESC addp_test_db.users;
DESC addp_test_db.orders;
DESC addp_test_db.user_activity_log;
DESC addp_test_db.user_statistics;

-- 查看表的详细信息
SHOW CREATE TABLE addp_test_db.users\G
SHOW CREATE TABLE addp_test_db.orders\G


-- ============================================================
-- 5. ADDP Develop 模块 SQL 工作台示例查询
-- ============================================================

/*
在 ADDP Develop 模块的 SQL 工作台中执行以下查询：
*/

-- 查询1：用户列表
SELECT
    user_id,
    username,
    email,
    city,
    register_date,
    CASE WHEN is_active THEN '激活' ELSE '未激活' END as status,
    last_login_time
FROM addp_test_db.users
ORDER BY last_login_time DESC;

-- 查询2：订单统计报表
SELECT
    DATE(order_time) as order_date,
    category,
    status,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    AVG(amount) as avg_amount
FROM addp_test_db.orders
GROUP BY DATE(order_time), category, status
ORDER BY order_date DESC, total_amount DESC;

-- 查询3：用户消费排行
SELECT
    u.user_id,
    u.username,
    u.city,
    COUNT(o.order_id) as order_count,
    SUM(o.amount) as total_spent,
    AVG(o.amount) as avg_order_value
FROM addp_test_db.users u
LEFT JOIN addp_test_db.orders o ON u.user_id = o.user_id AND o.status = 'completed'
GROUP BY u.user_id, u.username, u.city
ORDER BY total_spent DESC;

-- 查询4：用户行为分析
SELECT
    event_type,
    COUNT(*) as event_count,
    COUNT(DISTINCT user_id) as unique_users,
    AVG(duration) as avg_duration
FROM addp_test_db.user_activity_log
WHERE log_time >= DATE_SUB(NOW(), INTERVAL 7 DAY)
GROUP BY event_type
ORDER BY event_count DESC;

-- 查询5：用户活跃度分析
SELECT
    DATE(log_time) as activity_date,
    COUNT(DISTINCT user_id) as daily_active_users,
    COUNT(*) as total_events,
    SUM(duration) as total_duration
FROM addp_test_db.user_activity_log
WHERE log_time >= DATE_SUB(NOW(), INTERVAL 30 DAY)
GROUP BY DATE(log_time)
ORDER BY activity_date DESC;


-- ============================================================
-- 6. ADDP Transfer 模块数据传输示例
-- ============================================================

/*
在 ADDP Transfer 模块配置数据传输任务：

**场景1：从 Doris 导出数据到 CSV**
- 源数据库：Doris (addp_test_db)
- 源表：users
- 目标：CSV 文件
- 导出查询：
*/
SELECT
    user_id,
    username,
    email,
    phone,
    city,
    register_date,
    CASE WHEN is_active THEN 'Yes' ELSE 'No' END as is_active
FROM addp_test_db.users
WHERE is_active = TRUE;

/*
**场景2：从 PostgreSQL 导入数据到 Doris**

假设在 ADDP 的 PostgreSQL 业务库中有用户表，需要同步到 Doris：

步骤：
1. 在 Transfer 模块创建任务
2. 源：PostgreSQL 业务库
3. 目标：Doris (addp_test_db.users)
4. 字段映射：id -> user_id, name -> username, etc.
5. 同步策略：全量同步 或 增量同步

Transfer 模块会使用 Stream Load API 高性能导入数据到 Doris。
*/


-- ============================================================
-- 7. ADDP Manager 模块数据预览
-- ============================================================

/*
在 ADDP Manager 模块预览 Doris 表数据：

**预览配置**：
- 资源：doris_business
- 表：addp_test_db.users
- 限制：100 行

Manager 会执行类似以下查询：
*/
SELECT * FROM addp_test_db.users LIMIT 100;

-- 预览订单表
SELECT * FROM addp_test_db.orders LIMIT 100;

-- 预览用户行为日志
SELECT * FROM addp_test_db.user_activity_log
ORDER BY log_time DESC
LIMIT 100;


-- ============================================================
-- 8. ADDP 集成验证检查清单
-- ============================================================

/*
集成验证步骤：

✅ **1. System 模块**：
   - [ ] 成功创建 Doris 资源
   - [ ] 连接测试通过
   - [ ] 资源列表显示正常

✅ **2. Meta 模块**：
   - [ ] 元数据扫描成功
   - [ ] 数据库列表正确（addp_test_db）
   - [ ] 表列表正确（4 张表）
   - [ ] 字段信息完整

✅ **3. Manager 模块**：
   - [ ] 数据预览正常
   - [ ] 分页功能正常
   - [ ] 字段类型显示正确

✅ **4. Develop 模块**：
   - [ ] SQL 工作台连接成功
   - [ ] 查询执行正常
   - [ ] 结果集返回正确

✅ **5. Transfer 模块**：
   - [ ] 创建导入任务成功
   - [ ] 数据传输正常
   - [ ] 错误处理正确

✅ **6. 性能验证**：
   - [ ] 简单查询 < 1 秒
   - [ ] 聚合查询 < 2 秒
   - [ ] JOIN 查询 < 3 秒
*/


-- ============================================================
-- 9. 连接测试脚本
-- ============================================================

-- 在命令行测试 Doris 连接
/*
# 业务环境（business/docker-compose.yml 部署）
mysql -h127.0.0.1 -P9030 -uroot

# 学习环境（labs/doris_test/ 部署）
mysql -h127.0.0.1 -P19030 -uroot

# 连接后执行
USE addp_test_db;
SHOW TABLES;
SELECT COUNT(*) FROM users;
*/


-- ============================================================
-- 10. 常见问题排查
-- ============================================================

/*
**问题1：ADDP 无法连接 Doris**
- 检查 Doris FE 是否启动：docker ps | grep doris
- 检查端口是否正确：9030 (业务环境) 或 19030 (学习环境)
- 检查网络连通性：telnet 127.0.0.1 9030

**问题2：元数据扫描失败**
- 检查数据库是否存在：SHOW DATABASES;
- 检查用户权限：SHOW GRANTS FOR 'root';
- 查看 Meta 模块日志

**问题3：查询性能慢**
- 检查分区裁剪：EXPLAIN SELECT ...
- 检查是否使用索引
- 查看数据量：SELECT COUNT(*) ...

**问题4：数据导入失败**
- 检查表结构是否匹配
- 检查字段映射是否正确
- 查看 Transfer 模块任务日志
*/


/*
ADDP 集成总结：

1. **资源注册**（System 模块）：
   - 资源类型：doris
   - 连接信息：host, port, user, password, database
   - 扫描配置：schedule_type, enabled

2. **元数据管理**（Meta 模块）：
   - 自动扫描数据库、表、字段
   - 支持定时扫描和手动触发
   - 存储到 ADDP 元数据库

3. **数据预览**（Manager 模块）：
   - 支持表数据预览
   - 分页显示
   - 字段类型识别

4. **SQL 查询**（Develop 模块）：
   - SQL 工作台执行 Doris 查询
   - 支持复杂 OLAP 查询
   - 结果导出

5. **数据传输**（Transfer 模块）：
   - 从 Doris 导出数据
   - 向 Doris 导入数据（Stream Load）
   - 支持字段映射和转换

6. **性能优势**：
   - OLAP 查询：10-100 倍于 MySQL
   - 聚合查询：秒级响应
   - 大数据量：亿级数据支持
*/
