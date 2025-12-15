-- ============================================================
-- 03-query/join_optimization.sql
-- Doris JOIN 查询和优化技巧
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 准备测试数据
-- ============================================================

-- 用户表（维度表）
CREATE TABLE IF NOT EXISTS users (
    user_id BIGINT COMMENT '用户ID',
    username VARCHAR(100) COMMENT '用户名',
    email VARCHAR(255) COMMENT '邮箱',
    city VARCHAR(50) COMMENT '城市',
    register_date DATE COMMENT '注册日期',
    is_vip BOOLEAN COMMENT '是否VIP'
)
UNIQUE KEY(user_id)
COMMENT '用户表'
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

INSERT INTO users VALUES
(101, 'Alice', 'alice@example.com', 'Beijing', '2024-01-01', TRUE),
(102, 'Bob', 'bob@example.com', 'Shanghai', '2024-02-01', FALSE),
(103, 'Charlie', 'charlie@example.com', 'Guangzhou', '2024-03-01', TRUE),
(104, 'David', 'david@example.com', 'Shenzhen', '2024-04-01', FALSE),
(105, 'Eve', 'eve@example.com', 'Beijing', '2024-05-01', TRUE);

-- 商品表（维度表）
CREATE TABLE IF NOT EXISTS products (
    product_id INT COMMENT '商品ID',
    product_name VARCHAR(200) COMMENT '商品名称',
    category VARCHAR(100) COMMENT '类别',
    price DECIMAL(10,2) COMMENT '价格',
    stock INT COMMENT '库存'
)
UNIQUE KEY(product_id)
COMMENT '商品表'
DISTRIBUTED BY HASH(product_id) BUCKETS 10;

INSERT INTO products VALUES
(201, 'iPhone 15', 'Electronics', 5999.00, 100),
(202, 'Python Programming', 'Books', 49.99, 500),
(203, 'Nike Shoes', 'Clothing', 599.00, 200),
(204, 'MacBook Pro', 'Electronics', 12999.00, 50),
(205, 'Data Science Guide', 'Books', 79.99, 300),
(206, 'Adidas Jacket', 'Clothing', 899.00, 150),
(207, 'AirPods Pro', 'Electronics', 1999.00, 80),
(208, 'Clean Code', 'Books', 59.99, 400);

-- 订单表（事实表，已在 aggregation.sql 中创建，这里重新插入数据）
TRUNCATE TABLE orders;

INSERT INTO orders VALUES
(1001, 101, 201, 'Electronics', 5999.00, 1, '2025-01-01', '2025-01-01 10:00:00', 'completed'),
(1002, 101, 202, 'Books', 49.99, 1, '2025-01-01', '2025-01-01 11:00:00', 'completed'),
(1003, 102, 203, 'Clothing', 599.00, 1, '2025-01-02', '2025-01-02 10:00:00', 'completed'),
(1004, 103, 204, 'Electronics', 12999.00, 1, '2025-01-02', '2025-01-02 11:00:00', 'completed'),
(1005, 104, 205, 'Books', 79.99, 2, '2025-01-03', '2025-01-03 10:00:00', 'completed'),
(1006, 105, 206, 'Clothing', 899.00, 1, '2025-01-03', '2025-01-03 11:00:00', 'pending'),
(1007, 101, 207, 'Electronics', 1999.00, 1, '2025-01-04', '2025-01-04 10:00:00', 'completed'),
(1008, 102, 208, 'Books', 59.99, 1, '2025-01-04', '2025-01-04 11:00:00', 'cancelled'),
(1009, 103, 201, 'Electronics', 5999.00, 1, '2025-01-05', '2025-01-05 10:00:00', 'completed'),
(1010, 104, 202, 'Books', 49.99, 3, '2025-01-05', '2025-01-05 11:00:00', 'completed');


-- ============================================================
-- 2. INNER JOIN（内连接）
-- ============================================================

-- 基础 INNER JOIN：订单 + 用户信息
SELECT
    o.order_id,
    o.user_id,
    u.username,
    u.city,
    o.amount,
    o.order_date
FROM orders o
INNER JOIN users u ON o.user_id = u.user_id
WHERE o.status = 'completed'
ORDER BY o.order_date;

-- 三表 JOIN：订单 + 用户 + 商品
SELECT
    o.order_id,
    u.username,
    u.city,
    p.product_name,
    p.category,
    o.quantity,
    o.amount,
    o.order_date
FROM orders o
INNER JOIN users u ON o.user_id = u.user_id
INNER JOIN products p ON o.product_id = p.product_id
WHERE o.status = 'completed'
ORDER BY o.order_date;


-- ============================================================
-- 3. LEFT JOIN（左外连接）
-- ============================================================

