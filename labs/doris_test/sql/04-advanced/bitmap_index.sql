-- ============================================================
-- 04-advanced/bitmap_index.sql
-- Doris Bitmap 索引和精确去重实战
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. Bitmap 概述
-- ============================================================

/*
Bitmap（位图）是 Doris 的高级特性：

**核心优势**：
- 精确去重：COUNT DISTINCT 的高性能替代
- 空间高效：使用位图压缩存储用户ID
- 快速运算：支持交集、并集、差集等位运算

**适用场景**：
- UV（Unique Visitor）统计
- 用户行为分析（留存、漏斗）
- 标签人群圈选
- 精确去重计数

**与 HLL 对比**：
- Bitmap：精确去重，内存占用适中
- HLL：近似去重，误差约 1%，内存占用极小
*/


-- ============================================================
-- 2. 创建 Bitmap 表（Aggregate 模型）
-- ============================================================

-- 用户访问统计表（使用 Bitmap 存储用户ID）
CREATE TABLE IF NOT EXISTS page_visit_stats (
    visit_date DATE COMMENT '访问日期',
    page_url VARCHAR(200) COMMENT '页面URL',
    city VARCHAR(50) COMMENT '城市',
    pv BIGINT SUM COMMENT 'PV（页面浏览量）',
    user_bitmap BITMAP BITMAP_UNION COMMENT 'UV Bitmap（用户去重）'
)
AGGREGATE KEY(visit_date, page_url, city)
COMMENT '页面访问统计表（Bitmap 去重）'
DISTRIBUTED BY HASH(page_url) BUCKETS 10;

-- 插入数据（使用 TO_BITMAP 函数）
INSERT INTO page_visit_stats VALUES
('2025-01-01', '/home', 'Beijing', 100, TO_BITMAP(1001)),
('2025-01-01', '/home', 'Beijing', 50, TO_BITMAP(1002)),
('2025-01-01', '/home', 'Beijing', 30, TO_BITMAP(1001)),  -- 重复用户，会自动去重
('2025-01-01', '/home', 'Shanghai', 80, TO_BITMAP(2001)),
('2025-01-01', '/product/1', 'Beijing', 60, TO_BITMAP(1001)),
('2025-01-01', '/product/1', 'Beijing', 40, TO_BITMAP(1003)),
('2025-01-02', '/home', 'Beijing', 120, TO_BITMAP(1001)),
('2025-01-02', '/home', 'Beijing', 90, TO_BITMAP(1004)),
('2025-01-02', '/product/1', 'Shanghai', 70, TO_BITMAP(2002));

-- 查询 UV（自动去重）
SELECT
    visit_date,
    page_url,
    city,
    pv,
    BITMAP_COUNT(BITMAP_UNION(user_bitmap)) as uv  -- 精确去重计数
FROM page_visit_stats
GROUP BY visit_date, page_url, city
ORDER BY visit_date, pv DESC;


-- ============================================================
-- 3. Bitmap 基础函数
-- ============================================================

-- TO_BITMAP：将整数转换为 Bitmap
SELECT TO_BITMAP(1001) as bitmap_value;

-- BITMAP_UNION：合并多个 Bitmap（并集）
SELECT
    page_url,
    BITMAP_COUNT(BITMAP_UNION(user_bitmap)) as total_uv
FROM page_visit_stats
WHERE visit_date = '2025-01-01'
GROUP BY page_url;

-- BITMAP_COUNT：计算 Bitmap 中的元素数量
SELECT
    visit_date,
    BITMAP_COUNT(BITMAP_UNION(user_bitmap)) as daily_uv
FROM page_visit_stats
GROUP BY visit_date
ORDER BY visit_date;

-- BITMAP_INTERSECT：交集（找出共同用户）
SELECT
    BITMAP_COUNT(
        BITMAP_INTERSECT(user_bitmap)
    ) as common_users
FROM page_visit_stats
WHERE visit_date = '2025-01-01'
  AND page_url IN ('/home', '/product/1')
  AND city = 'Beijing';


-- ============================================================
-- 4. Bitmap 高级运算
-- ============================================================

-- 案例1：找出访问了 /home 但未访问 /product/1 的用户数
SELECT
    BITMAP_COUNT(
        BITMAP_ANDNOT(
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/home'),
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/product/1')
        )
    ) as exclusive_users;

-- 案例2：找出同时访问 /home 和 /product/1 的用户数（交集）
SELECT
    BITMAP_COUNT(
        BITMAP_AND(
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/home'),
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/product/1')
        )
    ) as both_visited_users;

-- 案例3：找出访问 /home 或 /product/1 的总用户数（并集）
SELECT
    BITMAP_COUNT(
        BITMAP_OR(
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/home'),
            (SELECT BITMAP_UNION(user_bitmap) FROM page_visit_stats
             WHERE visit_date = '2025-01-01' AND page_url = '/product/1')
        )
    ) as total_unique_users;


