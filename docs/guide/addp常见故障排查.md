# ADDP 常见故障排查指南

本文档记录 ADDP 平台开发和使用过程中遇到的常见问题及解决方案。

---

## 脚本/启动问题

### 1. restart.sh 误报端口被浏览器占用（端口检查假阳性）

#### 问题现象

运行 `./scripts/dev/restart.sh -system`（或其他模块）时，报错：
```
✗ 端口 8180 已被非 ADDP 进程占用 (PID: 1785)
  进程: /Applications/Google Chrome.app/...
✗ 无法启动 system
```
但实际上用 `lsof -i :8180` 查询端口为空闲状态。

#### 根本原因

`scripts/dev/start.sh` 中的端口检查原先使用：
```bash
lsof -ti :$port
```
该命令会匹配所有 TCP 连接（含 ESTABLISHED、TIME_WAIT 等状态），不仅仅是监听中的进程。Chrome 等浏览器建立 HTTPS 连接时，操作系统随机分配临时出站端口（ephemeral port），偶尔会恰好用到 8180，导致误报。

#### 修复方案

改用 `-sTCP:LISTEN` 标志，只检查真正在监听（LISTEN 状态）该端口的进程：
```bash
# 修复前（错误）
lsof -ti :$port

# 修复后（正确）
lsof -ti :$port -sTCP:LISTEN
```

已在 `scripts/dev/start.sh` 的 `check_service_running()` 函数中修复（约第 422 行）。

---

## 前端问题

### 1. Manager 数据预览显示"暂无数据"（双重 .data 访问问题）

#### 问题现象

- 用户在 Manager 模块的数据浏览器中点击表格节点
- 右侧预览面板显示"暂无数据"
- 后端 API 返回 200 成功状态，数据完整
- 浏览器控制台无明显错误

#### 问题根因

**双重 `.data` 访问导致数据丢失**

数据流跟踪：

1. **后端返回**（正确）
   ```json
   HTTP 200 OK
   {
     "mode": "table",
     "columns": ["id", "name", ...],
     "rows": [{...}, {...}]
   }
   ```

2. **createAPIClient 自动提取**（正确）
   - 配置：`extractData = true`（默认开启）
   - 响应拦截器：`return response.data`
   - 结果：API 调用返回已提取的数据对象

3. **问题发生点**（错误）
   ```javascript
   // manager/frontend/src/views/DataExplorer.vue
   const response = await dataExplorerAPI.getPreview(params)
   // response = { mode: "table", columns: [...], rows: [...] } ✅

   previewData.value = normalizePreviewPayload(response.data, selectedNode.value)
   //                                           ^^^^^^^^^^^^^
   //                                           undefined! ❌
   ```

4. **渲染层判断**（触发"暂无数据"）
   ```vue
   <div v-else-if="!previewData" class="empty-state">
     <el-empty description="暂无数据" />
   </div>
   ```

#### 技术细节

**common-frontend 的 createAPIClient 设计：**

位置：`common-frontend/basic/src/composables/useAuth.js`

```javascript
export function createAPIClient(getAuthStore, options = {}) {
  const {
    extractData = true,  // ← 默认开启自动提取
    // ...
  } = options

  // 响应拦截器
  client.interceptors.response.use(
    (response) => {
      const processedResponse = refreshOnFulfilled(response)
      return extractData ? processedResponse.data : processedResponse
      //                    ^^^^^^^^^^^^^^^^^^^^ 已经提取了 .data
    },
    // ...
  )
}
```

**为什么问题被隐藏：**
- 后端日志显示成功（200 状态）
- 无 JavaScript 运行时错误（`undefined.data` 不抛出异常）
- 用户体验误导（"暂无数据"让用户以为数据库是空的）
- 开发者习惯性写 `.data` 而不知道已自动提取

#### 解决方案

**关键发现：不同 API 的响应格式不一致**

检查后端代码发现：
- **PreviewTable API**（`/api/data-explorer/preview`）：直接返回数据对象，无 `{data: ...}` 包装
- **ListEngines API**（`/api/data-explorer/engines`）：返回 `{data: engines}` 格式
- **GetTree API**（`/api/data-explorer/tree`）：返回 `{data: tree}` 格式

因此 **只需修改预览 API 的调用**，资源列表和树接口保持原样。

**修改：数据预览（唯一需要修改的地方）**

文件：`manager/frontend/src/views/DataExplorer.vue`，第 532 行

```diff
const response = await dataExplorerAPI.getPreview(params)
- previewData.value = normalizePreviewPayload(response.data, selectedNode.value)
+ previewData.value = normalizePreviewPayload(response, selectedNode.value)
```

**不需要修改的地方**（保持 `.data` 访问）：
- 第 376 行：`response.data` ✅ 正确（ListEngines 返回 `{data: ...}`）
- 第 476 行：`response.data` ✅ 正确（GetTree 返回 `{data: ...}`）

#### 验证方法

1. **修改代码后重启前端**
   ```bash
   # Manager 前端会自动热重载
   # 如果未生效，手动重启
   bash scripts/dev/restart.sh
   ```

