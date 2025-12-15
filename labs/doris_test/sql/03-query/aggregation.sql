-- ============================================================
-- 03-query/aggregation.sql
-- Doris 聚合查询和优化技巧
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 准备测试数据
-- ============================================================

-- 创建订单表
CREATE TABLE IF NOT EXISTS orders (
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '商品ID',
    category VARCHAR(100) COMMENT '商品类别',
    amount DECIMAL(10,2) COMMENT '订单金额',
    quantity INT COMMENT '数量',
    order_date DATE COMMENT '订单日期',
    order_time DATETIME COMMENT '订单时间',
    status VARCHAR(20) COMMENT '订单状态'
)
DUPLICATE KEY(order_id, user_id)
COMMENT '订单表 - 聚合查询示例'
PARTITION BY RANGE(order_date) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01"),
    PARTITION p202503 VALUES LESS THAN ("2025-04-01")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 10;

-- 插入测试数据
INSERT INTO orders VALUES
(1001, 101, 201, 'Electronics', 299.99, 1, '2025-01-01', '2025-01-01 10:00:00', 'completed'),
(1002, 101, 202, 'Books', 49.99, 2, '2025-01-01', '2025-01-01 11:00:00', 'completed'),
(1003, 102, 203, 'Clothing', 199.99, 1, '2025-01-01', '2025-01-01 12:00:00', 'completed'),
(1004, 103, 204, 'Electronics', 599.99, 1, '2025-01-02', '2025-01-02 10:00:00', 'completed'),
(1005, 104, 205, 'Books', 79.99, 3, '2025-01-02', '2025-01-02 11:00:00', 'completed'),
(1006, 105, 206, 'Clothing', 299.99, 2, '2025-01-02', '2025-01-02 12:00:00', 'pending'),
(1007, 101, 207, 'Electronics', 399.99, 1, '2025-01-03', '2025-01-03 10:00:00', 'completed'),
(1008, 102, 208, 'Books', 29.99, 1, '2025-01-03', '2025-01-03 11:00:00', 'cancelled'),
(1009, 103, 209, 'Clothing', 499.99, 3, '2025-01-03', '2025-01-03 12:00:00', 'completed'),
(1010, 104, 210, 'Electronics', 899.99, 1, '2025-01-04', '2025-01-04 10:00:00', 'completed');


-- ============================================================
-- 2. 基础聚合函数
-- ============================================================

-- COUNT：计数
SELECT COUNT(*) as total_orders FROM orders;
SELECT COUNT(DISTINCT user_id) as unique_users FROM orders;

-- SUM：求和
SELECT SUM(amount) as total_amount FROM orders;
SELECT SUM(quantity) as total_quantity FROM orders;

-- AVG：平均值
SELECT AVG(amount) as avg_order_amount FROM orders;
SELECT AVG(quantity) as avg_quantity FROM orders;

-- MIN/MAX：最小值/最大值
SELECT
    MIN(amount) as min_amount,
    MAX(amount) as max_amount,
    MAX(amount) - MIN(amount) as amount_range
FROM orders;

-- 组合使用
SELECT
    COUNT(*) as order_count,
    COUNT(DISTINCT user_id) as user_count,
    SUM(amount) as total_amount,
    AVG(amount) as avg_amount,
    MIN(amount) as min_amount,
    MAX(amount) as max_amount
FROM orders;


-- ============================================================
-- 3. GROUP BY 分组聚合
-- ============================================================

-- 按类别统计
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    AVG(amount) as avg_amount,
    SUM(quantity) as total_quantity
FROM orders
GROUP BY category
ORDER BY total_amount DESC;

-- 按日期统计
SELECT
    order_date,
    COUNT(*) as order_count,
    SUM(amount) as daily_revenue,
    AVG(amount) as avg_order_value
FROM orders
GROUP BY order_date
ORDER BY order_date;

-- 按用户统计
SELECT
    user_id,
    COUNT(*) as order_count,
    SUM(amount) as total_spent,
    AVG(amount) as avg_order_value,
    MIN(order_date) as first_order,
    MAX(order_date) as last_order
FROM orders
GROUP BY user_id
ORDER BY total_spent DESC;

-- 多字段分组
SELECT
    category,
    status,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
GROUP BY category, status
ORDER BY category, status;