-- ============================================================
-- 5. 用户留存分析（Bitmap 实战）
-- ============================================================

-- 创建用户日活表
CREATE TABLE IF NOT EXISTS user_daily_active (
    active_date DATE COMMENT '活跃日期',
    user_bitmap BITMAP BITMAP_UNION COMMENT '活跃用户 Bitmap'
)
AGGREGATE KEY(active_date)
COMMENT '用户日活表'
DISTRIBUTED BY HASH(active_date) BUCKETS 10;

-- 插入测试数据
INSERT INTO user_daily_active VALUES
('2025-01-01', TO_BITMAP(1001)),
('2025-01-01', TO_BITMAP(1002)),
('2025-01-01', TO_BITMAP(1003)),
('2025-01-01', TO_BITMAP(1004)),
('2025-01-02', TO_BITMAP(1001)),  -- 1001 次日留存
('2025-01-02', TO_BITMAP(1002)),  -- 1002 次日留存
('2025-01-02', TO_BITMAP(1005)),
('2025-01-03', TO_BITMAP(1001)),  -- 1001 连续活跃
('2025-01-03', TO_BITMAP(1003)),  -- 1003 回流
('2025-01-03', TO_BITMAP(1006));

-- 计算次日留存率
SELECT
    d1.active_date as base_date,
    BITMAP_COUNT(BITMAP_UNION(d1.user_bitmap)) as day1_users,
    BITMAP_COUNT(
        BITMAP_AND(
            BITMAP_UNION(d1.user_bitmap),
            BITMAP_UNION(d2.user_bitmap)
        )
    ) as retained_users,
    ROUND(
        BITMAP_COUNT(
            BITMAP_AND(
                BITMAP_UNION(d1.user_bitmap),
                BITMAP_UNION(d2.user_bitmap)
            )
        ) * 100.0 / BITMAP_COUNT(BITMAP_UNION(d1.user_bitmap)),
        2
    ) as retention_rate_pct
FROM user_daily_active d1
LEFT JOIN user_daily_active d2
    ON d2.active_date = DATE_ADD(d1.active_date, INTERVAL 1 DAY)
GROUP BY d1.active_date
ORDER BY d1.active_date;


-- ============================================================
-- 6. 漏斗分析（Bitmap 实战）
-- ============================================================

-- 创建漏斗事件表
CREATE TABLE IF NOT EXISTS funnel_events (
    event_date DATE COMMENT '事件日期',
    event_type VARCHAR(50) COMMENT '事件类型：view, cart, checkout, purchase',
    user_bitmap BITMAP BITMAP_UNION COMMENT '用户 Bitmap'
)
AGGREGATE KEY(event_date, event_type)
COMMENT '漏斗事件表'
DISTRIBUTED BY HASH(event_type) BUCKETS 10;

-- 插入漏斗数据
INSERT INTO funnel_events VALUES
-- 2025-01-01
('2025-01-01', 'view', TO_BITMAP(1001)),
('2025-01-01', 'view', TO_BITMAP(1002)),
('2025-01-01', 'view', TO_BITMAP(1003)),
('2025-01-01', 'view', TO_BITMAP(1004)),
('2025-01-01', 'view', TO_BITMAP(1005)),
('2025-01-01', 'cart', TO_BITMAP(1001)),
('2025-01-01', 'cart', TO_BITMAP(1002)),
('2025-01-01', 'cart', TO_BITMAP(1003)),
('2025-01-01', 'checkout', TO_BITMAP(1001)),
('2025-01-01', 'checkout', TO_BITMAP(1002)),
('2025-01-01', 'purchase', TO_BITMAP(1001));

-- 计算转化漏斗
SELECT
    'view' as step,
    BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'view' THEN user_bitmap END)) as user_count,
    100.0 as conversion_rate
FROM funnel_events
WHERE event_date = '2025-01-01'

UNION ALL

SELECT
    'cart' as step,
    BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'cart' THEN user_bitmap END)) as user_count,
    ROUND(
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'cart' THEN user_bitmap END)) * 100.0 /
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'view' THEN user_bitmap END)),
        2
    ) as conversion_rate
FROM funnel_events
WHERE event_date = '2025-01-01'

UNION ALL

SELECT
    'checkout' as step,
    BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'checkout' THEN user_bitmap END)) as user_count,
    ROUND(
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'checkout' THEN user_bitmap END)) * 100.0 /
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'view' THEN user_bitmap END)),
        2
    ) as conversion_rate
FROM funnel_events
WHERE event_date = '2025-01-01'

UNION ALL

SELECT
    'purchase' as step,
    BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'purchase' THEN user_bitmap END)) as user_count,
    ROUND(
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'purchase' THEN user_bitmap END)) * 100.0 /
        BITMAP_COUNT(BITMAP_UNION(CASE WHEN event_type = 'view' THEN user_bitmap END)),
        2
    ) as conversion_rate
