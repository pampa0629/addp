-- ============================================================
-- 05-integration/meta_scan_test.sql
-- ADDP Meta 模块 Doris 元数据扫描测试和验证
-- ============================================================

USE addp_test_db;

-- ============================================================
-- 1. 元数据扫描测试数据准备
-- ============================================================

-- 创建不同类型的表用于测试 Meta 扫描

-- 测试表1：包含所有常用数据类型
CREATE TABLE IF NOT EXISTS datatype_test (
    -- 整数类型
    tinyint_col TINYINT COMMENT 'TINYINT 类型',
    smallint_col SMALLINT COMMENT 'SMALLINT 类型',
    int_col INT COMMENT 'INT 类型',
    bigint_col BIGINT COMMENT 'BIGINT 类型',

    -- 浮点类型
    float_col FLOAT COMMENT 'FLOAT 类型',
    double_col DOUBLE COMMENT 'DOUBLE 类型',
    decimal_col DECIMAL(18,4) COMMENT 'DECIMAL 类型',

    -- 字符串类型
    char_col CHAR(10) COMMENT 'CHAR 类型',
    varchar_col VARCHAR(255) COMMENT 'VARCHAR 类型',
    string_col STRING COMMENT 'STRING 类型',
    text_col TEXT COMMENT 'TEXT 类型',

    -- 日期时间类型
    date_col DATE COMMENT 'DATE 类型',
    datetime_col DATETIME COMMENT 'DATETIME 类型',

    -- 布尔类型
    boolean_col BOOLEAN COMMENT 'BOOLEAN 类型',

    -- JSON 类型
    json_col JSON COMMENT 'JSON 类型',

    -- 数组类型
    array_col ARRAY<VARCHAR(50)> COMMENT 'ARRAY 类型',

    -- Bitmap 类型
    bitmap_col BITMAP BITMAP_UNION COMMENT 'BITMAP 类型'
)
AGGREGATE KEY(tinyint_col, smallint_col, int_col, bigint_col)
COMMENT 'Meta 扫描数据类型测试表'
DISTRIBUTED BY HASH(int_col) BUCKETS 4;

-- 测试表2：包含中文字段名和注释
CREATE TABLE IF NOT EXISTS chinese_fields_test (
    用户ID BIGINT COMMENT '用户唯一标识',
    用户名 VARCHAR(100) COMMENT '用户登录名',
    手机号 VARCHAR(20) COMMENT '联系电话',
    注册时间 DATETIME COMMENT '账户创建时间',
    最后登录 DATETIME COMMENT '最近一次登录时间'
)
UNIQUE KEY(用户ID)
COMMENT 'Meta 扫描中文字段测试表'
DISTRIBUTED BY HASH(用户ID) BUCKETS 4;

-- 测试表3：无注释的表（测试 Meta 对空注释的处理）
CREATE TABLE IF NOT EXISTS no_comment_test (
    id BIGINT,
    name VARCHAR(100),
    value INT
)
DUPLICATE KEY(id)
DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 测试表4：Primary Key 表
CREATE TABLE IF NOT EXISTS primary_key_test (
    pk_id BIGINT COMMENT '主键ID',
    data VARCHAR(200) COMMENT '数据',
    update_time DATETIME COMMENT '更新时间'
)
PRIMARY KEY(pk_id)
COMMENT 'Meta 扫描 Primary Key 表测试'
DISTRIBUTED BY HASH(pk_id) BUCKETS 4;


-- ============================================================
-- 2. 插入测试数据
-- ============================================================

-- 插入数据类型测试数据
INSERT INTO datatype_test VALUES
(127, 32767, 2147483647, 9223372036854775807,
 3.14, 2.718281828, 12345.6789,
 'CHAR', 'VARCHAR test', 'STRING test', 'TEXT test',
 '2025-01-01', '2025-01-01 12:00:00',
 TRUE,
 '{"key": "value", "number": 123}',
 ['tag1', 'tag2', 'tag3'],
 TO_BITMAP(1001));

-- 插入中文字段测试数据
INSERT INTO chinese_fields_test VALUES
(1001, '张三', '13800001001', '2024-01-15 10:00:00', '2025-01-10 14:30:00'),
(1002, '李四', '13800001002', '2024-02-20 11:00:00', '2025-01-09 09:15:00');

-- 插入无注释表数据
INSERT INTO no_comment_test VALUES
(1, 'test1', 100),
(2, 'test2', 200);

-- 插入 Primary Key 表数据
INSERT INTO primary_key_test VALUES
(1, 'data1', NOW()),
(2, 'data2', NOW());


-- ============================================================
-- 3. Meta 扫描验证 SQL（模拟 Meta 模块的扫描逻辑）
-- ============================================================

-- 3.1 扫描数据库列表
SELECT SCHEMA_NAME as database_name
FROM information_schema.SCHEMATA
WHERE SCHEMA_NAME NOT IN ('information_schema', '__internal_schema')
ORDER BY SCHEMA_NAME;

