# ADDP 常见故障排查指南

本文档记录 ADDP 平台开发和使用过程中遇到的常见问题及解决方案。

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

---

## 后端问题

（待补充）

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