FROM funnel_events
WHERE event_date = '2025-01-01';


-- ============================================================
-- 7. 用户分群（Bitmap 实战）
-- ============================================================

-- 创建用户标签表
CREATE TABLE IF NOT EXISTS user_tags (
    tag_date DATE COMMENT '标签日期',
    tag_name VARCHAR(100) COMMENT '标签名称',
    user_bitmap BITMAP BITMAP_UNION COMMENT '用户 Bitmap'
)
AGGREGATE KEY(tag_date, tag_name)
COMMENT '用户标签表'
DISTRIBUTED BY HASH(tag_name) BUCKETS 10;

-- 插入标签数据
INSERT INTO user_tags VALUES
('2025-01-01', 'VIP用户', TO_BITMAP(1001)),
('2025-01-01', 'VIP用户', TO_BITMAP(1002)),
('2025-01-01', 'VIP用户', TO_BITMAP(1003)),
('2025-01-01', '活跃用户', TO_BITMAP(1001)),
('2025-01-01', '活跃用户', TO_BITMAP(1002)),
('2025-01-01', '活跃用户', TO_BITMAP(1004)),
('2025-01-01', '活跃用户', TO_BITMAP(1005)),
('2025-01-01', '购买用户', TO_BITMAP(1001)),
('2025-01-01', '购买用户', TO_BITMAP(1002));

-- 查询：VIP 且活跃的用户数（交集）
SELECT
    BITMAP_COUNT(
        BITMAP_AND(
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = 'VIP用户'),
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = '活跃用户')
        )
    ) as vip_active_users;

-- 查询：VIP 或购买用户数（并集）
SELECT
    BITMAP_COUNT(
        BITMAP_OR(
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = 'VIP用户'),
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = '购买用户')
        )
    ) as vip_or_buyer_users;

-- 查询：活跃但未购买的用户数（差集）
SELECT
    BITMAP_COUNT(
        BITMAP_ANDNOT(
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = '活跃用户'),
            (SELECT BITMAP_UNION(user_bitmap) FROM user_tags WHERE tag_name = '购买用户')
        )
    ) as active_not_buyer_users;


-- ============================================================
-- 8. Bitmap 性能对比
-- ============================================================

-- 创建对比测试表（不使用 Bitmap）
CREATE TABLE IF NOT EXISTS page_visit_no_bitmap (
    visit_date DATE,
    page_url VARCHAR(200),
    user_id BIGINT
)
DUPLICATE KEY(visit_date, page_url, user_id)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- 插入测试数据
INSERT INTO page_visit_no_bitmap VALUES
('2025-01-01', '/home', 1001),
('2025-01-01', '/home', 1002),
('2025-01-01', '/home', 1001),  -- 重复
('2025-01-01', '/home', 1003),
('2025-01-01', '/product/1', 1001),
('2025-01-01', '/product/1', 1004);

-- 方式1：COUNT DISTINCT（传统方式，性能差）
SELECT
    visit_date,
    page_url,
    COUNT(DISTINCT user_id) as uv
FROM page_visit_no_bitmap
WHERE visit_date = '2025-01-01'
GROUP BY visit_date, page_url;

-- 方式2：Bitmap（高性能）
SELECT
    visit_date,
    page_url,
    BITMAP_COUNT(BITMAP_UNION(user_bitmap)) as uv
FROM page_visit_stats
WHERE visit_date = '2025-01-01'
GROUP BY visit_date, page_url;

/*
性能对比：
- COUNT DISTINCT：需要扫描所有数据并去重，O(n)
- Bitmap：预聚合，O(1) 查询
- 性能提升：10-100 倍（数据量越大提升越明显）
*/


/*
Bitmap 使用总结：

1. **核心优势**：
   - 精确去重：替代 COUNT DISTINCT，性能提升 10-100 倍
   - 位运算：支持交集、并集、差集
   - 空间高效：压缩存储，节省内存

2. **常用函数**：
   - TO_BITMAP(id)：转换为 Bitmap
   - BITMAP_UNION：并集（合并）
   - BITMAP_AND：交集（共同）
   - BITMAP_ANDNOT：差集（A 有 B 无）
   - BITMAP_OR：并集（等同 BITMAP_UNION）
   - BITMAP_COUNT：计数

3. **典型应用场景**：
   - UV 统计：页面去重访问用户数
   - 留存分析：次日/7日留存率
   - 漏斗分析：转化率计算
   - 用户分群：标签圈选

4. **最佳实践**：
   - 使用 Aggregate 表模型（BITMAP BITMAP_UNION）
   - 预聚合存储 Bitmap，查询时只需 BITMAP_COUNT
   - 适合大规模用户行为分析

5. **与其他方案对比**：
   - COUNT DISTINCT：精确但慢（全表扫描）
   - HLL：近似快速（误差 1%）
   - Bitmap：精确且快（推荐）
*/
