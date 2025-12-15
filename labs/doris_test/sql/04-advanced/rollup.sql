-- ============================================================
-- 04-advanced/rollup.sql
-- Doris Rollup 表（物化视图）详解和最佳实践
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. Rollup 概述
-- ============================================================

/*
Rollup 是 Doris 的物化视图实现：

**核心特性**：
- 自动聚合：创建后自动维护
- 透明路由：查询自动选择最优 Rollup
- 列式存储：只存储需要的列，节省空间

**与普通视图的区别**：
- 普通视图：逻辑视图，每次查询都执行 SQL
- Rollup：物理存储，预聚合结果

**适用场景**：
- 固定维度的聚合查询
- 报表查询加速
- 大宽表的列裁剪
*/


-- ============================================================
-- 2. 创建基础表
-- ============================================================

-- 销售明细表（大宽表）
CREATE TABLE IF NOT EXISTS sales_detail (
    sale_time DATETIME COMMENT '销售时间',
    order_id BIGINT COMMENT '订单ID',
    user_id BIGINT COMMENT '用户ID',
    product_id INT COMMENT '商品ID',
    store_id INT COMMENT '门店ID',
    category VARCHAR(100) COMMENT '类别',
    brand VARCHAR(100) COMMENT '品牌',
    city VARCHAR(50) COMMENT '城市',
    province VARCHAR(50) COMMENT '省份',
    amount DECIMAL(10,2) COMMENT '销售金额',
    quantity INT COMMENT '数量',
    discount DECIMAL(5,2) COMMENT '折扣',
    cost DECIMAL(10,2) COMMENT '成本'
)
DUPLICATE KEY(sale_time, order_id)
COMMENT '销售明细表（基础表）'
PARTITION BY RANGE(sale_time) (
    PARTITION p202501 VALUES LESS THAN ("2025-02-01 00:00:00"),
    PARTITION p202502 VALUES LESS THAN ("2025-03-01 00:00:00")
)
DISTRIBUTED BY HASH(order_id) BUCKETS 16;

-- 插入测试数据
INSERT INTO sales_detail VALUES
('2025-01-01 10:00:00', 1001, 101, 201, 1, 'Electronics', 'Apple', 'Beijing', 'Beijing', 5999.00, 1, 0.95, 4500.00),
('2025-01-01 11:00:00', 1002, 102, 202, 1, 'Electronics', 'Samsung', 'Beijing', 'Beijing', 3999.00, 1, 0.90, 3000.00),
('2025-01-01 12:00:00', 1003, 103, 203, 2, 'Clothing', 'Nike', 'Shanghai', 'Shanghai', 599.00, 2, 1.00, 300.00),
('2025-01-02 10:00:00', 1004, 104, 204, 1, 'Electronics', 'Huawei', 'Beijing', 'Beijing', 4999.00, 1, 0.85, 3800.00),
('2025-01-02 11:00:00', 1005, 105, 205, 3, 'Books', 'Unknown', 'Guangzhou', 'Guangdong', 79.99, 5, 1.00, 40.00),
('2025-01-02 12:00:00', 1006, 106, 206, 2, 'Clothing', 'Adidas', 'Shanghai', 'Shanghai', 899.00, 1, 0.88, 600.00),
('2025-01-03 10:00:00', 1007, 101, 207, 1, 'Electronics', 'Apple', 'Beijing', 'Beijing', 1999.00, 1, 1.00, 1500.00),
('2025-01-03 11:00:00', 1008, 102, 208, 4, 'Books', 'Unknown', 'Shenzhen', 'Guangdong', 59.99, 3, 1.00, 30.00);


-- ============================================================
-- 3. 创建 Rollup 表
-- ============================================================

-- Rollup 1: 按日期 + 类别聚合
ALTER TABLE sales_detail
ADD ROLLUP rollup_date_category (
    sale_time,
    category,
    amount,
    quantity,
    cost
);

-- Rollup 2: 按日期 + 城市聚合
ALTER TABLE sales_detail
ADD ROLLUP rollup_date_city (
    sale_time,
    city,
    province,
    amount,
    quantity
);

-- Rollup 3: 按日期 + 品牌聚合
ALTER TABLE sales_detail
ADD ROLLUP rollup_date_brand (
    sale_time,
    brand,
    category,
    amount,
    cost
);