2. **浏览器测试**
   - 刷新 Manager 页面
   - 点击左侧树中的表格节点
   - 应该看到表格数据而不是"暂无数据"

3. **添加临时调试（可选）**
   ```javascript
   const response = await dataExplorerAPI.getPreview(params)
   console.log('🔍 API 响应:', response)
   console.log('🔍 Response 类型:', typeof response)
   console.log('🔍 Response 键:', Object.keys(response || {}))
   ```

#### 预防措施

1. **后端响应格式规范化（建议）**
   - 统一所有 API 使用 `c.JSON(http.StatusOK, gin.H{"data": ...})` 格式
   - 或统一所有 API 直接返回数据对象
   - 当前混合格式容易导致混淆

2. **代码审查检查清单**
   - 检查后端 handler 的响应格式（是否有 `gin.H{"data": ...}` 包装）
   - 前端调用时根据后端格式决定是否使用 `.data`
   - 搜索 `response.data.data`（双重访问）避免错误

3. **使用 TypeScript（推荐）**
   ```typescript
   // 类型定义明确返回值
   function getPreview(params): Promise<TablePreview>  // 直接返回数据
   function getEngines(): Promise<{data: Engine[]}>  // 包装格式
   ```

#### 相关问题

如果遇到类似"数据加载成功但不显示"的问题，检查以下几点：

1. **后端 API 是否返回 200**
   - 打开浏览器开发者工具 → Network 标签
   - 检查响应状态码和响应体

2. **前端是否正确接收数据**
   - 在控制台打印 `response` 对象
   - 检查是否误用了 `.data`

3. **预览组件是否匹配数据格式**
   - 检查 `mode` 字段是否存在
   - 检查预览插件的 `canHandle` 函数

#### 修复日期

- **发现日期：** 2025-12-18
- **修复版本：** v0.0.15+
- **影响范围：** Manager 模块数据浏览器

### 2. Manager 刷新 item 成功但属性未更新（item 刷新误走 catalog scan）

#### 问题现象

- 点击 Manager 的“刷新数据项”按钮后，接口提示成功。
- 但 item 的属性仍然缺少新的能力或索引信息，例如 `access_index` 仍为空。
- 现象在 Shapefile 这类 multi-ref item 上最容易暴露，但根因不局限于某一种格式。

#### 根本原因

item 刷新原先复用了 catalog scan 的入口，把 `item_id` 先转换为 catalog paths，再从目录或对象路径重新发现 item。

这会混淆两类输入：

1. catalog scan 的输入是扫描范围和候选资源集合，用于发现 item。
2. item refresh 的输入应是已知 item descriptor，用于基于现有 `layout`、`format`、`refs` 和 `storage` 刷新属性。

对于 multi-ref item，如果把 `refs` 当作多个扫描路径，会破坏原本的候选集合，复合识别入口无法再次获得完整 refs。于是刷新流程可能正常结束，但没有真正重算并落盘该 item 的深度属性。

#### 修复方案

- Manager 的 item 刷新只通过 Meta client 调用 Meta item refresh 能力。
- Meta 新增已知 item refresh 路径：从标准 attributes 还原 item descriptor，再按 format provider 能力刷新属性并落回同一个 item。
- catalog scan 继续负责发现 item；item refresh 不再通过 `item_id` 反推父目录，也不通过 `refs` 反推扫描范围。

#### 预防措施

1. 新增或修改 item refresh 能力时，先还原标准 item descriptor，不要在 Manager 或具体扫描服务中重复格式识别。
2. `refs` 只表示 item 的组成内容，不应直接作为 catalog scan target。
3. `layout=multi` 的 item refresh 应使用已落库 `attributes.item.refs`，不得把父目录或 `storage.physical_path` 目录当作普通文件读取。
4. 如果 item 刷新成功但属性未变化，先检查该 item 的 `attributes.item.layout / item.format / item.refs / storage` 是否足以打开内容。

---

## 后端问题

### 1. MVT 物化视图创建失败 - column "id" does not exist（主键大小写问题）

#### 问题现象

在 Manager 模块的数据预览界面，点击"准备预缓存"按钮时，物化视图创建检查失败：

```
物化视图创建失败: ERROR: column "id" does not exist (SQLSTATE 42703)
```

错误信息显示：
- 检查项：物化视图 ❌ 失败
- 说明：物化视图创建失败: ERROR: column "id" does not exist (SQLSTATE 42703)
- 检查项：空间索引 ❌ 失败（因物化视图不存在而失败）

#### 问题根因

**三个核心问题：**

1. **硬编码主键列名为 `id`**
   - 原代码假设所有表的主键都叫 `id`
   - 实际表的主键可能是 `SmID`、`gid`、`objectid`、`fid` 等
   - 导致 SQL 语句引用不存在的列

2. **未处理 PostgreSQL 大小写敏感性 - 主键列**
   - PostgreSQL 中不带引号的标识符会自动转换为小写
   - 如果主键是 `SmID`（混合大小写），必须用双引号包裹：`"SmID"`
   - 原代码生成的 SQL：`SELECT SmID AS id` → 被识别为 `smid` → 找不到列
   - 正确的 SQL：`SELECT "SmID" AS id` → 保留原始大小写

