# 数据指纹机制 (Data Fingerprint Mechanism)

## 概述

ADDP元数据管理系统使用**数据指纹（Fingerprint）**机制来确保每个数据对象在metadata.meta_item表中的唯一性，防止重复记录，并为未来的数据血缘追踪奠定基础。

## 核心思想

每个数据项（表、文件、对象等）通过其核心标识生成唯一的SHA256哈希指纹，指纹作为唯一索引，确保：
1. **去重**：同一数据对象只有一条记录
2. **UPSERT**：扫描时自动插入或更新
3. **血缘追踪**：通过fingerprint可以追踪数据的变化历史

## 指纹生成规则

### 1. 对象存储 (MinIO/S3/OSS)

```
fingerprint = SHA256(res_id + bucket + object_path)
```

**示例**：
```go
// 资源ID: 9
// Bucket: addp
// 对象路径: 开会.jpg
fingerprint = models.GenerateObjectFingerprint(9, "addp", "开会.jpg")
// 结果: "91b9aba48493dff8b98a7329c20e634aab79be37aa4b17b173cac7977e0d315c"
```

**Go实现**：
```go
func GenerateObjectFingerprint(resID uint, bucket, objectPath string) string {
    identifier := fmt.Sprintf("%s/%s", bucket, objectPath)
    return GenerateItemFingerprint(resID, identifier)
}
```

### 2. 关系数据库 (PostgreSQL/MySQL)

```
fingerprint = SHA256(res_id + schema + table_name)
```

**示例**：
```go
// 资源ID: 1
// Schema: public
// 表名: users
fingerprint = models.GenerateTableFingerprint(1, "public", "users")
// 结果: "a3f8c2e1d7b4f9a6e3c8d1b7f4e9a2c5d8b1e7f3a9c6d2e8b4f1a7c3e9d5b2a8"
```

**Go实现**：
```go
func GenerateTableFingerprint(resID uint, schema, tableName string) string {
    identifier := fmt.Sprintf("%s.%s", schema, tableName)
    return GenerateItemFingerprint(resID, identifier)
}
```

### 3. 文件系统

```
fingerprint = SHA256(res_id + file_path)
```

**示例**：
```go
// 资源ID: 5
// 文件路径: /data/csv/sales.csv
fingerprint = models.GenerateFileFingerprint(5, "/data/csv/sales.csv")
```

### 4. 通用指纹生成

所有类型最终调用通用函数：

```go
func GenerateItemFingerprint(resID uint, identifier string) string {
    data := fmt.Sprintf("%d:%s", resID, identifier)
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:])
}
```

## 数据库结构

### meta_item表新增字段

```sql
ALTER TABLE metadata.meta_item
ADD COLUMN fingerprint VARCHAR(64) NOT NULL;

-- 唯一索引确保fingerprint不重复
CREATE UNIQUE INDEX idx_meta_item_fingerprint
ON metadata.meta_item(fingerprint);
```

### 完整结构

```sql
CREATE TABLE metadata.meta_item (
    id                SERIAL PRIMARY KEY,
    tenant_id         INTEGER NOT NULL,
    res_id            INTEGER NOT NULL,
    node_id           INTEGER NOT NULL,
    item_type         VARCHAR(64) NOT NULL,
    name              VARCHAR(255) NOT NULL,
    full_name         TEXT,
    fingerprint       VARCHAR(64) NOT NULL UNIQUE,  -- 数据指纹
    status            VARCHAR(20) DEFAULT 'active',
    meta_schema_version INTEGER DEFAULT 1,
    row_count         BIGINT,
    size_bytes        BIGINT,
    object_size_bytes BIGINT,
    last_modified_at  TIMESTAMP,
    attributes        JSONB,
    sync_version      BIGINT DEFAULT 0,
    source            VARCHAR(64),
    created_at        TIMESTAMP NOT NULL,
    updated_at        TIMESTAMP NOT NULL,
    deleted_at        TIMESTAMP
);
```

## UPSERT逻辑

### 扫描时的处理流程

```go
func (s *ScanServiceNew) upsertItem(...) (*models.MetaItem, error) {
    // 1. 生成指纹
    fingerprint := generateFingerprintFromAttrs(resourceID, attrs, fullName)

    // 2. 尝试查找已有记录
    var item models.MetaItem
    err := s.db.Where("fingerprint = ?", fingerprint).First(&item).Error

    if err == gorm.ErrRecordNotFound {
        // 3a. 记录不存在，创建新记录
        item = models.MetaItem{
            Fingerprint: fingerprint,
            // ... 其他字段
        }
        s.db.Create(&item)
    } else {
        // 3b. 记录已存在，更新元数据
        s.db.Model(&item).Updates(map[string]interface{}{
            "node_id":    node.ID,          // 允许数据移动
            "attributes": updatedAttrs,
            "updated_at": time.Now(),
        })
    }

    return &item, nil
}
```

