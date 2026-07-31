# ADDP 常见故障排查指南

本文档记录 ADDP 平台开发和使用过程中遇到的常见问题及解决方案。

---

## 脚本/启动问题

### 1. Codex 等托管命令环境中 restart.sh 退出后服务立刻不可用

#### 问题现象

在 Codex 等托管命令执行环境中运行：
```bash
./scripts/dev/restart.sh -orchestrator
```

脚本输出中健康检查通过，但命令结束后端口立刻不可用：
```bash
curl http://localhost:8084/health
# 无响应或 HTTP 000
```

`logs/*-backend.log` 中能看到服务曾经成功启动并响应过 `/health`。

#### 根本原因

`scripts/dev/start.sh` 和 `scripts/dev/restart.sh` 会以后台进程方式启动开发服务。在普通终端中，脚本退出后后台服务会继续运行。

Codex 等托管命令执行环境可能会在一次性命令结束时回收其派生的后台进程树。此时服务不是自行崩溃，而是随命令会话结束被执行环境清理，所以会出现“脚本内 health 通过，脚本退出后端口不可用”的现象。

#### 解决方案

在 Codex 等托管命令环境中，使用前台保活入口：
```bash
bash scripts/dev/keepalive.sh restart -orchestrator
```

常用示例：
```bash
# 重启 Go 模块并保持服务可用；会继承 restart.sh 的全局重启语义
bash scripts/dev/keepalive.sh restart -orchestrator

# 局部重启扩展服务并保持服务可用；不会停止整套 ADDP 开发环境
bash scripts/dev/keepalive.sh restart -python-workflow

# 全量重启并保持服务可用
bash scripts/dev/keepalive.sh restart -all

# 启动单个模块并保持服务可用
bash scripts/dev/keepalive.sh start -system
```

`keepalive.sh` 会调用现有 `start.sh` / `restart.sh`，然后以前台阻塞进程承载服务生命周期。按 `Ctrl+C` 或终止命令时，会自动调用 `scripts/dev/stop.sh` 清理开发服务。

#### 使用边界

- 普通本地终端继续优先使用 `bash scripts/dev/start.sh` 或 `bash scripts/dev/restart.sh`。
- Codex 等命令结束后会回收后台进程的托管环境，使用 `bash scripts/dev/keepalive.sh ...`。
- `keepalive.sh restart -<Go模块名>` 会继承 `restart.sh` 的全局停止语义：先停止整套 ADDP 开发环境，再启动指定模块及其依赖。它适合“只需要该模块继续可用”的场景，不适合在用户外部终端已经启动全套服务时由 Codex 接管局部重启。
- `keepalive.sh restart -python-workflow|-math-workflow|-spark-workflow|-jupyter|-copilot|-agent` 会继承扩展服务局部重启语义，只重启对应服务。
- 如果需要保持全套服务可用，在 Codex 中使用 `bash scripts/dev/keepalive.sh restart -all` 并让该命令持续前台运行；如果只想做一次性验证，运行测试和构建命令即可，不要为了局部 Go 后端改动在 Codex 中执行 `restart -<Go模块名>`。
- 不要同时在外部终端和 Codex 中并发执行 `restart.sh` / `stop.sh`，因为脚本会按 PID、进程名和端口清理 ADDP 开发服务，两个会话可能互相清理。

---

### 2. restart.sh 误报端口被浏览器占用（端口检查假阳性）

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

### 3. restart.sh 报告 ADDP 旧进程未清理（launchd KeepAlive 自动拉起）

#### 问题现象

`restart.sh` 已调用 `stop.sh`，但启动阶段仍报告端口被 `.dev-bins/addp-*` 占用。对应 PID 文件不存在，杀死进程后又会出现新的 PID。

#### 根本原因

服务曾通过 `launchctl submit` 注册为 `com.addp.codex.*` KeepAlive 作业。`stop.sh` 只杀死进程时，`launchd` 会立即重新拉起服务，导致端口再次被占用。

#### 处理方式

`stop.sh` 会在 macOS 上先枚举 `com.addp.codex.*` 作业，只卸载作业命令中包含当前仓库绝对路径的条目，然后执行常规 PID、进程名和端口清理。卸载失败会使 `stop.sh` 返回非零状态，并中断 `restart.sh`。

旧版脚本可先手动卸载作业：

```bash
launchctl bootout gui/$(id -u)/com.addp.codex.meta
bash scripts/dev/stop.sh
```

---

## CLI 问题

### 1. macOS 询问 Python 访问 `addp-cli` 钥匙串密码

#### 问题现象