-- ============================================================
-- 4. HAVING 条件筛选（聚合后过滤）
-- ============================================================

-- 找出消费总额超过 500 的用户
SELECT
    user_id,
    COUNT(*) as order_count,
    SUM(amount) as total_spent
FROM orders
GROUP BY user_id
HAVING SUM(amount) > 500
ORDER BY total_spent DESC;

-- 找出订单数超过 1 的类别
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
GROUP BY category
HAVING COUNT(*) > 1
ORDER BY order_count DESC;

-- 组合 WHERE 和 HAVING
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
WHERE status = 'completed'  -- 先过滤（WHERE 在聚合前）
GROUP BY category
HAVING SUM(amount) > 300    -- 后过滤（HAVING 在聚合后）
ORDER BY total_amount DESC;


-- ============================================================
-- 5. 高级聚合：窗口函数
-- ============================================================

-- 计算每个用户的累计消费
SELECT
    user_id,
    order_date,
    amount,
    SUM(amount) OVER (PARTITION BY user_id ORDER BY order_date) as cumulative_amount
FROM orders
ORDER BY user_id, order_date;

-- 计算每个类别的排名
SELECT
    category,
    order_id,
    amount,
    RANK() OVER (PARTITION BY category ORDER BY amount DESC) as amount_rank
FROM orders
ORDER BY category, amount_rank;

-- 计算移动平均（最近3笔订单）
SELECT
    order_date,
    amount,
    AVG(amount) OVER (ORDER BY order_date ROWS BETWEEN 2 PRECEDING AND CURRENT ROW) as moving_avg_3
FROM orders
ORDER BY order_date;


-- ============================================================
-- 6. WITH ROLLUP：多维度汇总
-- ============================================================

-- 按类别和状态汇总，同时生成小计和总计
SELECT
    category,
    status,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
GROUP BY category, status WITH ROLLUP
ORDER BY category, status;


-- ============================================================
-- 7. CASE WHEN 条件聚合
-- ============================================================

-- 统计不同状态的订单数和金额
SELECT
    COUNT(*) as total_orders,
    COUNT(CASE WHEN status = 'completed' THEN 1 END) as completed_orders,
    COUNT(CASE WHEN status = 'pending' THEN 1 END) as pending_orders,
    COUNT(CASE WHEN status = 'cancelled' THEN 1 END) as cancelled_orders,
    SUM(CASE WHEN status = 'completed' THEN amount ELSE 0 END) as completed_amount,
    SUM(CASE WHEN status = 'pending' THEN amount ELSE 0 END) as pending_amount
FROM orders;

-- 按金额区间统计
SELECT
    CASE
        WHEN amount < 100 THEN '0-100'
        WHEN amount < 300 THEN '100-300'
        WHEN amount < 500 THEN '300-500'
        ELSE '500+'
    END as amount_range,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
GROUP BY
    CASE
        WHEN amount < 100 THEN '0-100'
        WHEN amount < 300 THEN '100-300'
        WHEN amount < 500 THEN '300-500'
        ELSE '500+'
    END
ORDER BY amount_range;


-- ============================================================
-- 8. 时间维度聚合
-- ============================================================

-- 按年月日统计
SELECT
    YEAR(order_date) as year,
    MONTH(order_date) as month,
    DAY(order_date) as day,
    COUNT(*) as order_count,
    SUM(amount) as daily_revenue
FROM orders
GROUP BY YEAR(order_date), MONTH(order_date), DAY(order_date)
ORDER BY year, month, day;

-- 按星期统计
SELECT
    DAYOFWEEK(order_date) as day_of_week,
    CASE DAYOFWEEK(order_date)
        WHEN 1 THEN 'Sunday'
        WHEN 2 THEN 'Monday'
        WHEN 3 THEN 'Tuesday'
        WHEN 4 THEN 'Wednesday'
        WHEN 5 THEN 'Thursday'
        WHEN 6 THEN 'Friday'
        WHEN 7 THEN 'Saturday'
    END as weekday_name,
    COUNT(*) as order_count,
    SUM(amount) as total_amount
FROM orders
GROUP BY DAYOFWEEK(order_date)
ORDER BY day_of_week;

-- 按小时统计
SELECT
    HOUR(order_time) as hour,
    COUNT(*) as order_count,
    SUM(amount) as hourly_revenue
