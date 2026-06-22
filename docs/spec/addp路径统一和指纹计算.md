# ADDP 路径字段统一规范

## 一、对象存储 (MinIO/S3)

### 字段定义

**示例**: `addp/image/开会.jpg`

- **bucket**: `addp` (存储桶名称，不含路径分隔符)
- **path**: `image/` (目录路径，以 `/` 结尾，不含 bucket 和文件名)
- **name**: `开会.jpg` (文件名)
- **full_name**: `addp/image/开会.jpg` (完整路径，拼接规则: `bucket + "/" + path + name`)

对象存储路径必须区分两种语义：

| 字段 / 语义 | 示例 | 说明 |
|---|---|---|
| `bucket` | `addp` | bucket 独立保存，不进入 `path`。 |
| bucket 内 object key | `image/开会.jpg` | scan resource、content mapper 的 bucket 内输入。 |
| `path` | `image/` | attributes 中的目录路径，不含 bucket，不含文件名。 |
| `full_name` / 外部 ref path | `addp/image/开会.jpg` | item 身份、`catalog_paths`、`ref_groups.path` 对外表达。 |

跨模块请求中的 `catalog_paths` 和 `ref_groups.path` 可以使用 `bucket/object_key`；进入 Meta scan 内部对象存储资源模型后，必须拆成 `bucket` 与 bucket 内 `object_key`。不得把 `bucket/object_key` 写入 `attributes.storage.path`，也不得把它传给只接受 bucket 内 key 的 mapper。

### 指纹计算（两步方式）

```go
// 步骤1: 计算 full_name
fullName := commonModels.JoinObjectPath(bucket, path, name)
// "addp" + "/" + "image/" + "开会.jpg" → "addp/image/开会.jpg"

// 步骤2: 计算指纹
fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
// SHA256("9:addp/image/开会.jpg")
// 结果: 43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4
```

### 常见错误

❌ **错误示例**:
```go
// 错误1: path 包含 bucket
path := "addp/image/"  // ❌ 错误
// 应该是 "image/"

// 错误2: path 不以 / 结尾
path := "image"  // ❌ 错误
// 应该是 "image/"

// 错误3: 使用已删除的便利函数
fingerprint := commonModels.GenerateObjectFingerprint(...)  // ❌ 已删除

// 错误4: 把外部 ref path 当成 bucket 内 object key 再拼一次 bucket
bucket := "addp"
objectKey := "addp/image/开会.jpg" // ❌ 错误，应为 "image/开会.jpg"
catalogPath := plugin.ObjectItemPathForBucket(engineID, bucket)(objectKey)
// 结果会指向 addp/addp/image/开会.jpg
```

✅ **正确示例**:
```go
// 方式1: 已知目录和文件名
bucket := "addp"
path := "image/"
name := "开会.jpg"
fullName := commonModels.JoinObjectPath(bucket, path, name)
fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)

// 方式2: 从完整路径拆分
fullPath := "image/开会.jpg"
dir, name := commonModels.SplitObjectPath(fullPath)
// dir = "image/", name = "开会.jpg"
fullName := commonModels.JoinObjectPath(bucket, dir, name)
fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
```

## 二、数据库表

### 字段定义

**示例**: `system.engines`

- **schema**: `system` (数据库模式名)
- **table**: `engines` (表名)
- **full_name**: `system.engines` (拼接规则: `schema + "." + table`)

### 指纹计算（两步方式）

```go
// 步骤1: 计算 full_name
fullName := fmt.Sprintf("%s.%s", schema, table)
// "public" + "." + "buildings" → "public.buildings"

// 步骤2: 计算指纹
fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
// SHA256("2:public.buildings")
```

## 三、文件系统

文件系统包括：NFS、本地文件系统（Linux/macOS/Windows）、SFTP 等。

### root 的定义

不同文件系统类型的 `root` 值不同：

| 文件系统类型 | root 值 | 说明 |
|------------|--------|------|
| NFS | `""` (空字符串) | 挂载点由引擎配置的 export_path 决定，不进入路径 |
| Linux / macOS | `"/"` | 绝对路径以 `/` 开头 |
| Windows | `"C:/"` 等 | 盘符加斜杠 |

### 字段定义

**示例（NFS）**: `dir1/dir2/开会.jpg`

- **root**: `""` (NFS 为空，由引擎配置决定挂载点)
- **path**: `dir1/dir2/` (目录路径，以 `/` 结尾；根目录下的文件 path 为 `""`)
- **name**: `开会.jpg` (文件名)
- **full_name**: `dir1/dir2/开会.jpg` (拼接规则: `root + path + name`)

