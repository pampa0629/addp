-- ============================================================
-- 01-basic/data_types.sql
-- Doris 数据类型完整示例和最佳实践
-- ============================================================

USE learning_db;

-- ============================================================
-- 1. 数值类型
-- ============================================================

CREATE TABLE IF NOT EXISTS numeric_types_demo (
    -- 整数类型
    tiny_col TINYINT COMMENT '1字节整数：-128 到 127',
    small_col SMALLINT COMMENT '2字节整数：-32768 到 32767',
    int_col INT COMMENT '4字节整数：-2^31 到 2^31-1',
    bigint_col BIGINT COMMENT '8字节整数：-2^63 到 2^63-1',
    largeint_col LARGEINT COMMENT '16字节整数：-2^127 到 2^127-1',

    -- 浮点类型
    float_col FLOAT COMMENT '单精度浮点：4字节',
    double_col DOUBLE COMMENT '双精度浮点：8字节',

    -- 精确数值类型（推荐用于金额）
    decimal_col DECIMAL(10, 2) COMMENT '定点数：最多10位，2位小数',
    money_col DECIMAL(18, 4) COMMENT '金额字段：18位数字，4位小数'
)
DUPLICATE KEY(tiny_col)
COMMENT '数值类型示例表'
DISTRIBUTED BY HASH(tiny_col) BUCKETS 4;

-- 插入示例数据
INSERT INTO numeric_types_demo VALUES
(100, 30000, 1000000, 9223372036854775807, 170141183460469231731687303715884105727,
 3.14, 2.718281828, 12345.67, 9999999999.9999);

-- 查询验证
SELECT * FROM numeric_types_demo;


-- ============================================================
-- 2. 字符串类型
-- ============================================================

CREATE TABLE IF NOT EXISTS string_types_demo (
    id INT COMMENT '主键ID',

    -- 固定长度字符串（不常用）
    char_col CHAR(10) COMMENT '固定10字符（自动填充空格）',

    -- 可变长度字符串（常用）
    varchar_short VARCHAR(50) COMMENT '短字符串：最多50字符',
    varchar_long VARCHAR(500) COMMENT '长字符串：最多500字符',

    -- 大文本类型
    string_col STRING COMMENT '不限长度字符串（最大2GB）',
    text_col TEXT COMMENT '大文本字段（最大2GB）'
)
DUPLICATE KEY(id)
COMMENT '字符串类型示例表'
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 插入示例数据
INSERT INTO string_types_demo VALUES
(1, 'FIXED', 'Short text', 'This is a longer string with more characters',
 'This is a very long string that can contain any text data including JSON, XML, or plain text',
 'Large text field for storing documents, logs, or descriptions');

-- 查询验证
SELECT id, varchar_short, LENGTH(string_col) as string_length FROM string_types_demo;


-- ============================================================
-- 3. 日期和时间类型
-- ============================================================

CREATE TABLE IF NOT EXISTS datetime_types_demo (
    id INT COMMENT '主键ID',

    -- 日期类型
    date_col DATE COMMENT '日期：YYYY-MM-DD (范围：0000-01-01 到 9999-12-31)',

    -- 日期时间类型（常用）
    datetime_col DATETIME COMMENT '日期时间：YYYY-MM-DD HH:MM:SS (精度到秒)',

    -- 日期时间类型（微秒精度）
    datetimev2_col DATETIMEV2(3) COMMENT '日期时间V2：精度到毫秒 (0-6位小数)',

    -- 时间类型（不常用）
    -- time_col TIME COMMENT '时间：HH:MM:SS',

    -- 日期函数示例
    current_date DATE DEFAULT CURRENT_DATE COMMENT '当前日期',
    current_time DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '当前时间'
)
DUPLICATE KEY(id)
COMMENT '日期时间类型示例表'
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 插入示例数据（使用日期函数）
INSERT INTO datetime_types_demo (id, date_col, datetime_col, datetimev2_col) VALUES
(1, '2025-01-01', '2025-01-01 10:30:00', '2025-01-01 10:30:00.123'),
(2, CURRENT_DATE(), NOW(), NOW()),
(3, DATE_ADD(CURRENT_DATE(), INTERVAL 7 DAY), DATE_ADD(NOW(), INTERVAL 1 HOUR), NOW());

-- 查询验证（日期计算）
SELECT
    id,
    date_col,
    datetime_col,
    DATEDIFF(CURRENT_DATE(), date_col) as days_diff,
    DATE_FORMAT(datetime_col, '%Y年%m月%d日 %H:%i:%s') as formatted_time
FROM datetime_types_demo;


-- ============================================================
-- 4. 布尔类型
-- ============================================================