-- 3.2 扫描指定数据库的表列表
SELECT
    TABLE_SCHEMA as database_name,
    TABLE_NAME as table_name,
    TABLE_TYPE as table_type,
    ENGINE as table_engine,
    TABLE_ROWS as row_count,
    TABLE_COMMENT as table_comment
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db'
ORDER BY TABLE_NAME;

-- 3.3 扫描指定表的字段信息
SELECT
    TABLE_SCHEMA as database_name,
    TABLE_NAME as table_name,
    COLUMN_NAME as column_name,
    ORDINAL_POSITION as column_position,
    DATA_TYPE as data_type,
    COLUMN_TYPE as column_full_type,
    IS_NULLABLE as is_nullable,
    COLUMN_KEY as column_key,
    COLUMN_DEFAULT as default_value,
    COLUMN_COMMENT as column_comment
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db'
  AND TABLE_NAME = 'datatype_test'
ORDER BY ORDINAL_POSITION;

-- 3.4 扫描所有表的所有字段（Meta 完整扫描）
SELECT
    TABLE_SCHEMA as database_name,
    TABLE_NAME as table_name,
    COLUMN_NAME as column_name,
    ORDINAL_POSITION as column_position,
    DATA_TYPE as data_type,
    COLUMN_TYPE as column_full_type,
    IS_NULLABLE as is_nullable,
    COLUMN_KEY as column_key,
    COLUMN_DEFAULT as default_value,
    COLUMN_COMMENT as column_comment
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db'
ORDER BY TABLE_NAME, ORDINAL_POSITION;


-- ============================================================
-- 4. Doris 特有元数据扫描（可选扩展）
-- ============================================================

-- 4.1 获取表的分区信息
SHOW PARTITIONS FROM addp_test_db.orders;

-- 4.2 获取表的 Rollup 信息
SHOW ALTER TABLE ROLLUP FROM addp_test_db;

-- 4.3 获取表的索引信息
SHOW INDEX FROM addp_test_db.users;

-- 4.4 获取表的详细创建语句
SHOW CREATE TABLE addp_test_db.users\G

-- 4.5 获取表的统计信息
SELECT
    TABLE_SCHEMA,
    TABLE_NAME,
    TABLE_ROWS,
    AVG_ROW_LENGTH,
    DATA_LENGTH,
    INDEX_LENGTH,
    CREATE_TIME,
    UPDATE_TIME
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db'
ORDER BY DATA_LENGTH DESC;


-- ============================================================
-- 5. Meta 扫描结果验证
-- ============================================================

-- 验证1：检查所有表是否都被扫描
SELECT
    'Expected Tables' as check_type,
    COUNT(*) as count
FROM (
    SELECT 'users' as table_name UNION ALL
    SELECT 'orders' UNION ALL
    SELECT 'user_activity_log' UNION ALL
    SELECT 'user_statistics' UNION ALL
    SELECT 'datatype_test' UNION ALL
    SELECT 'chinese_fields_test' UNION ALL
    SELECT 'no_comment_test' UNION ALL
    SELECT 'primary_key_test'
) expected

UNION ALL

SELECT
    'Actual Tables' as check_type,
    COUNT(*) as count
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db';

-- 验证2：检查字段类型映射是否正确
SELECT
    COLUMN_NAME,
    DATA_TYPE,
    COLUMN_TYPE,
    CASE
        WHEN DATA_TYPE IN ('TINYINT', 'SMALLINT', 'INT', 'BIGINT') THEN 'Integer'
        WHEN DATA_TYPE IN ('FLOAT', 'DOUBLE', 'DECIMAL') THEN 'Numeric'
        WHEN DATA_TYPE IN ('CHAR', 'VARCHAR', 'STRING', 'TEXT') THEN 'String'
        WHEN DATA_TYPE IN ('DATE', 'DATETIME') THEN 'DateTime'
        WHEN DATA_TYPE = 'BOOLEAN' THEN 'Boolean'
        WHEN DATA_TYPE = 'JSON' THEN 'JSON'
        WHEN DATA_TYPE LIKE 'ARRAY%' THEN 'Array'
        WHEN DATA_TYPE = 'BITMAP' THEN 'Bitmap'
        ELSE 'Unknown'
    END as mapped_type
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db'
  AND TABLE_NAME = 'datatype_test'
ORDER BY ORDINAL_POSITION;

-- 验证3：检查注释是否正确扫描
SELECT
    TABLE_NAME,
    COLUMN_NAME,
    COLUMN_COMMENT,
    CASE
        WHEN COLUMN_COMMENT IS NULL OR COLUMN_COMMENT = '' THEN 'No Comment'
        ELSE 'Has Comment'
    END as comment_status
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db'
ORDER BY TABLE_NAME, ORDINAL_POSITION;