**示例（Linux）**: `/data/image/开会.jpg`

- **root**: `"/"`
- **path**: `data/image/` (不含 root 前缀，以 `/` 结尾)
- **name**: `开会.jpg`
- **full_name**: `/data/image/开会.jpg` (拼接规则: `root + path + name`)

**根目录下的文件（NFS）**: `README.md`

- **root**: `""`
- **path**: `""` (根目录，空字符串)
- **name**: `README.md`
- **full_name**: `README.md`

### 指纹计算（两步方式）

```go
// NFS 根目录下的文件
root := ""
path := ""
name := "README.md"
fullName := root + path + name  // "README.md"
fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)

// NFS 子目录文件
root = ""
path = "dir1/dir2/"
name = "users.csv"
fullName = root + path + name  // "dir1/dir2/users.csv"
fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)

// Linux 文件系统
root = "/"
path = "data/image/"
name = "开会.jpg"
fullName = root + path + name  // "/data/image/开会.jpg"
fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)
```

## 四、通用原则

1. **full_name 按需拼接**: 动态生成，不冗余存储（除非查询性能必需）
2. **path 语义统一**: 表示目录路径（含 `/` 结尾），不含文件名
3. **name 语义统一**: 表示文件名或表名，不含路径
4. **指纹统一两步**: 先计算 full_name，再调用 `GenerateItemFingerprint`

## 五、ResourceLocator 资源定位符规范

### 5.1 定义

ResourceLocator 是 ADDP 平台中用于唯一标识任何资源的统一定位符，采用 `addp://` 协议 URI 格式。

```go
type ResourceLocator struct {
    EngineID uint         `json:"engine_id"`          // 引擎 ID
    Path     []string     `json:"path"`               // 资源路径（数组）
    Type     ResourceType `json:"type"`               // catalog 术语，不表示内容语义
    NodeID   *uint        `json:"node_id,omitempty"`  // 可选：MetaNode ID
    ItemID   *uint        `json:"item_id,omitempty"`  // 可选：MetaItem ID
}
```

**URI 格式**: `addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}&item_id={item_id}`

`node_id` 和 `item_id` 互斥。`node_id` 表示定位到资源树节点，`item_id` 表示定位到 data item。不得再使用 `meta_id` 同时表达两类 ID，也不得使用 `item_id + 偏移量` 的虚拟 ID 进入 locator。

`type` 保留为 catalog / 路径模型中的稳定术语，用于路径语义、预览路由、树展示和无 ID locator 的辅助解析。`type` 不负责区分 node / item；当 `node_id` 或 `item_id` 存在时，ID 对应的 Meta 事实优先，`type` 只作为校验和路由提示。

搜索结果、预览入口和资源树跳转都应只消费标准 ResourceLocator。前端和跨模块调用方不得根据 engine type、bucket、path、name 等字段自行拼接定位身份；缺少稳定 locator 所需事实的数据应通过重新扫描或重建索引修复。

### 5.1.1 资源树事实入口与 locator 回显

ResourceLocator 是平台级资源身份，不归属于 Manager。Manager、Transfer、Develop、Service、Agent 等模块需要浏览、搜索或回显资源树时，统一消费 Meta resource-tree API：

```http
GET  /api/v1/meta/resource-tree/{engine_id}
GET  /api/v1/meta/resource-tree/{engine_id}/node?locator={resource_locator}
GET  /api/v1/meta/resource-tree/{engine_id}/ancestors?locator={resource_locator}
GET  /api/v1/meta/resource-tree/{engine_id}/search?q={keyword}&limit=50
POST /api/v1/meta/resource-tree/{engine_id}/refresh?locator={resource_locator}
```

其中 `ancestors` 是 `initialLocator` 回显和资源树跳转的标准入口。调用方不得按 locator path 在前端逐层猜测 schema、bucket、目录或表，也不得恢复 Manager 私有 `/tree` 代理作为资源树事实入口。

`ancestors` 必须以 locator 为入口，并由 Meta 按 path 回查当前 `meta_node` / `meta_item` 事实后返回 root 到目标自身的标准 `TreeNode` 链。这样可以在重新扫描导致旧 `node_id` / `item_id` 漂移时，仍按稳定路径重写为当前事实 locator。

### 5.1.2 已有资源与待创建资源

ResourceLocator 只表示已经存在并形成 Meta node / item 事实的资源。新表、新文件、新对象等待创建目标没有真实 `node_id` / `item_id`，不得提前构造虚拟 locator。

创建目标必须表达为父级真实 node 加名称：