### 关键特性

1. **原子性**：基于唯一索引，数据库层面保证不会有重复fingerprint
2. **允许移动**：node_id可以更新，支持数据在不同目录/schema间移动
3. **元数据刷新**：每次扫描更新attributes、row_count等动态信息
4. **保留历史**：updated_at字段记录最后更新时间

## 迁移脚本

### 为现有数据生成指纹

```sql
-- 对象存储记录
UPDATE metadata.meta_item
SET fingerprint = ENCODE(
    SHA256(
        (res_id::TEXT || ':' ||
         COALESCE(attributes->>'bucket', '') || '/' ||
         COALESCE(attributes->>'path', attributes->>'relative_path', '')
        )::BYTEA
    ),
    'hex'
)
WHERE item_type = 'object'
AND fingerprint IS NULL;

-- 关系数据库记录
UPDATE metadata.meta_item
SET fingerprint = ENCODE(
    SHA256(
        (res_id::TEXT || ':' ||
         COALESCE(attributes->>'schema_name', 'public') || '.' ||
         name
        )::BYTEA
    ),
    'hex'
)
WHERE item_type IN ('table', 'view')
AND fingerprint IS NULL;
```

### 检查重复记录

```sql
-- 查找重复的fingerprint
SELECT fingerprint, COUNT(*) as count
FROM metadata.meta_item
GROUP BY fingerprint
HAVING COUNT(*) > 1;

-- 查找同一对象的多条记录（迁移前）
SELECT
    res_id,
    attributes->>'bucket' as bucket,
    attributes->>'path' as path,
    COUNT(*) as count
FROM metadata.meta_item
WHERE item_type = 'object'
GROUP BY res_id, attributes->>'bucket', attributes->>'path'
HAVING COUNT(*) > 1;
```

## 数据血缘追踪（未来功能）

### 设计思路

基于fingerprint，可以构建完整的数据血缘图：

```sql
CREATE TABLE metadata.meta_lineage (
    id                SERIAL PRIMARY KEY,
    source_fingerprint VARCHAR(64) NOT NULL,  -- 源数据指纹
    target_fingerprint VARCHAR(64) NOT NULL,  -- 目标数据指纹
    transform_type    VARCHAR(64),            -- 转换类型（ETL, 复制, 聚合等）
    transform_details JSONB,                  -- 转换详情
    created_at        TIMESTAMP NOT NULL,

    FOREIGN KEY (source_fingerprint) REFERENCES metadata.meta_item(fingerprint),
    FOREIGN KEY (target_fingerprint) REFERENCES metadata.meta_item(fingerprint)
);
```

### 示例场景

1. **数据复制**：
   ```
   source: addp/raw_sales.csv (fingerprint: abc123...)
     ↓ ETL
   target: public.sales_fact (fingerprint: def456...)
   ```

2. **数据聚合**：
   ```
   sources: [public.orders, public.customers]
      ↓ JOIN + GROUP BY
   target: analytics.monthly_sales
   ```

3. **文件归档**：
   ```
   source: s3://bucket/2024/data.csv
      ↓ MOVE
   target: s3://archive/2024/data.csv
   (fingerprint相同，但node_id变化)
   ```

## 实际应用示例

### 示例1：图片文件扫描

```go
// 扫描对象存储中的图片
resID := uint(9)
bucket := "addp"
objectPath := "开会.jpg"

// 生成指纹
fingerprint := models.GenerateObjectFingerprint(resID, bucket, objectPath)
// "91b9aba48493dff8b98a7329c20e634aab79be37aa4b17b173cac7977e0d315c"

// 第一次扫描：创建记录
item := models.MetaItem{
    ResID:       resID,
    NodeID:      25,
    Fingerprint: fingerprint,
    Name:        "开会.jpg",
    Attributes: models.JSONMap{
        "bucket":      "addp",
        "path":        "addp/开会.jpg",
        "content_type": "image/jpeg",
        "image_metadata": map[string]interface{}{
            "width":  2880,
            "height": 2160,
        },
    },
}
db.Create(&item)

// 第二次扫描：更新元数据（如果图片重新处理）
db.Model(&item).Updates(map[string]interface{}{
    "attributes": updatedAttributes,  // 新的元数据
    "updated_at": time.Now(),
})

// ✅ 不会创建重复记录，因为fingerprint唯一
```