3. **未处理 PostgreSQL 大小写敏感性 - 几何列**
   - 同样的问题也出现在几何列上（如 `SmGeometry`、`SHAPE`、`geom` 等）
   - 原代码：`ST_Transform(SmGeometry, 3857)` → 被识别为 `smgeometry` → 找不到列
   - 正确写法：`ST_Transform("SmGeometry", 3857)` → 保留原始大小写
   - WHERE 子句也需要用引号：`WHERE "SmGeometry" IS NOT NULL`

#### 技术细节

**原代码问题**（`manager/backend/internal/mvt/preparation_service.go:445-452`）：

```go
// 创建物化视图（转换为 3857）
createMVSQL := fmt.Sprintf(`
    CREATE MATERIALIZED VIEW %s AS
    SELECT
        id,                          -- ❌ 问题1：硬编码，假设主键叫 id
        ST_Transform(%s, 3857) AS geom_3857  -- ❌ 问题2：几何列未加引号
    FROM %s.%s
    WHERE %s IS NOT NULL             -- ❌ 问题3：WHERE 子句的列未加引号
`, mvFullName, geomColumn, schema, table, geomColumn)
```

**具体错误示例：**

```sql
-- 假设表结构：SmID (主键), SmGeometry (几何列)

-- 原代码生成的 SQL（错误）
CREATE MATERIALIZED VIEW public.test_mv3857 AS
SELECT
    id,                              -- ❌ 列不存在
    ST_Transform(SmGeometry, 3857) AS geom_3857  -- ❌ 找不到 smgeometry
FROM public.test
WHERE SmGeometry IS NOT NULL         -- ❌ 找不到 smgeometry

-- 正确的 SQL
CREATE MATERIALIZED VIEW public.test_mv3857 AS
SELECT
    "SmID" AS id,                    -- ✅ 保留大小写
    ST_Transform("SmGeometry", 3857) AS geom_3857  -- ✅ 保留大小写
FROM public.test
WHERE "SmGeometry" IS NOT NULL       -- ✅ 保留大小写
```

**PostgreSQL 大小写规则：**
```sql
-- 不带引号 → 自动转为小写
SELECT SmID FROM test;     -- 查找 smid 列（失败）

-- 带引号 → 保留原始大小写
SELECT "SmID" FROM test;   -- 查找 SmID 列（成功）
```

**Meta 模块的主键存储：**
- Meta 已经扫描并存储了表的主键信息
- 存储位置：`meta.meta_item.attributes` JSONB 字段
  - `attributes.type_info.table.primary_key` - 主键列名列表
  - `attributes.type_info.table.fields[].primary_key` - 每个字段的主键标记
- Manager 可以通过 `metaClient.GetTableSpatialMetadata()` 获取主键信息

#### 解决方案

**修复步骤：**

1. **动态查询主键列名**（不再硬编码）

添加查询主键的方法（`manager/backend/internal/mvt/preparation_service.go:408-426`）：

```go
// getPrimaryKeyColumn 获取表的主键列名
// 返回主键列名，如果没有主键返回空字符串
func (ps *PreparationService) getPrimaryKeyColumn(ctx context.Context, engineDB *gorm.DB, schema, table string) (string, error) {
    var pkColumn string
    query := `
        SELECT a.attname
        FROM pg_index i
        JOIN pg_attribute a ON a.attrelid = i.indrelid AND a.attnum = ANY(i.indkey)
        WHERE i.indrelid = ($1 || '.' || $2)::regclass
          AND i.indisprimary
        LIMIT 1
    `
    err := engineDB.WithContext(ctx).Raw(query, schema, table).Scan(&pkColumn).Error
    if err != nil {
        // 如果查询失败（可能是没有主键），返回空字符串而不是错误
        return "", nil
    }
    return pkColumn, nil
}
```

2. **处理大小写问题**（用双引号包裹所有列名）

修改物化视图创建逻辑（`manager/backend/internal/mvt/preparation_service.go:452-474`）：

```go
// 查询主键列名
pkColumn, err := ps.getPrimaryKeyColumn(ctx, engineDB, schema, table)
if err != nil {
    prepStatus.Checks[i].Status = "failed"
    prepStatus.Checks[i].Message = fmt.Sprintf("查询主键失败: %v", err)
    return fmt.Errorf("failed to get primary key: %w", err)
}

// 构建SELECT子句
var selectClause string
if pkColumn != "" {
    // 有主键，使用实际主键列（用双引号包裹以保留大小写）
    selectClause = fmt.Sprintf(`"%s" AS id`, pkColumn)
    prepStatus.Checks[i].Details["primary_key"] = pkColumn
} else {
    // 没有主键，生成临时ID
    selectClause = "row_number() OVER () AS id"
    prepStatus.Checks[i].Details["primary_key"] = "generated"
    prepStatus.Checks[i].Details["warning"] = "源表无主键，使用 row_number() 生成临时ID"
}

// 创建物化视图（转换为 3857）
// 注意：所有列名都用双引号包裹以保留大小写（PostgreSQL 区分大小写）
createMVSQL := fmt.Sprintf(`
    CREATE MATERIALIZED VIEW %s AS
    SELECT
        %s,                          -- ✅ 动态主键，支持大小写
        ST_Transform("%s", 3857) AS geom_3857  -- ✅ 几何列加引号
    FROM %s.%s
    WHERE "%s" IS NOT NULL           -- ✅ WHERE 子句也加引号