-- 查询所有用户及其订单（包括未下单的用户）
SELECT
    u.user_id,
    u.username,
    COUNT(o.order_id) as order_count,
    COALESCE(SUM(o.amount), 0) as total_spent
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id AND o.status = 'completed'
GROUP BY u.user_id, u.username
ORDER BY total_spent DESC;

-- 找出未购买的用户
SELECT
    u.user_id,
    u.username,
    u.email
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id
WHERE o.order_id IS NULL;


-- ============================================================
-- 4. RIGHT JOIN（右外连接）
-- ============================================================

-- 查询所有商品及其销售情况（包括未售出的商品）
SELECT
    p.product_id,
    p.product_name,
    p.category,
    p.price,
    COUNT(o.order_id) as sold_count,
    COALESCE(SUM(o.quantity), 0) as total_quantity
FROM orders o
RIGHT JOIN products p ON o.product_id = p.product_id AND o.status = 'completed'
GROUP BY p.product_id, p.product_name, p.category, p.price
ORDER BY sold_count DESC;


-- ============================================================
-- 5. CROSS JOIN（笛卡尔积，慎用）
-- ============================================================

-- 生成用户和商品的所有组合（用于推荐系统）
SELECT
    u.user_id,
    u.username,
    p.product_id,
    p.product_name
FROM users u
CROSS JOIN products p
WHERE u.is_vip = TRUE  -- 限制范围，避免数据爆炸
LIMIT 20;


-- ============================================================
-- 6. SEMI JOIN（半连接，使用 IN 或 EXISTS）
-- ============================================================

-- 找出有订单的用户（使用 IN）
SELECT
    user_id,
    username,
    email
FROM users
WHERE user_id IN (
    SELECT DISTINCT user_id
    FROM orders
    WHERE status = 'completed'
);

-- 找出有订单的用户（使用 EXISTS，性能更好）
SELECT
    u.user_id,
    u.username,
    u.email
FROM users u
WHERE EXISTS (
    SELECT 1
    FROM orders o
    WHERE o.user_id = u.user_id
    AND o.status = 'completed'
);


-- ============================================================
-- 7. ANTI JOIN（反连接，使用 NOT IN 或 NOT EXISTS）
-- ============================================================

-- 找出没有订单的用户（使用 NOT IN）
SELECT
    user_id,
    username,
    email
FROM users
WHERE user_id NOT IN (
    SELECT DISTINCT user_id
    FROM orders
);

-- 找出没有订单的用户（使用 NOT EXISTS，性能更好）
SELECT
    u.user_id,
    u.username,
    u.email
FROM users u
WHERE NOT EXISTS (
    SELECT 1
    FROM orders o
    WHERE o.user_id = u.user_id
);


-- ============================================================
-- 8. JOIN 性能优化技巧
-- ============================================================

-- 技巧1：使用 EXPLAIN 查看执行计划
EXPLAIN
SELECT
    o.order_id,
    u.username,
    p.product_name,
    o.amount
FROM orders o
INNER JOIN users u ON o.user_id = u.user_id
INNER JOIN products p ON o.product_id = p.product_id
WHERE o.order_date >= '2025-01-01';

-- 技巧2：小表 JOIN 大表（Broadcast Join）
-- Doris 自动识别小表并广播到所有节点

-- 技巧3：大表 JOIN 大表（Shuffle Join）
-- Doris 自动按 JOIN 键重新分布数据

-- 技巧4：Colocate Join（表共同分布优化）
-- 将经常 JOIN 的表按相同键分桶，避免数据 Shuffle
-- ALTER TABLE orders SET ("colocate_with" = "order_group");
-- ALTER TABLE order_items SET ("colocate_with" = "order_group");


-- ============================================================
-- 9. JOIN 与聚合结合
-- ============================================================

-- 用户消费排行榜
SELECT
    u.user_id,
    u.username,
    u.city,
    u.is_vip,
    COUNT(o.order_id) as order_count,
    SUM(o.amount) as total_spent,
    AVG(o.amount) as avg_order_value
FROM users u
LEFT JOIN orders o ON u.user_id = o.user_id AND o.status = 'completed'
GROUP BY u.user_id, u.username, u.city, u.is_vip
ORDER BY total_spent DESC;

-- 商品类别销售分析
SELECT
    p.category,
    COUNT(DISTINCT p.product_id) as product_count,
    COUNT(o.order_id) as order_count,
    SUM(o.amount) as total_revenue,
    AVG(o.amount) as avg_order_value
FROM products p
LEFT JOIN orders o ON p.product_id = o.product_id AND o.status = 'completed'
GROUP BY p.category
ORDER BY total_revenue DESC;


-- ============================================================
-- 10. 自连接（Self Join）
-- ============================================================

-- 找出在同一天购买过多次的用户
SELECT
    o1.user_id,
    o1.order_date,
    COUNT(*) as order_count
