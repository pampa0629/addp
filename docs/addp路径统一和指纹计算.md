### ⚠️ 对象路径命名规范 (重要)

**为确保系统各模块数据一致性,ADDP 平台对对象存储的 bucket 和 path 有严格的命名约定:**

#### 1. Bucket (存储桶)

**定义**: 存储桶名称,不包含路径分隔符

**规范**:
- ✅ 正确: `addp`、`business-data`、`system-files`
- ❌ 错误: `addp/image`、`business-data/2024`

#### 2. Path (对象路径)

**定义**: 对象在桶内的相对路径,**不包含** bucket 名称

**字段别名**:
- `path`: 对象存储引擎和元数据模块中使用
- `object_key`: 向量化服务和检索服务中使用
- `relative_path`: 前端显示和 API 返回中使用

**规范**:
- ✅ 正确: `image/开会.jpg`、`data/2024/report.pdf`、`shapefile/cities.shp`
- ❌ 错误: `addp/image/开会.jpg`、`/image/开会.jpg`

**注意事项**:
- path 以 `/` 分隔目录层级,但不以 `/` 开头
- path 不包含 bucket 名称(避免重复)

#### 3. Full Path (完整路径)

**定义**: 完整的对象路径,仅用于**显示目的**

**格式**: `{bucket}/{path}`

**示例**:
- `addp/image/开会.jpg`
- `business-data/2024/report.pdf`

**使用场景**:
- 前端界面显示完整路径
- 日志记录
- 错误消息

**禁止场景**:
- ❌ 不要用于存储到数据库 (应分别存储 bucket 和 path)
- ❌ 不要用于 fingerprint 计算
- ❌ 不要用于 API 参数传递

#### 4. Fingerprint (数据指纹)

**定义**: 对象的唯一标识符,用于去重和数据血缘追踪

**计算公式**:
```go
fingerprint = SHA256("{engineID}:{bucket}/{path}")
```

**标准函数**: `commonModels.GenerateObjectFingerprint(engineID, bucket, path)`

**示例**:
```go
// 正确示例
engineID := uint(9)
bucket := "addp"
path := "image/开会.jpg"
fingerprint := commonModels.GenerateObjectFingerprint(engineID, bucket, path)
// 计算: SHA256("9:addp/image/开会.jpg")
// 结果: 43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4

// 错误示例 (path 包含 bucket)
path := "addp/image/开会.jpg"  // ❌ 错误！
fingerprint := commonModels.GenerateObjectFingerprint(engineID, bucket, path)
// 计算: SHA256("9:addp/addp/image/开会.jpg")  // bucket 重复！
// 结果: abe230a7d1a1d256fec6293f884442d47f2cd7714e018e869e78763c9eaf146d  // 与正确结果不同
```

#### 5. 影响范围

**该约定影响的功能和模块**:

1. **Meta 模块 (元数据扫描)**:
   - 扫描对象存储时,必须正确设置 bucket 和 path
   - 计算 fingerprint 并写入 `metadata.meta_item` 表
   - 写入 Meilisearch 索引时,包含正确的 document_id

2. **Manager 模块 (向量化和检索)**:
   - 向量化时使用 bucket 和 object_key 计算 fingerprint
   - 混合检索通过 document_id 去重合并结果
   - 必须与 Meta 模块的 fingerprint 保持一致

3. **向量数据库 (manager.embeddings)**:
   - fingerprint 字段作为主键
   - object_key 字段存储不包含 bucket 的相对路径

4. **Meilisearch (全文检索)**:
   - document_id 字段用于混合检索去重
   - 必须与向量数据库的 fingerprint 一致

5. **Common 模块 (指纹计算)**:
   - `common/models/fingerprint.go` 提供标准函数
   - 所有模块必须使用统一函数,禁止自行计算

#### 6. 开发检查清单

**新功能开发时,务必检查**:

- [ ] 存储对象元数据时,bucket 和 path 是否正确分离
- [ ] path 字段是否包含了 bucket (应剔除)
- [ ] fingerprint 是否使用 `GenerateObjectFingerprint` 标准函数
- [ ] fingerprint 计算的 bucket 和 path 参数是否正确
- [ ] 数据库表设计是否分别存储 bucket 和 path (避免存储 full_path)
- [ ] API 参数是否使用 bucket + object_key 而非 full_path

#### 7. 常见错误案例

❌ **错误案例 1: path 包含 bucket**
```go
// Meta 扫描时错误地设置 path
meta := format.ObjectMetadata{
    Bucket: "addp",
    Path:   "addp/image/开会.jpg",  // ❌ path 包含了 bucket
}
fingerprint := commonModels.GenerateObjectFingerprint(engineID, meta.Bucket, meta.Path)
// 结果: SHA256("9:addp/addp/image/开会.jpg")  // bucket 重复
```

✅ **正确做法**:
```go
meta := format.ObjectMetadata{
    Bucket: "addp",
    Path:   "image/开会.jpg",  // ✅ path 不包含 bucket
}
fingerprint := commonModels.GenerateObjectFingerprint(engineID, meta.Bucket, meta.Path)
// 结果: SHA256("9:addp/image/开会.jpg")
```

❌ **错误案例 2: 使用 full_path 存储**
```sql
-- 错误的表设计
CREATE TABLE objects (
    id SERIAL PRIMARY KEY,
    full_path TEXT NOT NULL  -- ❌ 存储 "addp/image/开会.jpg"
);
```

✅ **正确做法**:
```sql
-- 正确的表设计
CREATE TABLE objects (
    id SERIAL PRIMARY KEY,
    bucket VARCHAR(255) NOT NULL,      -- ✅ 存储 "addp"
    path TEXT NOT NULL,                 -- ✅ 存储 "image/开会.jpg"
    fingerprint VARCHAR(64) NOT NULL    -- ✅ 使用标准函数计算
);
```