执行 `addp auth login`、`addp auth status` 或需要刷新登录会话的 Tool 命令时，macOS 显示系统弹窗：

```text
Python 想要访问你的钥匙串中的密码“addp-cli”。
若要给予许可，请输入“登录”钥匙串的密码。
```

选择“拒绝”或未通过系统验证后，CLI 返回：

```json
{"error":{"code":"authentication_unavailable","message":"无法访问操作系统凭据存储"}}
```

登录阶段可能使用 `authentication_failed`，但消息相同。

#### 根本原因

正式 `addp` CLI 由 Python wheel 安装并通过 macOS Keychain 保存 Refresh Token。macOS 会按请求访问钥匙串条目的应用身份实施访问控制；首次访问已有条目，或 Python、pipx 环境、可执行文件身份发生变化后，系统可能要求当前用户再次确认。弹窗中的请求方因此显示为 `Python`。

这是 macOS 对“登录”钥匙串的本地授权，不是 ADDP OAuth 登录、ADDP 账号密码或 System 服务故障。OAuth 主路径和凭据存储边界不变：Refresh Token 只进入 OS Keychain，Access Token 只存在于当前进程内存。

#### 正确处理

1. 确认弹窗来自 macOS，访问的条目名是 `addp-cli`，当前执行的命令确实是已安装的 `addp` CLI。
2. 只在该系统弹窗中输入当前 macOS 用户的“登录”钥匙串密码，通常就是系统登录密码；不要把密码输入终端或浏览器授权页。
3. 正常首次使用选择“允许”完成单次授权。项目不默认建议对通用 `Python` 进程选择“始终允许”。
4. 重新执行状态检查：

   ```bash
   addp --version
   addp auth status
   ```

5. 如果状态检查正常执行但返回 `authenticated:false`，说明此前登录未能保存 Refresh Token，重新执行：

   ```bash
   addp auth login
   addp auth status
   ```

6. 完成临时验收后需要撤销会话时，使用正式退出命令：

   ```bash
   addp auth logout
   ```

#### 禁止的处理方式

- 不使用 `security find-generic-password -w` 等命令把 Refresh Token 输出到终端。
- 不把 Refresh Token 复制到文件、环境变量、命令参数或日志。
- 不删除整个“登录”钥匙串，也不把删除 `addp-cli` 条目作为常规授权处理。
- 不配置明文文件 Keyring 降级，不新增手工 Token、密码、Client Secret 或 API Key 登录路径。

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

**当前规则：Manager 前端 API client 默认返回已提取后的响应体**

当前 Manager 公共入口统一走 `/api/v1/manager/...`：

- 数据预览：`GET /api/v1/manager/preview`
- 引擎列表：`GET /api/v1/manager/engines`
- 资源树事实：`GET /api/v1/meta/resource-tree/{engine_id}`

`manager/frontend/src/api/client.js` 使用 `createAPIClient()` 默认 `extractData=true`。因此业务调用拿到的是响应体对象，不是 Axios response。写调用代码时应以 API helper 的返回类型为准，不要习惯性再访问一层 `.data`。

**修改：数据预览**

文件：`manager/frontend/src/stores/explorer.js` 或调用 `dataExplorerAPI.getPreview()` 的位置：

```diff
const response = await dataExplorerAPI.getPreview(locator, page, pageSize)
- previewData.value = normalizePreviewPayload(response.data, selectedNode.value)
+ previewData.value = normalizePreviewPayload(response, selectedNode.value)
```

如果某个 helper 显式关闭 `extractData`，才按 Axios response 处理；否则 Manager 前端默认直接消费返回体。

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
   - 公共 API 响应遵守 `docs/spec/addp-API设计规范.md`
   - 前端 helper 层负责消化统一响应包装，组件和 store 不直接猜测 wrapper 结构

2. **代码审查检查清单**
   - 检查 API helper 是否使用默认 `extractData=true`
   - 前端调用时根据 helper 返回值决定是否使用 `.data`
   - 搜索 `response.data.data` 和对已提取响应再次 `.data` 的访问