FROM orders o1
INNER JOIN orders o2
    ON o1.user_id = o2.user_id
    AND o1.order_date = o2.order_date
    AND o1.order_id < o2.order_id  -- 避免重复计数
GROUP BY o1.user_id, o1.order_date
HAVING COUNT(*) >= 1;


-- ============================================================
-- 11. 窗口函数 + JOIN
-- ============================================================

-- 为每个用户的订单添加累计消费金额
SELECT
    u.username,
    o.order_id,
    o.order_date,
    o.amount,
    SUM(o.amount) OVER (PARTITION BY o.user_id ORDER BY o.order_date) as cumulative_spent
FROM orders o
INNER JOIN users u ON o.user_id = u.user_id
WHERE o.status = 'completed'
ORDER BY u.username, o.order_date;


-- ============================================================
-- 12. 实战案例：复杂 JOIN 查询
-- ============================================================

-- 案例1：找出 VIP 用户购买 Electronics 类别的订单
SELECT
    u.user_id,
    u.username,
    u.city,
    p.product_name,
    o.amount,
    o.order_date
FROM orders o
INNER JOIN users u ON o.user_id = u.user_id
INNER JOIN products p ON o.product_id = p.product_id
WHERE u.is_vip = TRUE
  AND p.category = 'Electronics'
  AND o.status = 'completed'
ORDER BY o.order_date DESC;

-- 案例2：用户首单和复购分析
SELECT
    u.user_id,
    u.username,
    u.city,
    first_order.first_order_date,
    first_order.first_order_amount,
    user_stats.total_orders,
    user_stats.total_spent,
    CASE
        WHEN user_stats.total_orders = 1 THEN 'New Customer'
        WHEN user_stats.total_orders BETWEEN 2 AND 5 THEN 'Regular Customer'
        ELSE 'Loyal Customer'
    END as customer_segment
FROM users u
LEFT JOIN (
    SELECT
        user_id,
        MIN(order_date) as first_order_date,
        SUM(CASE WHEN order_date = (SELECT MIN(order_date) FROM orders o2 WHERE o2.user_id = orders.user_id) THEN amount ELSE 0 END) as first_order_amount
    FROM orders
    WHERE status = 'completed'
    GROUP BY user_id
) first_order ON u.user_id = first_order.user_id
LEFT JOIN (
    SELECT
        user_id,
        COUNT(*) as total_orders,
        SUM(amount) as total_spent
    FROM orders
    WHERE status = 'completed'
    GROUP BY user_id
) user_stats ON u.user_id = user_stats.user_id
ORDER BY user_stats.total_spent DESC;

-- 案例3：商品推荐（购买了 A 商品的用户还购买了什么）
SELECT
    p1.product_name as purchased_product,
    p2.product_name as also_purchased,
    COUNT(DISTINCT o1.user_id) as user_count
FROM orders o1
INNER JOIN orders o2
    ON o1.user_id = o2.user_id
    AND o1.product_id != o2.product_id
INNER JOIN products p1 ON o1.product_id = p1.product_id
INNER JOIN products p2 ON o2.product_id = p2.product_id
WHERE o1.status = 'completed'
  AND o2.status = 'completed'
  AND p1.product_id = 201  -- iPhone 15
GROUP BY p1.product_name, p2.product_name
ORDER BY user_count DESC
LIMIT 5;


/*
JOIN 优化总结：

1. **JOIN 类型选择**：
   - INNER JOIN：只返回匹配的记录
   - LEFT/RIGHT JOIN：返回左/右表所有记录
   - SEMI JOIN (IN/EXISTS)：判断是否存在匹配
   - ANTI JOIN (NOT IN/NOT EXISTS)：判断是否不存在匹配

2. **性能优化技巧**：
   - ✅ 小表 JOIN 大表：Doris 自动 Broadcast Join
   - ✅ 大表 JOIN 大表：使用 Shuffle Join 或 Colocate Join
   - ✅ 使用 EXISTS 替代 IN（性能更好）
   - ✅ 使用 NOT EXISTS 替代 NOT IN（避免 NULL 问题）
   - ✅ 合理使用索引（前缀索引、Bloom Filter）

3. **Colocate Join 优化**：
   - 将经常 JOIN 的表设置为 Colocate Group
   - 表必须按相同的键分桶
   - 避免数据 Shuffle，显著提升性能

4. **查看执行计划**：
   - 使用 EXPLAIN 查看 JOIN 类型（Broadcast/Shuffle/Colocate）
   - 检查是否有分区裁剪
   - 评估数据扫描量

5. **实际应用场景**：
   - 订单分析：订单 + 用户 + 商品
   - 用户分群：首单分析、RFM 模型
   - 商品推荐：协同过滤、关联规则
*/