-- Rollup 4: 按门店聚合
ALTER TABLE sales_detail
ADD ROLLUP rollup_store (
    store_id,
    amount,
    quantity,
    cost
);

-- 查看 Rollup 创建进度
SHOW ALTER TABLE ROLLUP FROM learning_db;

-- 等待 Rollup 创建完成（状态：FINISHED）
-- 创建时间取决于数据量（几秒到几分钟不等）


-- ============================================================
-- 4. 验证 Rollup 自动路由
-- ============================================================

-- 查询1：按类别统计（会路由到 rollup_date_category）
EXPLAIN
SELECT
    category,
    SUM(amount) as total_amount,
    SUM(quantity) as total_quantity,
    SUM(cost) as total_cost,
    SUM(amount) - SUM(cost) as profit
FROM sales_detail
GROUP BY category;

-- 实际执行查询
SELECT
    category,
    SUM(amount) as total_amount,
    SUM(quantity) as total_quantity,
    SUM(cost) as total_cost,
    SUM(amount) - SUM(cost) as profit
FROM sales_detail
GROUP BY category
ORDER BY total_amount DESC;

-- 查询2：按城市统计（会路由到 rollup_date_city）
EXPLAIN
SELECT
    city,
    province,
    SUM(amount) as total_amount,
    COUNT(*) as order_count
FROM sales_detail
GROUP BY city, province;

-- 查询3：按品牌统计（会路由到 rollup_date_brand）
SELECT
    brand,
    category,
    SUM(amount) as total_revenue,
    SUM(cost) as total_cost,
    SUM(amount) - SUM(cost) as profit,
    ROUND((SUM(amount) - SUM(cost)) * 100.0 / SUM(amount), 2) as profit_margin
FROM sales_detail
GROUP BY brand, category
ORDER BY profit DESC;

-- 查询4：按门店统计（会路由到 rollup_store）
SELECT
    store_id,
    SUM(amount) as store_revenue,
    SUM(quantity) as total_quantity,
    SUM(cost) as total_cost
FROM sales_detail
GROUP BY store_id
ORDER BY store_revenue DESC;


-- ============================================================
-- 5. Rollup 列顺序的重要性
-- ============================================================

/*
Rollup 列顺序遵循**前缀匹配**原则：

示例 Rollup 列顺序：(date, category, brand, amount, quantity)

✅ 可以命中 Rollup 的查询：
- GROUP BY date
- GROUP BY date, category
- GROUP BY date, category, brand
- WHERE date = '2025-01-01' GROUP BY category

❌ 无法命中 Rollup 的查询：
- GROUP BY category（缺少 date 前缀）
- GROUP BY brand（缺少 date, category 前缀）
- WHERE category = 'Electronics' GROUP BY date（WHERE 条件不匹配）

**设计建议**：
1. 高频过滤字段放在前面
2. 常用分组字段按使用频率排序
3. 聚合字段放在最后
*/


-- ============================================================
-- 6. Rollup 性能对比
-- ============================================================

-- 准备大数据量表（模拟真实场景）
CREATE TABLE IF NOT EXISTS sales_large (
    sale_time DATETIME,
    category VARCHAR(100),
    city VARCHAR(50),
    amount DECIMAL(10,2),
    quantity INT
)
DUPLICATE KEY(sale_time)
DISTRIBUTED BY HASH(sale_time) BUCKETS 32;

-- 创建 Rollup
ALTER TABLE sales_large
ADD ROLLUP rollup_category (
    category,
    amount,
    quantity
);

-- 对比查询性能
-- 无 Rollup：扫描全表
EXPLAIN
SELECT category, SUM(amount)
FROM sales_large
GROUP BY category;

-- 有 Rollup：只扫描 Rollup
-- 性能提升：10-100 倍


-- ============================================================
-- 7. Rollup 最佳实践
-- ============================================================

/*
Rollup 设计原则：

1. **分析高频查询**：
   - 找出最常用的 GROUP BY 组合
   - 统计查询频率和数据扫描量
   - 优先为慢查询创建 Rollup

2. **列顺序设计**：
   - 第一列：最常用的过滤字段（如日期）
   - 中间列：分组字段（按频率排序）
   - 最后列：聚合字段

3. **数量控制**：
   - 建议每个表 Rollup 数量 < 10 个
   - 过多 Rollup 会占用存储空间
   - 权衡查询性能和存储成本

4. **监控和维护**：
   - 定期查看 Rollup 命中率
   - 删除不再使用的 Rollup
   - 根据业务变化调整 Rollup
*/