3. **使用 TypeScript（推荐）**
   ```typescript
   // 类型定义明确返回值
   function getPreview(params): Promise<TablePreview>  // 直接返回数据
   function getEngines(): Promise<Engine[]>  // helper 已提取响应体
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

### 1. Transfer 写出 Shapefile 后资源树看不到 `.prj`

#### 问题现象

- Transfer 空间表导出到对象存储 Shapefile 后，执行记录显示任务成功。
- 对象存储目标实际包含 `.shp/.shx/.dbf/.prj/.cpg` 等组件，或 `common.task_executions.metadata.target_refs` 已包含 `.prj`。
- 但 Manager / Meta 资源树或 item 属性中 `attributes.item.refs` 缺少 `.prj`，`format_info.shapefile.has_prj=false`。
- Meta worker 日志可能出现类似 `bucket=gis path=a2.shp`、`The specified bucket does not exist` 的错误。

#### 根本原因

对象存储存在两种路径语义：

1. 外部 content path / `full_name` / `ref_groups.path`：必须是 `bucket/object_key`，例如 `addp/gis/a2.prj`。
2. 对象存储内部 object key：不含 bucket，例如 `gis/a2.prj`。

Transfer 写 Shapefile sidecar 时可以在 executor 内部使用 bucket 内 object key，再由 content adapter 映射到正确 bucket；但写入 execution metadata 或提交 Meta `ref_groups` 时，必须使用外部完整 content path。如果把 `gis/a2.prj` 直接提交给 Meta，Meta 会按规范把 `gis` 拆成 bucket，把 `a2.prj` 拆成 object key，最终扫描错误 bucket，资源树就看不到 `.prj`。

另一类常见原因是字段映射 transform 只复制了几何列类型和 SRID，没有保留 `SpatialInfo.CRSRef/CRSDefinitions`。这种情况下 Shapefile writer 没有 CRS WKT 文本，也不会生成 `.prj`。

#### 排查顺序

1. 查 Transfer execution：
   ```sql
   select id, execution_id, status, metadata->'target_refs'
   from common.task_executions
   where module='transfer' and source_task_id='<task_id>'
   order by id desc limit 5;
   ```
   对象存储 Shapefile 的 refs 应为 `bucket/object_key`，例如 `addp/gis/a2.prj`，不能是 `gis/a2.prj`。

2. 查 Meta item：
   ```sql
   select id, full_name,
          attributes->'item'->'refs' as refs,
          attributes->'format_info'->'shapefile' as shapefile_info
   from meta.meta_item
   where engine_id=<engine_id> and full_name='<bucket/object.shp>' and deleted_at is null;
   ```

3. 查日志：
   - `logs/transfer-bounded-worker.log`：确认 `target_refs` 是否包含 `.prj`。
   - `logs/meta-worker.log`：如果看到把 object key 第一段当 bucket 的错误，优先检查 Transfer 提交的 `ref_groups.path`。

4. 不要用 MinIO 容器内 `/data/...` 文件系统形态判断对象是否存在。MinIO 后端存储布局可能把对象表现为目录；应通过项目 content reader、MinIO API 或 Meta/Transfer 执行元数据确认。

#### 修复原则

- Transfer executor 对外上报的 `TargetRefs` 必须是完整 content path；对象存储目标必须带 bucket。
- Transfer 写后 Meta 扫描只提交本次实际生成的 refs，不补不存在的 optional sidecar。
- 字段映射 transform 必须保留空间 CRS 事实，尤其是 primary geometry column 的 `CRSRef` 和 `SpatialInfo.CRSDefinitions`。
- Meta 不兼容 bucketless 对象存储 `ref_groups.path`；错误路径应在 Transfer 边界修正。

#### 验证命令

```bash
cd transfer/backend && go test ./internal/executor ./internal/service
cd meta/backend && go test ./internal/scanflow ./internal/scanruntime
```

运行时验证重新执行任务后，应同时满足：

- `common.task_executions.metadata->'target_refs'` 包含 `bucket/.../*.prj`。
- `meta.meta_item.attributes.item.refs` 包含同一个 `.prj`。
- `meta.meta_item.attributes.format_info.shapefile.has_prj=true`。

### 2. 历史问题归档：MVT 物化视图准备失败 - column "id" does not exist

#### 问题现象

历史实现曾在瓦片缓存生成任务中隐式准备 3857 物化视图。遇到混合大小写主键或几何列时，可能报错：

```text
物化视图创建失败: ERROR: column "id" does not exist (SQLSTATE 42703)
```

#### 当前状态

该旧准备路径已经删除。瓦片缓存生成任务不再隐式创建物化视图、空间索引或执行准备检查；3857 矢量物化视图目标只能由 `vector_materialized_view_generation` 任务显式创建和刷新。

当前排查方式：

1. 在空间预览页看到源表转换慢路径、动态 MVT 超时或优化建议时，先进入“矢量物化视图”。
2. 创建并执行 `vector_materialized_view_generation` 任务。
3. 确认 `manager.vector_materialized_view.status=ready` 后，再生成瓦片缓存。
4. 若仍有大小写字段相关错误，检查 `manager/backend/internal/service/vector_materialized_view_task_service.go` 和 `common/spatial` 是否使用 PostGIS 标识符引用函数。

#### 验证命令

```bash
cd manager/backend && go test -count=1 ./internal/service ./internal/mvt ./internal/mvtbenchmark
cd manager/frontend && npm run build
```

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
        f"{system_url}/api/v1/internal/engines/register",
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
   WHERE engine_type IN ('geopython_workflow', 'spark_workflow');
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
| 2026-01-27 | GeoPython Workflow Engine 依赖安装失败（NumPy 版本冲突） | Claude Code |
| 2026-01-27 | 工作流引擎注册失败 404（缺少 /api 前缀） | Claude Code |
| 2026-01-29 | MVT 物化视图创建失败（主键大小写问题） | Claude Code |

---

## 后端问题

### 3. GeoPython Workflow Engine 依赖安装失败（NumPy 版本冲突）

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
./venv/bin/python -m pip install -r requirements.txt

# 验证核心依赖
./venv/bin/python -c "import flask, pandas, geopandas, numpy, pyarrow, pyproj; \
  print(f'NumPy: {numpy.__version__}'); \
  print(f'GeoPandas: {geopandas.__version__}'); \
  print(f'PyArrow: {pyarrow.__version__}'); \
  print(f'✓ 所有依赖安装成功')"
```

#### 修复日期

- **发现日期：** 2026-01-27
- **修复版本：** v0.0.22+
- **影响范围：** GeoPython Workflow Engine（所有 Python 3.11+ 环境）
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

**注册 API 路径缺少 `/api/v1` 前缀**

- **错误路径**: `http://localhost:8180/internal/engines/register`
- **正确路径**: `http://localhost:8180/api/v1/internal/engines/register`

System Backend 的路由定义在 `system/backend/internal/api/engine_handler.go`：

```go
// POST /api/v1/internal/engines/register
func RegisterEngine(c *gin.Context) {
    // 引擎注册逻辑
}
```

所有内部 API 都必须带 `/api/v1/internal` 前缀，这是 System Backend 的统一路由规范。

#### 技术细节

**受影响的引擎：**

1. **GeoPython Workflow Engine** (`engines/python-workflow/api_server.py`)
2. **Spark Workflow Engine** (`engines/spark-workflow/api_server.py`)

**说明**：Math Workflow 现在是扩展引擎规范参考实现。开发环境可自动启动 Math Workflow 服务，但它不会自动注册到 System；需要使用时，应在 System 引擎管理中按扩展引擎手动注册。

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
- 注册失败不影响引擎的 API 服务（仍监听在 8099/8089/8098 端口）
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
    f"{system_url}/api/v1/internal/engines/register",
    # ...
)
```

**修复的文件：**
- [engines/python-workflow/api_server.py](../../engines/python-workflow/api_server.py)（第 593、604 行）
- [engines/spark-workflow/api_server.py](../../engines/spark-workflow/api_server.py)（第 395 行）

#### 验证修复

```bash
# 重启工作流引擎
bash scripts/dev/restart.sh -python-workflow

# 检查注册日志（应该看到 202 成功状态）
tail -f logs/python-workflow-engine-stderr.log

# 预期输出：
# 📤 发送注册请求到: http://localhost:8180/api/v1/internal/engines/register
# 📥 收到响应: status=202, body=...
# ✅ Successfully registered to System Backend (Engine ID: xxx)
```

**在 System 后端验证：**
```bash
# 查询已注册的引擎列表
curl -H "Authorization: Bearer YOUR_TOKEN" \
  http://localhost:8180/api/v1/system/engines

# 应该能看到已启动并成功注册的 geopython_workflow、spark_workflow
# math_workflow 需要通过 System 引擎管理手动注册后才会出现
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

#### 已修复的文件

**Manager 模块**：

1. [manager/backend/internal/service/vector_materialized_view_task_service.go](../../manager/backend/internal/service/vector_materialized_view_task_service.go)
   - 矢量物化视图目标创建：`CREATE MATERIALIZED VIEW`、`ST_Transform`、GiST 索引、`ANALYZE` 均使用 `common/spatial` 的 PostGIS 标识符引用函数。

2. [manager/backend/internal/mvt/quick_view_service.go](../../manager/backend/internal/mvt/quick_view_service.go)
   - 动态 MVT 只消费调用方解析后的目标，不再隐式准备 3857 派生对象。

3. [common/spatial/mvt.go](../../common/spatial/mvt.go)
   - MVT 查询通过统一 SQL 构建函数引用 schema、table、几何列和属性列。

**Service 模块**：

4. [service/backend/internal/api/ogc_features_handler.go](../../service/backend/internal/api/ogc_features_handler.go)
   - OGC API Features 请求入口：collection items、bbox 参数和单要素查询

5. [service/backend/internal/service/query_executor_service.go](../../service/backend/internal/service/query_executor_service.go)
   - Service 查询执行与 GeoJSON FeatureCollection 组装

#### 已确认正确的模块

以下模块无需修复，已正确处理标识符：

- ✅ **Common 模块** - [common/spatial/query.go](../../common/spatial/query.go)
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
3. 创建并执行矢量物化视图任务，验证矢量物化视图目标创建成功 ✅
4. 创建并执行瓦片缓存生成任务，验证 MVT 瓦片可以正常生成 ✅
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
| Manager | 3 | 矢量物化视图目标创建、动态 MVT、瓦片缓存生成 | 🔴 高 |
| Service | 1 | OGC WFS 要素查询 | 🟠 中 |
| Transfer | 0 | 已正确实现 | ✅ 无 |
| Meta | 0 | 已正确实现 | ✅ 无 |
| Common | 0 | 已正确实现 | ✅ 无 |

### 相关文档

- [数据库大小写处理规范](../spec/addp数据库大小写处理规范.md) - 完整的排查和修复计划
- [common/sqldialect](../../common/sqldialect/) - 跨 SQL 引擎基础方言工具
- [common/spatial](../../common/spatial/) - PostGIS 空间 SQL 表达式和空间能力
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

---

## Workflow Engine Python 测试包名冲突

### 现象

在同一个 `pytest` 进程中同时运行多个 workflow engine 测试时，例如同时运行 `engines/python-workflow` 和 `engines/math-workflow` 的测试，可能出现类似错误：

```text
ImportError: cannot import name 'get_operator_function' from 'operators'
```

### 根因

多个 Python workflow engine 都在各自目录下使用顶层包名 `operators`。同一个 Python 进程先导入某个引擎的 `operators` 后，后续测试可能复用 `sys.modules["operators"]`，导致另一个引擎的 `api_server.py` 导入到错误实现。

### 正确验证方式

按引擎目录隔离运行测试：

```bash
cd engines/python-workflow && PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 pytest test_operator_metadata.py
cd engines/math-workflow && PYTEST_DISABLE_PLUGIN_AUTOLOAD=1 pytest tests/test_api.py
```

不要在同一个 pytest 命令中混跑多个 workflow engine 的 Python 测试，除非先重构包名或测试导入隔离策略。

---

## 点云 COPC 快显生成失败

### 现象

LAS / LAZ / E57 / PCD / XYZ 数据项在 Manager 数据探查中点击“生成 COPC 快显”后失败，Manager 日志中可能出现：

```text
direct workflow operator laz_to_copc is unavailable
```

PointCloud Workflow 日志中可能出现：

```text
skip pointcloud_workflow registration because PDAL is not bound
```

### 根因

点云 COPC 转换依赖 `pointcloud_workflow` 运行时内置的 PDAL。开发环境不要求、也不应要求宿主机全局安装 PDAL；如果 `pointcloud-workflow` 被按宿主机 Python 服务启动，默认找不到 engine runtime 内部 PDAL，`/health` 会返回 `degraded`，并跳过向 System 自注册。此时 Manager 无法通过 `WorkflowRuntimeProvider` 找到 `las_to_copc` / `laz_to_copc` / `e57_to_copc` / `pcd_to_copc` / `xyz_to_copc` direct operator。

### 正确处理

使用 Docker runtime 启动或重启点云引擎：

```bash
bash scripts/dev/restart.sh -pointcloud-workflow
curl http://localhost:8102/health
```

健康检查应包含：

```json
{
  "status": "healthy",
  "conversion_ready": true,
  "dependencies": {
    "pdal": {
      "path": "/opt/conda/bin/pdal",
      "available": true
    }
  }
}
```

System 中 `pointcloud_workflow` 应为 `online`，且连接信息应指向宿主机可访问的 `localhost:8102`。如果 Manager 与 infra MinIO 运行在宿主机开发态，点云容器会将 `localhost:19000` 这类对象存储 endpoint 改写为 `host.docker.internal:19000`，生产或 Compose 网络中的非 localhost endpoint 不受影响。点云 COPC 写入不是纯流式写；当前 PDAL 2.10.2 实测 `writers.copc` 不能可靠直接写 `/vsis3/` 目标，因此运行时会先写 `POINTCLOUD_WORK_DIR` 下的受控工作文件，再上传到 Manager infra MinIO。大点云转换必须给 `POINTCLOUD_WORK_HOST_PATH` / `POINTCLOUD_WORK_DIR` / `CPL_TMPDIR` 配置容量足够的工作目录。