-- ============================================================
-- 6. Meta 扫描性能测试
-- ============================================================

-- 测试1：扫描大量表的性能
-- 创建多个测试表
CREATE TABLE IF NOT EXISTS perf_test_1 (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 4;
CREATE TABLE IF NOT EXISTS perf_test_2 (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 4;
CREATE TABLE IF NOT EXISTS perf_test_3 (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 4;
CREATE TABLE IF NOT EXISTS perf_test_4 (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 4;
CREATE TABLE IF NOT EXISTS perf_test_5 (id BIGINT) DUPLICATE KEY(id) DISTRIBUTED BY HASH(id) BUCKETS 4;

-- 测试扫描所有表的耗时
SELECT
    COUNT(*) as table_count,
    NOW() as scan_time
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db';

-- 测试扫描所有字段的耗时
SELECT
    COUNT(*) as column_count,
    NOW() as scan_time
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db';


-- ============================================================
-- 7. Meta 模块 API 测试数据
-- ============================================================

/*
Meta 模块扫描完成后，可以通过 API 验证：

**API 1：获取数据库列表**
GET http://localhost:8082/api/meta/databases?resource_id=1

预期返回：
{
  "code": 0,
  "data": [
    {
      "database_name": "addp_test_db",
      "table_count": 12,
      "last_scan_time": "2025-01-10 10:00:00"
    }
  ]
}

**API 2：获取表列表**
GET http://localhost:8082/api/meta/tables?resource_id=1&database=addp_test_db

预期返回：
{
  "code": 0,
  "data": [
    {
      "table_name": "users",
      "table_type": "UNIQUE",
      "row_count": 5,
      "table_comment": "ADDP 用户表"
    },
    {
      "table_name": "orders",
      "table_type": "DUPLICATE",
      "row_count": 7,
      "table_comment": "ADDP 订单表"
    }
  ]
}

**API 3：获取字段列表**
GET http://localhost:8082/api/meta/columns?resource_id=1&database=addp_test_db&table=users

预期返回：
{
  "code": 0,
  "data": [
    {
      "column_name": "user_id",
      "data_type": "BIGINT",
      "is_nullable": "NO",
      "column_key": "PRI",
      "column_comment": "用户ID"
    },
    {
      "column_name": "username",
      "data_type": "VARCHAR",
      "is_nullable": "YES",
      "column_comment": "用户名"
    }
  ]
}
*/


-- ============================================================
-- 8. Meta 扫描常见问题排查
-- ============================================================

-- 问题1：扫描不到某些表
-- 检查表是否存在
SHOW TABLES FROM addp_test_db;

-- 检查表是否在 information_schema 中
SELECT TABLE_NAME
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db';

-- 问题2：字段类型映射错误
-- 检查 Doris 数据类型
SELECT DISTINCT DATA_TYPE
FROM information_schema.COLUMNS
WHERE TABLE_SCHEMA = 'addp_test_db';

-- 问题3：中文注释乱码
-- 检查数据库字符集
SHOW VARIABLES LIKE 'character%';

-- 检查表字符集
SELECT
    TABLE_SCHEMA,
    TABLE_NAME,
    TABLE_COLLATION
FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'addp_test_db';


-- ============================================================
-- 9. 清理测试数据（可选）
-- ============================================================

-- 删除性能测试表
-- DROP TABLE IF EXISTS perf_test_1;
-- DROP TABLE IF EXISTS perf_test_2;
-- DROP TABLE IF EXISTS perf_test_3;
-- DROP TABLE IF EXISTS perf_test_4;
-- DROP TABLE IF EXISTS perf_test_5;


/*
Meta 扫描测试总结：

1. **扫描范围**：
   - 数据库列表
   - 表列表（包含表类型、行数、注释）
   - 字段列表（包含字段名、类型、注释、约束）

2. **扫描数据源**：
   - information_schema.SCHEMATA（数据库）
   - information_schema.TABLES（表）
   - information_schema.COLUMNS（字段）

3. **Doris 特有信息**（可选）：
   - SHOW PARTITIONS（分区信息）
   - SHOW ALTER TABLE ROLLUP（Rollup 信息）
   - SHOW INDEX（索引信息）

4. **验证要点**：
   - ✅ 所有表都被扫描
   - ✅ 字段类型映射正确
   - ✅ 中文字段名和注释正常
   - ✅ 无注释字段处理正确
   - ✅ 不同表模型识别正确

5. **性能考虑**：
   - 扫描速度：10-100 表/秒
   - 增量扫描：只扫描变更的表
   - 缓存策略：定期更新元数据缓存

6. **与 MySQL 扫描对比**：
   - 扫描逻辑：完全相同（Doris 兼容 MySQL 协议）
   - 数据类型：Doris 有额外类型（BITMAP, HLL, ARRAY）
   - 特有信息：Doris 分区、Rollup、表模型
*/