```json
{
  "parent_locator": "addp://engine/8/path/public?type=schema&node_id=12",
  "name": "new_table"
}
```

业务模块可以在自己的 DTO 中增加 `target_kind`、`write_mode`、格式选项等执行参数，但资源身份只能引用 `parent_locator`。创建成功并完成 Meta 事实写入或扫描后，才由 Meta / resource-tree 链路返回新资源的真实 locator。

### 5.2 Path 字段语义

**重要**: ResourceLocator 的 `Path` 字段包含**从 bucket/schema 到 name 的完整路径**，与存储层的字段拆分规范不同。

引擎目录根是结构入口，不进入 ResourceLocator 的业务 `Path`。root node 的 `Path` 为空数组，但仍按普通 node 规则携带 `Type` 和 `NodeID`，例如 `addp://engine/8/path/?type=server&node_id=99`。Manager 可以把该节点标题显示为引擎实例名称，但 locator 规则不因此特殊化。

| 存储层 | 拆分规范 | ResourceLocator.Path |
|--------|---------|---------------------|
| 对象存储 | bucket + path + name | `["bucket", "path_segment", ..., "name"]` |
| 数据库表 | schema + table | `["schema", "table"]` |
| 图数据库 graph | database + graph item | `["database", "graph"]` |
| 文件系统 | path + name | `["path_segment", ..., "name"]` |

### 5.3 为什么 Path 包含 bucket/schema？

1. **唯一标识**: 一个 MinIO 引擎可能有多个 bucket（如 addp、gischain、manager），如果 Path 不包含 bucket，则无法区分：
   - `addp/image/test.jpg` 和 `gischain/image/test.jpg`

2. **与 full_name 一致**: `Path.join("/")` 或 `Path.join(".")` 等于 `full_name`，便于计算和理解

3. **无需额外查询**: 所有路径信息都在 Path 中，无需查询 attributes 获取 bucket

4. **统一简单**: 所有资源类型使用相同的逻辑，无需特殊判断

### 5.4 不同资源类型的示例

#### 对象存储 (MinIO/S3)

**数据库存储**（按照字段拆分规范）:
```json
{
  "full_name": "addp/image/开会.jpg",
  "name": "开会.jpg",
  "attributes": {
    "bucket": "addp",
    "path": "image/",
    "name": "开会.jpg"
  }
}
```

**ResourceLocator**:
```go
ResourceLocator{
    EngineID: 9,
    Path:     []string{"addp", "image", "开会.jpg"},  // 包含 bucket
    Type:     "object",
    ItemID:   &456,
}
```

**URI**: `addp://engine/9/path/addp/image/开会.jpg?type=object&item_id=456`

#### 数据库表 (PostgreSQL)

**数据库存储**:
```json
{
  "full_name": "public.users",
  "name": "users"
}
```

**ResourceLocator**:
```go
ResourceLocator{
    EngineID: 8,
    Path:     []string{"public", "users"},  // schema + table
    Type:     "table",
    ItemID:   &123,
}
```

**URI**: `addp://engine/8/path/public/users?type=table&item_id=123`

#### 图数据库 (Neo4j)

Neo4j 的 database 作为路径第一段，graph 作为路径第二段；locator 的 `Type` 必须使用 `graph`。节点 label、relationship type 和连接模式属于 `type_info.graph`，不参与 ResourceLocator 路径。

**graph ResourceLocator**:
```go
ResourceLocator{
    EngineID: 25,
    Path:     []string{"neo4j", "graph"},  // database + graph item
    Type:     "graph",
    ItemID:   &578,
}
```

**graph URI**: `addp://engine/25/path/neo4j/graph?type=graph&item_id=578`

#### 文件系统

**数据库存储**:
```json
{
  "full_name": "data/image/users.csv",
  "name": "users.csv",
  "attributes": {
    "path": "data/image/",
    "name": "users.csv"
  }
}
```

**ResourceLocator**:
```go
ResourceLocator{
    EngineID: 3,
    Path:     []string{"data", "image", "users.csv"},  // 完整路径
    Type:     "file",
    ItemID:   &789,
}
```

**URI**: `addp://engine/3/path/data/image/users.csv?type=file&item_id=789`

### 5.5 Path 构建规则

在 TreeBuilder 中，从 MetaNode 构建 ResourceLocator 时：

```go
// 对象存储: full_name = "addp/image/开会.jpg"
path := parsePath(fullName, "object")
// path = ["addp", "image", "开会.jpg"]

// 数据库表: full_name = "public.users"
path := parsePath(fullName, "table")
// path = ["public", "users"]
```