CREATE TABLE IF NOT EXISTS boolean_demo (
    id INT COMMENT '主键ID',
    is_active BOOLEAN COMMENT '是否激活（TRUE/FALSE）',
    is_deleted BOOLEAN DEFAULT FALSE COMMENT '是否删除（默认FALSE）',
    status VARCHAR(20) COMMENT '状态描述'
)
DUPLICATE KEY(id)
COMMENT '布尔类型示例表'
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 插入示例数据
INSERT INTO boolean_demo VALUES
(1, TRUE, FALSE, 'active'),
(2, FALSE, FALSE, 'inactive'),
(3, TRUE, TRUE, 'deleted');

-- 查询验证（布尔条件筛选）
SELECT * FROM boolean_demo WHERE is_active = TRUE AND is_deleted = FALSE;


-- ============================================================
-- 5. JSON 类型（重要）
-- ============================================================

CREATE TABLE IF NOT EXISTS json_demo (
    id INT COMMENT '主键ID',
    user_info JSON COMMENT '用户信息JSON',
    metadata JSON COMMENT '元数据JSON',
    create_time DATETIME DEFAULT CURRENT_TIMESTAMP
)
DUPLICATE KEY(id)
COMMENT 'JSON类型示例表'
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 插入 JSON 数据
INSERT INTO json_demo (id, user_info, metadata) VALUES
(1, '{"name": "Alice", "age": 25, "city": "Beijing", "tags": ["developer", "python"]}',
    '{"source": "web", "ip": "192.168.1.1", "device": "Chrome"}'),
(2, '{"name": "Bob", "age": 30, "city": "Shanghai", "tags": ["manager", "product"]}',
    '{"source": "mobile", "ip": "192.168.1.2", "device": "iOS"}');

-- 查询 JSON 字段（使用 JSON 函数）
SELECT
    id,
    JSON_EXTRACT(user_info, '$.name') as user_name,
    JSON_EXTRACT(user_info, '$.age') as age,
    JSON_EXTRACT(user_info, '$.city') as city,
    JSON_EXTRACT(metadata, '$.source') as source
FROM json_demo;

-- 筛选 JSON 数据
SELECT * FROM json_demo
WHERE JSON_EXTRACT(user_info, '$.age') > 25;


-- ============================================================
-- 6. 数组类型
-- ============================================================

CREATE TABLE IF NOT EXISTS array_demo (
    id INT COMMENT '主键ID',
    tags ARRAY<VARCHAR(50)> COMMENT '标签数组',
    scores ARRAY<INT> COMMENT '分数数组',
    create_time DATETIME DEFAULT CURRENT_TIMESTAMP
)
DUPLICATE KEY(id)
COMMENT '数组类型示例表'
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 插入数组数据
INSERT INTO array_demo (id, tags, scores) VALUES
(1, ['python', 'java', 'go'], [85, 90, 88]),
(2, ['javascript', 'typescript'], [92, 95]),
(3, ['c++', 'rust', 'assembly'], [78, 82, 75]);

-- 查询数组数据
SELECT
    id,
    tags,
    scores,
    ARRAY_SIZE(tags) as tag_count,
    ARRAY_SIZE(scores) as score_count
FROM array_demo;

-- 数组元素访问（索引从1开始）
SELECT
    id,
    tags[1] as first_tag,
    scores[1] as first_score,
    ARRAY_AVG(scores) as avg_score
FROM array_demo;


-- ============================================================
-- 7. HLL 类型（精确去重）
-- ============================================================

CREATE TABLE IF NOT EXISTS hll_demo (
    date DATE COMMENT '日期',
    page_url VARCHAR(200) COMMENT '页面URL',
    user_id_hll HLL HLL_UNION COMMENT 'HLL去重计数（UV）'
)
AGGREGATE KEY(date, page_url)
COMMENT 'HLL去重示例表'
DISTRIBUTED BY HASH(page_url) BUCKETS 4;

-- 插入数据（使用 HLL_HASH 函数）
INSERT INTO hll_demo VALUES
('2025-01-01', '/home', HLL_HASH(1001)),
('2025-01-01', '/home', HLL_HASH(1002)),
('2025-01-01', '/home', HLL_HASH(1001)),  -- 重复用户会自动去重
('2025-01-01', '/product', HLL_HASH(1003));

-- 查询 UV（自动去重）
SELECT
    date,
    page_url,
    HLL_UNION_AGG(user_id_hll) as unique_users
FROM hll_demo
GROUP BY date, page_url;


-- ============================================================
-- 8. BITMAP 类型（精确去重，更高效）
-- ============================================================

CREATE TABLE IF NOT EXISTS bitmap_demo (
    date DATE COMMENT '日期',
    page_url VARCHAR(200) COMMENT '页面URL',
    user_bitmap BITMAP BITMAP_UNION COMMENT 'Bitmap去重（UV）'
)
AGGREGATE KEY(date, page_url)
COMMENT 'Bitmap去重示例表'
DISTRIBUTED BY HASH(page_url) BUCKETS 4;