-- ============================================================
-- 8. 查看和管理 Rollup
-- ============================================================

-- 查看表的所有 Rollup
SHOW ALTER TABLE ROLLUP FROM learning_db;

-- 查看表结构（包括 Rollup）
SHOW CREATE TABLE sales_detail\G

-- 查看 Rollup 的详细信息
DESC sales_detail ALL\G

-- 查看某个 Rollup 的列信息
DESC sales_detail INDEX rollup_date_category;


-- ============================================================
-- 9. 删除 Rollup
-- ============================================================

-- 如果 Rollup 不再需要，可以删除
-- ALTER TABLE sales_detail DROP ROLLUP rollup_date_category;
-- ALTER TABLE sales_detail DROP ROLLUP rollup_date_city;


-- ============================================================
-- 10. 实战案例：电商销售报表
-- ============================================================

-- 创建销售报表表
CREATE TABLE IF NOT EXISTS sales_report (
    report_date DATE COMMENT '报表日期',
    dimension_type VARCHAR(50) COMMENT '维度类型：category/city/brand/store',
    dimension_value VARCHAR(200) COMMENT '维度值',
    order_count BIGINT SUM COMMENT '订单数',
    total_amount DECIMAL(18,2) SUM COMMENT '销售额',
    total_quantity BIGINT SUM COMMENT '销售数量',
    total_cost DECIMAL(18,2) SUM COMMENT '成本',
    avg_discount DECIMAL(5,2) AVG COMMENT '平均折扣'
)
AGGREGATE KEY(report_date, dimension_type, dimension_value)
COMMENT '销售报表汇总表'
DISTRIBUTED BY HASH(dimension_value) BUCKETS 10;

-- 从明细表聚合导入报表表
INSERT INTO sales_report
SELECT
    DATE(sale_time) as report_date,
    'category' as dimension_type,
    category as dimension_value,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    SUM(quantity) as total_quantity,
    SUM(cost) as total_cost,
    AVG(discount) as avg_discount
FROM sales_detail
WHERE sale_time >= '2025-01-01'
GROUP BY DATE(sale_time), category

UNION ALL

SELECT
    DATE(sale_time) as report_date,
    'city' as dimension_type,
    city as dimension_value,
    COUNT(*) as order_count,
    SUM(amount) as total_amount,
    SUM(quantity) as total_quantity,
    SUM(cost) as total_cost,
    AVG(discount) as avg_discount
FROM sales_detail
WHERE sale_time >= '2025-01-01'
GROUP BY DATE(sale_time), city;

-- 查询报表数据
SELECT
    report_date,
    dimension_type,
    dimension_value,
    order_count,
    total_amount,
    total_quantity,
    total_amount - total_cost as profit,
    ROUND((total_amount - total_cost) * 100.0 / total_amount, 2) as profit_margin_pct
FROM sales_report
WHERE report_date = '2025-01-01'
ORDER BY dimension_type, total_amount DESC;


/*
Rollup 总结：

1. **核心优势**：
   - 自动聚合：创建后自动维护
   - 透明路由：查询自动选择最优 Rollup
   - 性能提升：10-100 倍加速（取决于数据量）

2. **设计原则**：
   - 列顺序：前缀匹配原则
   - 数量控制：< 10 个/表
   - 高频优先：为最常用查询创建 Rollup

3. **适用场景**：
   - 固定维度的聚合查询
   - 报表查询加速
   - 大宽表的列裁剪

4. **与 Aggregate 表对比**：
   - Rollup：依赖基础表，自动维护，透明路由
   - Aggregate 表：独立表，手动维护，显式查询
   - 推荐：Rollup 优先（更简单），复杂场景用 Aggregate 表

5. **注意事项**：
   - Rollup 创建是异步的，需要等待完成
   - 占用额外存储空间（约 20%-50%）
   - 写入性能略有下降（需要更新 Rollup）
   - 使用 EXPLAIN 验证 Rollup 是否生效
*/