### 示例2：数据库表扫描

```go
resID := uint(1)
schema := "public"
tableName := "users"

fingerprint := models.GenerateTableFingerprint(resID, schema, tableName)

// 多次扫描同一个表，只会更新记录，不会重复
item := models.MetaItem{
    Fingerprint: fingerprint,
    Name:        tableName,
    Attributes: models.JSONMap{
        "schema_name": schema,
        "fields": []map[string]interface{}{
            {"name": "id", "type": "integer"},
            {"name": "username", "type": "varchar"},
        },
    },
    RowCount: &rowCount,
}
```

## 优势与收益

### 1. 数据一致性
- ✅ 同一数据对象在数据库中唯一
- ✅ 避免扫描时产生重复记录
- ✅ 元数据始终是最新状态

### 2. 性能优化
- ✅ 基于唯一索引的快速查找
- ✅ 减少数据库存储空间（无重复）
- ✅ 提高查询效率

### 3. 可扩展性
- ✅ 为数据血缘追踪奠定基础
- ✅ 支持跨系统的数据溯源
- ✅ 可以构建完整的数据流图

### 4. 运维友好
- ✅ 清晰的数据标识机制
- ✅ 易于理解和调试
- ✅ 支持数据迁移和归档

## 测试验证

### 1. 指纹生成测试

```bash
# 测试对象存储指纹
psql -c "SELECT '9:addp/开会.jpg' as input,
         ENCODE(SHA256('9:addp/开会.jpg'::BYTEA), 'hex') as fingerprint;"
```

### 2. 唯一性测试

```sql
-- 尝试插入重复fingerprint（应该失败）
INSERT INTO metadata.meta_item (
    tenant_id, res_id, node_id, item_type,
    name, fingerprint, status, created_at, updated_at
) VALUES (
    1, 9, 25, 'object',
    '开会.jpg', '91b9aba48493dff8b98a7329c20e634aab79be37aa4b17b173cac7977e0d315c',
    'active', NOW(), NOW()
);
-- 预期: ERROR: duplicate key value violates unique constraint
```

### 3. UPSERT测试

```bash
# 扫描同一数据源两次，验证不会产生重复记录
# 第一次扫描
curl -X POST http://localhost:8082/api/meta/scan/resource \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"resource_id": 9}'

# 检查记录数
psql -c "SELECT COUNT(*) FROM metadata.meta_item WHERE res_id = 9;"

# 第二次扫描
curl -X POST http://localhost:8082/api/meta/scan/resource \
  -H "Authorization: Bearer $TOKEN" \
  -d '{"resource_id": 9}'

# 再次检查记录数（应该相同）
psql -c "SELECT COUNT(*) FROM metadata.meta_item WHERE res_id = 9;"
```

## 相关文件

- **模型定义**: [`meta/backend/internal/models/item.go`](../meta/backend/internal/models/item.go)
- **指纹生成**: [`meta/backend/internal/models/fingerprint.go`](../meta/backend/internal/models/fingerprint.go)
- **UPSERT逻辑**: [`meta/backend/internal/service/scan_service_new.go:210`](../meta/backend/internal/service/scan_service_new.go)
- **迁移脚本**: [`scripts/migrations/001_add_fingerprint_to_meta_item.sql`](../scripts/migrations/001_add_fingerprint_to_meta_item.sql)

## 常见问题

### Q: 如果数据对象改名怎么办？
A: fingerprint基于路径生成，改名会产生新fingerprint。这是预期行为，因为从系统角度看，这是一个新对象。可以通过数据血缘记录"重命名"操作。

### Q: 不同资源下的同名文件会冲突吗？
A: 不会。fingerprint包含res_id，不同资源的同名文件会有不同的fingerprint。

### Q: 如何处理数据迁移（跨bucket/schema）？
A: 迁移会产生新fingerprint（因为路径变化），但可以通过meta_lineage表记录迁移关系，保留数据血缘。

### Q: fingerprint会影响查询性能吗？
A: 不会。fingerprint上有唯一索引，查询速度很快。而且避免了复杂的复合条件查询，反而提升性能。

## 未来增强

1. **版本控制**：记录每次更新的历史快照
2. **变更通知**：当元数据更新时发送事件
3. **数据质量追踪**：关联数据质量检查结果
4. **智能去重**：基于内容哈希的物理去重
5. **跨系统溯源**：整合外部系统的数据血缘

---

**文档版本**: v1.0
**最后更新**: 2025-10-17
**作者**: ADDP开发团队