-- 插入数据（使用 TO_BITMAP 函数）
INSERT INTO bitmap_demo VALUES
('2025-01-01', '/home', TO_BITMAP(1001)),
('2025-01-01', '/home', TO_BITMAP(1002)),
('2025-01-01', '/home', TO_BITMAP(1001)),  -- 重复用户会自动去重
('2025-01-01', '/product', TO_BITMAP(1003)),
('2025-01-01', '/product', TO_BITMAP(1004));

-- 查询 UV（Bitmap 自动去重）
SELECT
    date,
    page_url,
    BITMAP_COUNT(BITMAP_UNION(user_bitmap)) as unique_users
FROM bitmap_demo
GROUP BY date, page_url;

-- Bitmap 运算（交集、并集、差集）
-- 找出同时访问 /home 和 /product 的用户数
SELECT
    BITMAP_COUNT(
        BITMAP_INTERSECT(
            (SELECT BITMAP_UNION(user_bitmap) FROM bitmap_demo WHERE page_url = '/home'),
            (SELECT BITMAP_UNION(user_bitmap) FROM bitmap_demo WHERE page_url = '/product')
        )
    ) as common_users;


-- ============================================================
-- 9. 数据类型选择最佳实践
-- ============================================================

CREATE TABLE IF NOT EXISTS best_practice_demo (
    -- 主键：使用 BIGINT（支持更大范围）
    id BIGINT COMMENT '主键ID',

    -- 用户名：VARCHAR(100) 足够
    username VARCHAR(100) COMMENT '用户名',

    -- 邮箱：VARCHAR(255)
    email VARCHAR(255) COMMENT '邮箱',

    -- 年龄：TINYINT（0-255）
    age TINYINT COMMENT '年龄',

    -- 金额：DECIMAL(18, 4) 精确到分
    balance DECIMAL(18, 4) COMMENT '账户余额',

    -- 状态：VARCHAR(20) 或枚举
    status VARCHAR(20) COMMENT '状态：active, inactive, deleted',

    -- 是否VIP：BOOLEAN
    is_vip BOOLEAN DEFAULT FALSE COMMENT '是否VIP用户',

    -- 扩展信息：JSON（灵活存储）
    extra_info JSON COMMENT '扩展信息（JSON）',

    -- 创建时间：DATETIME（常用）
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '创建时间',

    -- 更新时间：DATETIME
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP COMMENT '更新时间'
)
DUPLICATE KEY(id)
COMMENT '数据类型最佳实践示例'
DISTRIBUTED BY HASH(id) BUCKETS 10;

-- 插入示例数据
INSERT INTO best_practice_demo (id, username, email, age, balance, status, is_vip, extra_info) VALUES
(1, 'alice', 'alice@example.com', 25, 10000.50, 'active', TRUE,
 '{"level": 5, "points": 1500, "tags": ["premium", "loyal"]}'),
(2, 'bob', 'bob@example.com', 30, 5000.00, 'active', FALSE,
 '{"level": 3, "points": 800, "tags": ["regular"]}');

-- 查询验证
SELECT
    id, username, age, balance, status, is_vip,
    JSON_EXTRACT(extra_info, '$.level') as user_level,
    created_at
FROM best_practice_demo;


/*
数据类型选择建议：

1. **整数类型**：
   - ID 字段：BIGINT（推荐）
   - 计数器：INT 或 BIGINT
   - 枚举值（0-255）：TINYINT

2. **字符串类型**：
   - 短字符串（用户名、邮箱）：VARCHAR(50-255)
   - 长文本（描述、备注）：TEXT 或 STRING
   - 固定长度：CHAR（不推荐，浪费空间）

3. **数值类型（金额）**：
   - 金额：DECIMAL(18, 4)（精确到分）
   - 百分比：DECIMAL(5, 2)（如 99.99%）
   - 避免使用 FLOAT/DOUBLE 存储金额（精度问题）

4. **日期时间**：
   - 日期：DATE（如 2025-01-01）
   - 时间戳：DATETIME（推荐）
   - 高精度：DATETIMEV2(3)（毫秒）

5. **布尔类型**：
   - 是/否字段：BOOLEAN
   - 多状态：VARCHAR(20)（如 'active', 'inactive', 'deleted'）

6. **复杂类型**：
   - JSON：灵活存储扩展信息
   - ARRAY：存储列表数据
   - BITMAP：高效去重计数（UV统计）

7. **性能优化**：
   - 选择合适的字段长度（避免浪费）
   - 金额字段用 DECIMAL，不用 FLOAT
   - 高基数字段（ID、用户名）用作分桶键
   - 时间字段用作分区键
*/