`, mvFullName, selectClause, geomColumn, schema, table, geomColumn)
```

3. **在检查阶段也添加主键诊断**（`manager/backend/internal/mvt/preparation_service.go:157-166`）：

```go
// 快速检查：获取源表的主键
pkColumn, err := ps.getPrimaryKeyColumn(ctx, engineDB, schema, table)
if err != nil {
    // 查询主键时的错误不应该阻止物化视图创建
    check.Details["pk_check_warning"] = fmt.Sprintf("查询主键失败: %v，将使用 row_number() 生成ID", err)
}
check.Details["primary_key"] = pkColumn
if pkColumn == "" {
    check.Details["primary_key_status"] = "无主键，将生成临时ID"
}
```

#### 验证修复

```bash
# 重启 Manager 服务
bash scripts/dev/restart.sh -manager

# 在前端测试
# 1. 打开 Manager → 数据浏览器
# 2. 选择包含混合大小写主键的表（如 public.test，主键 SmID）
# 3. 点击"准备预缓存"
# 4. 应该显示：
#    - 物化视图 ✅ 通过（或标记为待创建）
#    - Details 中显示正确的主键名（如 "SmID"）
```

**SQL 验证：**
```sql
-- 查看创建的物化视图结构
\d public.test_mv3857

-- 应该包含：
-- Column    | Type
-- ----------+---------
-- id        | bigint     (从 SmID 映射而来)
-- geom_3857 | geometry
```

#### 支持的场景

修复后支持以下所有场景：

**主键列名：**
1. ✅ **混合大小写主键**（如 `SmID`、`ObjectID`、`FeatureId`）
2. ✅ **全小写主键**（如 `id`、`gid`、`fid`）
3. ✅ **全大写主键**（如 `ID`、`FID`）
4. ✅ **无主键表**（生成 `row_number() OVER () AS id`，标记警告）

**几何列名：**
1. ✅ **混合大小写几何列**（如 `SmGeometry`、`TheGeom`、`Shape`）
2. ✅ **全小写几何列**（如 `geom`、`geometry`、`shape`）
3. ✅ **全大写几何列**（如 `GEOM`、`SHAPE`、`GEOMETRY`）

**常见的 GIS 数据列名组合：**
- SuperMap 数据：`SmID` + `SmGeometry` ✅
- ArcGIS 数据：`OBJECTID` + `SHAPE` ✅
- PostGIS 默认：`id` + `geom` ✅
- QGIS 导入：`fid` + `geometry` ✅

#### 无主键表的限制

如果表没有主键，系统会生成临时 ID，但有以下限制：

- ✅ **可以生成** MVT 瓦片并在地图上显示
- ✅ **可以进行** 基本的地图浏览和缩放
- ❌ **无法唯一标识** 要素（点击、高亮等交互功能受限）
- ⚠️ **数据刷新后** ID 可能变化（row_number 基于查询顺序）

**建议**：前端应提示用户表没有主键，建议添加主键以支持完整功能。

#### 未来优化建议

1. **优先从 Meta 获取主键**
   - Meta 已经扫描过表结构，有主键信息缓存
   - 减少数据库查询，提升性能
   - 作为备用方案才直接查询数据库

2. **支持复合主键**
   - 当前只取第一个主键列
   - 可改为连接多个主键列：`CONCAT(col1, '-', col2) AS id`

3. **前端显示主键状态**
   - 在预缓存配置界面显示主键信息
   - 无主键时明确警告用户功能限制

#### 相关影响范围

此问题影响以下模块和功能：

**受影响的模块：**
1. **Manager 模块**
   - MVT 预缓存准备阶段（物化视图创建）
   - 空间索引创建（依赖物化视图）
   - 瓦片生成任务（依赖物化视图）

**受影响的数据源：**
2. **使用混合大小写列名的 PostgreSQL 表**
   - SuperMap、ArcGIS、QGIS 等 GIS 工具导入的数据
   - 遵循特定命名规范的企业数据（如大写列名）
   - 历史遗留系统的数据表

**不受影响的场景：**
3. **全小写列名的表**（PostgreSQL 默认风格）
4. **坐标系已是 3857 的表**（无需物化视图）
5. **通过 PostGIS 标准工具创建的表**

#### 预防措施

为避免类似问题，建议采取以下预防措施：

**1. 数据库设计规范**
   - ✅ **推荐**：使用全小写的列名（PostgreSQL 最佳实践）
   - ❌ **避免**：使用混合大小写，除非有特殊需求
   - ✅ **必须**：始终为空间表添加主键

**2. 代码规范**
   - PostgreSQL 动态 SQL 中的列名**必须**用双引号包裹
   - 查询元数据时使用 `attname` 获取原始列名（保留大小写）
   - 单元测试应覆盖大小写敏感场景