`parsePath` 函数根据资源类型使用不同的分隔符：
- 对象存储：使用 `/` 分隔
- 数据库表：使用 `.` 分隔
- 图数据库 graph：使用 `.` 分隔，ResourceLocator.Type 保持 `graph`

### 5.6 对比总结

| 层次 | Bucket/Schema | 目录路径 | 文件名/表名 | 完整路径 |
|------|--------------|---------|-----------|---------|
| **存储层字段** | `bucket` 字段 | `path` 字段 | `name` 字段 | `full_name` |
| **ResourceLocator** | Path[0] | Path[1...n-1] | Path[n] | Path.join("/") |

**关键区别**：
- 存储层将路径拆分为独立字段（bucket、path、name），便于查询和索引
- ResourceLocator 将路径合并为数组（Path），便于唯一标识和路由

## 六、Common 模块工具函数

### 核心指纹函数

```go
// GenerateItemFingerprint 是唯一的指纹计算函数
// 所有存储类型都使用此函数
func GenerateItemFingerprint(resID uint, identifier string) string {
    data := fmt.Sprintf("%d:%s", resID, identifier)
    hash := sha256.Sum256([]byte(data))
    return hex.EncodeToString(hash[:])
}
```

### 路径工具函数

```go
// 拆分完整路径为目录和文件名
dir, name := commonModels.SplitObjectPath("image/sub/开会.jpg")
// dir = "image/sub/", name = "开会.jpg"

// 拼接完整对象路径
fullName := commonModels.JoinObjectPath(bucket, path, name)
// "addp" + "/" + "image/" + "开会.jpg" → "addp/image/开会.jpg"
```

### 验证函数

```go
// 验证存储桶名称 (不能包含路径分隔符)
err := commonModels.ValidateBucketName(bucket)

// 验证目录路径 (必须以/结尾，对象存储不能以/开头)
err := commonModels.ValidateDirectoryPath(path, true)  // true表示对象存储
```

## 六、影响范围

### 1. Common 模块

**修改内容**:
- `models/fingerprint.go`:
  - 删除 `GenerateObjectFingerprint`、`GenerateTableFingerprint`、`GenerateFileFingerprint`
  - 保留 `GenerateItemFingerprint`（核心函数）
  - 保留 `SplitObjectPath`、`JoinObjectPath`（工具函数）
  - 保留 `ValidateBucketName`、`ValidateDirectoryPath`（验证函数）

### 2. Meta 模块 (元数据扫描)

**修改内容**:
- `scan_object_storage_service.go`: 使用两步指纹计算，删除 `RelativePath` 字段和 `relative_path` 属性
- `scan_repository.go`: 改为两步指纹计算
- `extractor/metadata_extractor.go`: 改为两步指纹计算
- `search/indexer.go`: `DeleteObjects` 参数从 `relativePath` 改为 `path`

**数据库变更**:
```sql
-- meta.meta_item 表的 attributes
-- 删除 relative_path 和 object_key 字段
UPDATE meta.meta_item
SET attributes = attributes - 'relative_path' - 'object_key'
WHERE attributes ? 'relative_path' OR attributes ? 'object_key';
```

### 3. Manager 模块 (向量化和检索)

**修改内容**:
- `embedding_service.go`: 改为两步指纹计算（2处）
- `quick_view_service.go`: 改为两步指纹计算
- `unified_mvt_service.go`: 改为两步指纹计算

**数据库说明**:
- `manager.embeddings` 当前按 `tenant_id + item_fingerprint` 去重，`locator` 用于回跳；字段语义见 `manager/docs/向量化能力说明.md`

### 4. Meilisearch 索引

**修改内容**:
- `indexer.go`: 删除 `RelativePath` 字段，只保留 `Path` 字段（目录路径）

**索引重建**:
- 清空现有索引数据
- 使用新的字段结构重新扫描和索引

## 七、迁移检查清单

在新功能开发或修改时，务必检查:

- [ ] 使用两步方式计算指纹（先 full_name，再 GenerateItemFingerprint）
- [ ] 对象存储：bucket、path、name 是否正确分离
- [ ] path 字段是否以 `/` 结尾
- [ ] path 字段是否不包含 bucket（应剔除）
- [ ] 数据库表设计是否分别存储 bucket、path、name（避免冗余）
- [ ] API 参数是否使用 bucket + path + name 而非单个 object_key
- [ ] 从旧代码迁移时，是否使用 `SplitObjectPath` 拆分完整路径
- [ ] 不使用已删除的便利函数（GenerateObjectFingerprint 等）

## 八、Meta 查询定位规则