FROM orders
GROUP BY HOUR(order_time)
ORDER BY hour;


-- ============================================================
-- 9. 子查询聚合
-- ============================================================

-- 找出高于平均订单金额的订单
SELECT
    order_id,
    user_id,
    amount
FROM orders
WHERE amount > (SELECT AVG(amount) FROM orders)
ORDER BY amount DESC;

-- 找出消费最多的前3个用户的订单
SELECT
    o.order_id,
    o.user_id,
    o.amount,
    u.total_spent
FROM orders o
JOIN (
    SELECT user_id, SUM(amount) as total_spent
    FROM orders
    GROUP BY user_id
    ORDER BY total_spent DESC
    LIMIT 3
) u ON o.user_id = u.user_id
ORDER BY u.total_spent DESC, o.order_date;


-- ============================================================
-- 10. 聚合性能优化技巧
-- ============================================================

-- 技巧1：使用分区裁剪加速聚合
EXPLAIN
SELECT
    order_date,
    COUNT(*) as order_count,
    SUM(amount) as daily_revenue
FROM orders
WHERE order_date >= '2025-01-01' AND order_date < '2025-02-01'  -- 分区裁剪
GROUP BY order_date;

-- 技巧2：使用 LIMIT 限制结果集
SELECT
    user_id,
    COUNT(*) as order_count,
    SUM(amount) as total_spent
FROM orders
GROUP BY user_id
ORDER BY total_spent DESC
LIMIT 10;  -- 只返回 TOP 10

-- 技巧3：避免 SELECT *，只选择需要的列
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount  -- 只选择需要的列
FROM orders
GROUP BY category;


-- ============================================================
-- 11. 实战案例：电商数据分析
-- ============================================================

-- 用户 RFM 分析（最近、频率、金额）
SELECT
    user_id,
    DATEDIFF(CURRENT_DATE(), MAX(order_date)) as recency,  -- 最近一次购买距今天数
    COUNT(*) as frequency,                                   -- 购买频率
    SUM(amount) as monetary                                  -- 消费金额
FROM orders
WHERE status = 'completed'
GROUP BY user_id
ORDER BY monetary DESC;

-- 用户留存分析（首单用户的复购情况）
SELECT
    first_order_date,
    COUNT(DISTINCT user_id) as new_users,
    COUNT(DISTINCT CASE WHEN order_count > 1 THEN user_id END) as retained_users,
    ROUND(COUNT(DISTINCT CASE WHEN order_count > 1 THEN user_id END) * 100.0 / COUNT(DISTINCT user_id), 2) as retention_rate
FROM (
    SELECT
        user_id,
        MIN(order_date) as first_order_date,
        COUNT(*) as order_count
    FROM orders
    GROUP BY user_id
) t
GROUP BY first_order_date
ORDER BY first_order_date;

-- 商品类别贡献分析
SELECT
    category,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    ROUND(SUM(amount) * 100.0 / (SELECT SUM(amount) FROM orders), 2) as revenue_contribution_pct,
    ROUND(AVG(amount), 2) as avg_order_value
FROM orders
WHERE status = 'completed'
GROUP BY category
ORDER BY total_amount DESC;


/*
聚合查询优化总结：

1. **基础聚合函数**：
   - COUNT(), SUM(), AVG(), MIN(), MAX()
   - COUNT(DISTINCT col) 用于去重计数

2. **分组聚合**：
   - GROUP BY：按一个或多个字段分组
   - HAVING：聚合后过滤（WHERE 是聚合前过滤）

3. **高级聚合**：
   - 窗口函数：SUM() OVER(), RANK() OVER()
   - WITH ROLLUP：生成小计和总计
   - CASE WHEN：条件聚合

4. **性能优化技巧**：
   - ✅ 使用分区裁剪（WHERE 条件包含分区字段）
   - ✅ 避免 SELECT *，只选择需要的列
   - ✅ 使用 LIMIT 限制结果集
   - ✅ 创建 Rollup 表预聚合（见 04-advanced/rollup.sql）
   - ✅ 使用 Aggregate 表模型自动预聚合

5. **常见场景**：
   - 销售报表：按日期、类别、用户统计
   - 用户分析：RFM 模型、留存分析
   - 商品分析：类别贡献、热销排行
*/