**3. 文档说明**
   - 在数据导入文档中说明列名大小写问题
   - 提供数据规范化工具或脚本（转换为小写）
   - 标注 GIS 数据迁移的注意事项

#### 修复日期

- **发现日期：** 2026-01-29
- **修复版本：** v0.0.23+
- **影响范围：** Manager 模块 MVT 预缓存功能（物化视图创建、空间索引、瓦片生成）
- **相关文件：** `manager/backend/internal/mvt/preparation_service.go`
- **关键修复：**
  - ✅ 动态查询主键列名（不再硬编码 `id`）
  - ✅ 支持大小写敏感的列名（主键和几何列都用双引号包裹）
  - ✅ 支持无主键表（使用 `row_number() OVER ()` 生成临时 ID）
  - ✅ 详细的主键诊断信息（便于故障排查）

---

## 数据库问题

（待补充）

---

## 网络问题

### 1. Workflow 引擎注册失败 502（系统代理拦截问题）

#### 问题现象

- Python/Spark Workflow 引擎启动后注册失败
- 引擎日志显示收到 502 Bad Gateway 响应
- System Backend 日志中没有任何注册请求记录
- curl 测试同一接口可以正常连接
- 引擎状态一直保持 `offline`

#### 问题根因

**系统 HTTP 代理拦截了 Python requests 库对 localhost 的请求**

技术细节：

1. **Python requests 库行为**
   - requests 和 urllib 会自动使用系统级 HTTP 代理设置
   - macOS 系统代理可通过 `networksetup -getwebproxy Wi-Fi` 查看
   - 常见代理工具：Clash、V2Ray、Charles 等

2. **代理服务器限制**
   - 多数代理服务器不处理对 `localhost` 或 `127.0.0.1` 的请求
   - 代理收到这类请求后直接返回 502 Bad Gateway
   - 响应头特征：只有 `Connection: close` 和 `Content-Length: 0`

3. **curl 能正常工作的原因**
   - curl 默认不使用系统代理（除非显式指定 `-x` 参数）
   - 所以 curl 可以直接连接到 localhost:8180

#### 关键证据

引擎日志显示：
```
📥 收到响应: status=502, body=
📋 响应头: {'Connection': 'close', 'Content-Length': '0'}
```

系统代理检查：
```bash
$ networksetup -getwebproxy Wi-Fi
Enabled: Yes
Server: 127.0.0.1
Port: 17890
```

手动测试对比：
```bash
# Python requests - 失败（走代理）
$ python -c "import requests; print(requests.get('http://localhost:8180/health').status_code)"
502

# curl - 成功（不走代理）
$ curl http://localhost:8180/health
{"status":"ok"}
```

#### 解决方案

**在 Python 引擎的注册函数中禁用代理**

修改文件：
- `engines/python-workflow/api_server.py:590-601`
- `engines/spark-workflow/api_server.py:382-393`

```python
def register_to_system():
    # ...

    # 禁用代理，直接连接到 System Backend（避免系统代理干扰）
    proxies = {
        'http': None,
        'https': None
    }

    response = requests.post(
        f"{system_url}/internal/engines/register",
        json=payload,
        headers=headers,
        proxies=proxies,  # ← 添加这个参数
        timeout=10
    )
```

#### 验证方法

1. **修改代码后重启引擎**
   ```bash
   bash scripts/dev/restart.sh
   ```

2. **查看引擎日志**
   ```bash
   tail -f logs/python-workflow-engine.log
   # 应该看到：✅ Successfully registered to System Backend (Engine ID: 65)
   ```

3. **检查数据库状态**
   ```sql
   SELECT id, name, engine_type, connection_status
   FROM system.engines
   WHERE engine_type IN ('python_workflow', 'spark_workflow');
   ```

   应该显示 `connection_status = 'online'`

#### 预防措施

1. **本地开发环境**
   - 关闭系统代理或配置代理规则排除 localhost
   - 推荐在代理工具中添加 `localhost` 到直连列表

2. **生产环境部署**
   - 确保容器内部通信不经过代理
   - 使用 `NO_PROXY` 环境变量排除内部服务

3. **通用 HTTP 客户端封装**（建议）
   ```python
   # 创建统一的内部服务调用客户端
   def create_internal_client():
       """创建用于内部服务调用的 HTTP 客户端（禁用代理）"""
       return requests.Session()
       session = requests.Session()
       session.proxies = {'http': None, 'https': None}
       return session
   ```

#### 相关问题

如果遇到其他 Python 服务无法连接 localhost 的问题，检查：

1. **是否启用了系统代理**
   ```bash
   # macOS
   networksetup -getwebproxy Wi-Fi

   # Linux
   echo $http_proxy
   echo $https_proxy
   ```

2. **Python 环境变量**
   ```bash
   env | grep -i proxy
   ```

3. **临时禁用代理测试**
   ```python
   import os
   os.environ['NO_PROXY'] = 'localhost,127.0.0.1'
   ```

#### 修复日期

- **发现日期：** 2026-01-03
- **修复版本：** v0.0.20+
- **影响范围：** Python/Spark Workflow 引擎自注册功能

---