Meta 查询 API 只读取已经扫描并持久化的 `meta_node`、`meta_item`、字段和空间元数据，不触发扫描、不写 `attributes`、不构建 `access_index`，也不创建扫描后派生产物。

查询入口分为两类：

| 入口类型 | 规则 |
|---|---|
| 已定位资源查询 | 已拿到 `node_id` 或 `item_id` 时，使用 `/nodes/:node_id`、`/nodes/:node_id/children`、`/nodes/:node_id/items`、`/items/:item_id`、`/items/:item_id/fields`、`/items/:item_id/spatial` 等主资源入口。 |
| 跨模块条件定位 | 需要从引擎 catalog 路径定位资源时，使用 `engine_id + catalog_path`：`/nodes/by-catalog-path` 或 `/items/by-catalog-path`。 |

`catalog_path` 是 Meta 与引擎 `CatalogModelSpec` 对齐后的稳定定位条件。对象存储路径必须先归一为 catalog path，再通过 `items/by-catalog-path` 定位 object item；不得恢复 `object_key` 作为 Meta 查询主键，也不得新增 `/metadata/table`、`/metadata/file`、`/metadata/spatial`、`/objects/:object_key/metadata` 等按引擎、格式或存储技术形态分叉的快捷查询入口。

`common/client.MetaClient` 只能封装正式查询入口。查询失败不得被包装成隐式扫描、隐式 attributes 补写或派生产物创建。

## 九、数据库表字段对照

| 模块 | 表名 | Bucket字段 | 目录字段 | 文件名字段 | 完整路径 |
|------|------|-----------|---------|----------|---------|
| Meta | meta.meta_item | attributes.bucket | 无 (通过NodeID) | Name | FullName |
| Meta | meta.meta_node | 无 | Path (层级路径) | Name | FullName |
| Manager | manager.embeddings | 不保存为核心字段 | 不保存为核心字段 | 不保存为核心字段 | locator / item_fingerprint |
| Meilisearch | assets索引 | bucket | path (目录) | name | full_name |

## 十、指纹计算示例汇总

### 对象存储

```go
// 示例1：对象存储文件
engineID := uint(9)
bucket := "addp"
path := "image/"
name := "开会.jpg"

fullName := commonModels.JoinObjectPath(bucket, path, name)
// fullName = "addp/image/开会.jpg"

fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
// fingerprint = SHA256("9:addp/image/开会.jpg")
// = "43788d99024bc40b4b7d19ed651f68014d18fa0199a1fb8d471cffb9897b67e4"
```

### 数据库表

```go
// 示例2：数据库表
engineID := uint(2)
schema := "public"
table := "buildings"

fullName := fmt.Sprintf("%s.%s", schema, table)
// fullName = "public.buildings"

fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
// fingerprint = SHA256("2:public.buildings")
```

### 文件系统

```go
// 示例3a：NFS 根目录下的文件
engineID := uint(3)
root := ""
path := ""
name := "README.md"

fullName := root + path + name
// fullName = "README.md"

fingerprint := commonModels.GenerateItemFingerprint(engineID, fullName)
// fingerprint = SHA256("3:README.md")

// 示例3b：NFS 子目录文件
root = ""
path = "dir1/dir2/"
name = "users.csv"

fullName = root + path + name
// fullName = "dir1/dir2/users.csv"

fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)
// fingerprint = SHA256("3:dir1/dir2/users.csv")

// 示例3c：Linux 文件系统
root = "/"
path = "data/image/"
name = "开会.jpg"

fullName = root + path + name
// fullName = "/data/image/开会.jpg"

fingerprint = commonModels.GenerateItemFingerprint(engineID, fullName)
// fingerprint = SHA256("3:/data/image/开会.jpg")
```

## 十一、常见问题 (FAQ)

**Q: 为什么删除便利函数？**
A: 简化概念，统一为两步计算方式，减少函数数量和参数混淆。

**Q: 如何从旧代码迁移？**
A: 查找所有 `GenerateObjectFingerprint` 等函数调用，改为两步方式（参考上面示例）。

**Q: 指纹会改变吗？**
A: 不会。两步方式计算的结果与便利函数完全一致，只是调用方式不同。

**Q: full_name 是否需要存储？**
A: 优先动态拼接。只有在查询性能关键时才考虑冗余存储。

**Q: attributes 中还能保留旧字段吗？**
A: 不能。旧 attributes 字段、旧分区和平铺字段不保留兼容读取或兼容写入。历史数据应删除后重新 Meta 扫描生成新结构；仍依赖旧结构的代码应尽早暴露并修正。