## 性能问题

（待补充）

---

## 更新日志

| 日期 | 问题 | 修复人员 |
|------|------|---------|
| 2025-12-18 | Manager 数据预览"暂无数据"（双重 .data 访问） | Claude Code |
| 2026-01-03 | Workflow 引擎注册失败 502（系统代理拦截） | Claude Code |
| 2026-01-27 | Python Workflow Engine 依赖安装失败（NumPy 版本冲突） | Claude Code |
| 2026-01-27 | 工作流引擎注册失败 404（缺少 /api 前缀） | Claude Code |
| 2026-01-29 | MVT 物化视图创建失败（主键大小写问题） | Claude Code |

---

## 后端问题

### 3. Python Workflow Engine 依赖安装失败（NumPy 版本冲突）

#### 问题现象

运行 `bash scripts/dev/restart.sh -python-workflow` 时失败，错误信息：

```
Collecting numpy<2.0 (from -r requirements.txt (line 4))
  Using cached numpy-1.26.4.tar.gz (15.8 MB)
  Installing build dependencies ... done
  Getting requirements to build wheel ... done
  Preparing metadata (pyproject.toml) ... error
  error: subprocess-exited-with-error
```

#### 问题根因

**NumPy 1.x 与 Python 3.13 不兼容**

- 虚拟环境使用 Python 3.11，但当重新创建 venv 时可能使用 Python 3.13（如系统默认更新）
- numpy<2.0 在 Python 3.13 上没有预编译 wheel
- 系统缺少编译工具（C 编译器、BLAS/LAPACK 库等），导致从源码编译失败
- numpy<2.0 的旧版本在 PyPI 中已被官方移除，Python 3.13 无法获取预编译包

#### 技术细节

**NumPy 与 Python 版本兼容性矩阵：**

| NumPy | Python 3.11 | Python 3.12 | Python 3.13 |
|-------|------------|------------|------------|
| 1.26.x | ✅ | ✅ | ❌ |
| 2.0+ | ✅ | ✅ | ✅ |

**相关依赖的兼容性：**

- `geopandas==0.14.1` + `numpy>=2.0` → **不兼容**（NumPy 2.x ABI 更改）
- `shapely==2.0.2` + `numpy>=2.0` → **不兼容**
- `geopandas>=0.15.0` + `numpy>=2.0` → **兼容** ✅
- `shapely>=2.1.0` + `numpy>=2.0` → **兼容** ✅

#### 解决方案

**更新 `engines/python-workflow/requirements.txt`：**

1. **升级 NumPy**（从 `<2.0` 改为 `>=2.0`）
2. **升级 GeoPandas**（从 `0.14.1` 改为 `>=0.15.0`）
3. **升级 Shapely**（从 `2.0.2` 改为 `>=2.1.0`）
4. **新增 python-dotenv**（缺失的依赖）

**修改前：**
```txt
numpy<2.0
pandas>=2.0,<3.0
geopandas==0.14.1
shapely==2.0.2
```

**修改后：**
```txt
# Core spatial computation (NumPy 2.0+ for Python 3.11+ compatibility)
numpy>=2.0
pandas>=2.0,<3.0
geopandas>=0.15.0
shapely>=2.1.0

# ... 其他依赖

# Environment variable loading
python-dotenv>=1.0.0
```

#### 验证修复

```bash
cd engines/python-workflow

# 清除旧的虚拟环境（如需要）
rm -rf venv

# 重新创建并安装依赖
python3 -m venv venv
./venv/bin/pip install -r requirements.txt

# 验证核心依赖
./venv/bin/python -c "import flask, pandas, geopandas, numpy; \
  print(f'NumPy: {numpy.__version__}'); \
  print(f'GeoPandas: {geopandas.__version__}'); \
  print(f'✓ 所有依赖安装成功')"
```

#### 修复日期

- **发现日期：** 2026-01-27
- **修复版本：** v0.0.22+
- **影响范围：** Python Workflow Engine（所有 Python 3.11+ 环境）
- **验证命令：** `bash scripts/dev/restart.sh -python-workflow`

---

### 4. 工作流引擎注册失败 404（缺少 /api 前缀）

#### 问题现象

启动 Python/Math/Spark Workflow Engine 后，日志显示注册失败：

```
2026-01-27 16:13:57,196 - __main__ - INFO - 📤 发送注册请求到: http://localhost:8180/internal/engines/register
2026-01-27 16:13:57,196 - urllib3.connectionpool - DEBUG - http://localhost:8180 "POST /internal/engines/register HTTP/1.1" 404 18
2026-01-27 16:13:57,196 - __main__ - INFO - 📥 收到响应: status=404, body=404 page not found
2026-01-27 16:13:57,196 - __main__ - WARNING - ⚠️  Failed to register: 404 - 404 page not found
```

引擎会重试 5 次，全部失败后放弃注册。

#### 问题根因

**注册 API 路径缺少 `/api` 前缀**

- **错误路径**: `http://localhost:8180/internal/engines/register`
- **正确路径**: `http://localhost:8180/api/internal/engines/register`

System Backend 的路由定义在 `system/backend/internal/api/engine_handler.go`：

```go
// POST /api/internal/engines/register
func RegisterEngine(c *gin.Context) {
    // 引擎注册逻辑
}
```

所有内部 API 都必须带 `/api` 前缀，这是 System Backend 的统一路由规范。

#### 技术细节

**受影响的引擎：**

1. **Python Workflow Engine** (`engines/python-workflow/api_server.py`)
2. **Math Workflow Engine** (`engines/math-workflow/api_server.py`)
3. **Spark Workflow Engine** (`engines/spark-workflow/api_server.py`)

**注册逻辑位置：**
```python
# engines/*/api_server.py
def register_to_system():
    response = requests.post(
        f"{system_url}/internal/engines/register",  # ❌ 错误
        json=payload,
        headers=headers
    )
```

**为什么引擎仍能正常运行：**
- 注册失败不影响引擎的 API 服务（仍监听在 8099/8097/8098 端口）
- 但 System 无法发现引擎，Develop 和 Orchestrator 模块无法调用
- 用户在前端看不到可用的计算引擎

#### 解决方案

**修复所有引擎的注册 URL：**

```python
# 修改前
response = requests.post(
    f"{system_url}/internal/engines/register",
    # ...
)

# 修改后
response = requests.post(
    f"{system_url}/api/internal/engines/register",
    # ...
)
```

**修复的文件：**
- [engines/python-workflow/api_server.py](../engines/python-workflow/api_server.py)（第 593、604 行）
- [engines/math-workflow/api_server.py](../engines/math-workflow/api_server.py)（第 354 行）
- [engines/spark-workflow/api_server.py](../engines/spark-workflow/api_server.py)（第 395 行）

#### 验证修复

```bash
# 重启工作流引擎
bash scripts/dev/restart.sh -python-workflow

# 检查注册日志（应该看到 202 成功状态）
tail -f logs/python-workflow-engine-stderr.log

# 预期输出：
# 📤 发送注册请求到: http://localhost:8180/api/internal/engines/register
# 📥 收到响应: status=202, body=...
# ✅ Successfully registered to System Backend (Engine ID: xxx)
```

**在 System 后端验证：**
```bash
# 查询已注册的引擎列表
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8180/api/engines

# 应该能看到 python_workflow、math_workflow、spark_workflow
```

#### 修复日期

- **发现日期：** 2026-01-27
- **修复版本：** v0.0.22+
- **影响范围：** 所有工作流引擎（Python、Math、Spark）
- **根本原因：** API 路由规范不一致
- **验证命令：** `bash scripts/dev/restart.sh && tail -f logs/*-workflow-engine-stderr.log`

---

## PostgreSQL 字段名大小写问题全面修复（2026-01-30）

### 问题描述

PostgreSQL 中不带引号的标识符会自动转换为小写，导致混合大小写的字段名（如 SuperMap 的 `SmID`、`SmGeometry`，ArcGIS 的 `OBJECTID`、`SHAPE`）在 SQL 查询中失败。

### 症状

1. **MVT 物化视图创建失败**：
   ```
   ERROR: column "smgeometry" does not exist
   ```

2. **OGC WFS 查询失败**：
   ```
   ERROR: column "shape" does not exist
   ```

3. **数据预览正常，但 MVT 瓦片生成失败**

### 根本原因

动态 SQL 构建时未使用双引号包裹标识符，导致 PostgreSQL 自动将标识符转换为小写。

**错误示例**：
```go
// ❌ 错误：SmGeometry 会被转换为 smgeometry
query := fmt.Sprintf("SELECT %s FROM %s.%s", geomColumn, schema, table)
```

**正确示例**：
```go
// ✅ 正确：使用双引号保留大小写
query := fmt.Sprintf(`SELECT "%s" FROM "%s"."%s"`, geomColumn, schema, table)
```

### 修复内容

#### 已修复的文件（共 7 处）

**Manager 模块（5 处）**：

1. [manager/backend/internal/mvt/preparation_service.go:596](../manager/backend/internal/mvt/preparation_service.go#L596)
   - 物化视图创建：`FROM %s.%s` → `FROM "%s"."%s"`

2. [manager/backend/internal/mvt/preparation_service.go:632](../manager/backend/internal/mvt/preparation_service.go#L632)
   - 索引创建：添加双引号到索引名、表名、列名

3. [manager/backend/internal/mvt/preparation_service.go:658](../manager/backend/internal/mvt/preparation_service.go#L658)
   - ANALYZE 语句：`ANALYZE %s.%s` → `ANALYZE "%s"."%s"`

4. [manager/backend/internal/mvt/quick_view_service.go:1164](../manager/backend/internal/mvt/quick_view_service.go#L1164)
   - Quick View 物化视图创建

5. [manager/backend/internal/mvt/quick_view_service.go:1271](../manager/backend/internal/mvt/quick_view_service.go#L1271)
   - ST_Extent 查询

**Service 模块（2 处）**：

6. [service/backend/internal/ogc/common/feature_query.go:58](../service/backend/internal/ogc/common/feature_query.go#L58)
   - WFS 要素查询：几何列转换

7. [service/backend/internal/ogc/common/feature_query.go:78](../service/backend/internal/ogc/common/feature_query.go#L78)
   - WFS 空间过滤：几何列引用

#### 已确认正确的模块

以下模块无需修复，已正确处理标识符：

- ✅ **Common 模块** - [common/spatial/query.go](../common/spatial/query.go)
- ✅ **Transfer 模块** - 完善的 `quoteIdentifier()` 机制
- ✅ **Meta 模块** - 所有查询都正确使用双引号
- ✅ **Develop 模块** - 直接执行用户 SQL
- ✅ **Orchestrator 模块** - 无动态 SQL 构建
- ✅ **System 模块** - 无动态 SQL 构建
- ✅ **Labs 模块** - 已正确处理

### 长期解决方案：统一 SQL 能力边界

为防止未来出现类似问题，SQL 拼装能力按职责收口：

- 跨 SQL 引擎的标识符引用、命名空间表名拼接、基础 SELECT / COUNT / 分页，统一使用 `common/sqldialect`。
- PostGIS 空间表达式、MVT、物化视图、GIST 索引等空间能力，统一使用 `common/spatial`。
- 不再保留独立的 PostgreSQL 倾向 SQL 构建包，避免与 `sqldialect` / `spatial` 职责重叠。

### 验证步骤

#### 1. 重启相关服务

```bash
# 重启 Manager 模块
bash scripts/dev/restart.sh -manager

# 重启 Service 模块
bash scripts/dev/restart.sh -service
```

#### 2. 测试混合大小写字段

创建测试表：
```sql
CREATE TABLE public.test_mixed_case (
    "SmID" SERIAL PRIMARY KEY,
    "SmGeometry" geometry(Point, 4326),
    "DataName" VARCHAR(100)
);

INSERT INTO public.test_mixed_case ("SmGeometry", "DataName")
VALUES (ST_GeomFromText('POINT(120.0 30.0)', 4326), 'Test Data');
```

测试流程：
1. 在 Manager 数据浏览器中浏览该表 ✅
2. 验证数据预览正常显示 ✅
3. 点击"准备预缓存"，验证物化视图创建成功 ✅
4. 验证 MVT 瓦片可以正常生成 ✅
5. 在 Service 模块中发布 WFS 服务 ✅
6. 验证 WFS GetFeature 请求正常 ✅

### 最佳实践

#### 1. 始终使用双引号

```go
// ❌ 避免
query := fmt.Sprintf("SELECT %s FROM %s.%s", col, schema, table)

// ✅ 推荐
query := fmt.Sprintf(`SELECT "%s" FROM "%s"."%s"`, col, schema, table)
```

#### 2. 使用 sqldialect / spatial 工具

```go
// ✅ 通用表查询：使用跨引擎方言工具
import "github.com/addp/common/sqldialect"

dialect := sqldialect.ForEngine(engineType)
query := dialect.SelectTableSQL(
    dialect.QuoteIdentifier(col),
    schema,
    table,
    "", "", 0, 0,
)
```

#### 3. WHERE 条件中的列名

```go
// ❌ 错误
whereClause := fmt.Sprintf("WHERE %s > 100", col)

// ✅ 正确：按引擎方言引用标识符
dialect := sqldialect.ForEngine(engineType)
whereClause := fmt.Sprintf("%s > 100", dialect.QuoteIdentifier(col))
```

### 影响范围

| 模块 | 修复文件数 | 影响功能 | 风险等级 |
|------|-----------|---------|----------|
| Manager | 2 | MVT 预缓存、物化视图创建 | 🔴 高 |
| Service | 1 | OGC WFS 要素查询 | 🟠 中 |
| Transfer | 0 | 已正确实现 | ✅ 无 |
| Meta | 0 | 已正确实现 | ✅ 无 |
| Common | 0 | 已正确实现 | ✅ 无 |

### 相关文档

- [字段名大小写梳理计划](./字段名的大小写梳理.md) - 完整的排查和修复计划
- [common/sqldialect](../common/sqldialect/) - 跨 SQL 引擎基础方言工具
- [common/spatial](../common/spatial/) - PostGIS 空间 SQL 表达式和空间能力
- [PostgreSQL 标识符文档](https://www.postgresql.org/docs/current/sql-syntax-lexical.html#SQL-SYNTAX-IDENTIFIERS)

### 修复日期

- **发现日期：** 2026-01-30
- **修复版本：** v0.0.24+
- **影响范围：** Manager MVT、Service OGC
- **根本原因：** 动态 SQL 构建未使用双引号包裹标识符
- **长期方案：** 按职责收口到 `common/sqldialect` 和 `common/spatial`

### 经验教训

1. **Transfer 模块的最佳实践值得借鉴**：已实现统一的 `quoteIdentifier()` 方法
2. **Meta 模块的正确性**：元数据扫描和空间元数据处理完全正确
3. **边界清晰是关键**：通用 SQL 方言与 PostGIS 空间表达式分包收口，可以防止未来重复出现类似问题
4. **测试覆盖很重要**：需要针对混合大小写字段的测试用例
5. **文档化经验**：及时记录问题和解决方案，避免重复踩坑
