# Graph 模块阶段4：图算法分析 — 详细设计规划

## Context

Graph 模块已完成阶段1（本体建模）和阶段3（知识探索），具备 G6 图浏览器和基础 Neo4j 查询能力。阶段4目标是在现有图浏览器基础上，叠加图算法分析能力，让用户能够发现知识图谱中的关键节点、社区结构和拓扑特征，提升图谱的洞察价值。

---

## 实施范围

### 功能清单

| 功能 | 算法类型 | 可用性 |
|------|----------|--------|
| 度中心性分析 | Cypher | 始终可用 |
| K跳邻居分析 | Cypher | 始终可用 |
| 多路最短路径 | Cypher | 始终可用 |
| PageRank | GDS | GDS 可用时 |
| Louvain 社区发现 | GDS | GDS 可用时 |
| 弱连通分量 (WCC) | GDS | GDS 可用时 |
| 介数中心性 | GDS | GDS 可用时 |

---

## 关键设计决策

| 决策点 | 选择 | 理由 |
|--------|------|------|
| GDS/Cypher 共同 API | 单一端点 `/analysis/run` | strategy 模式在 service 内路由，前端调用简洁 |
| GDS 投影生命周期 | 请求内创建-执行-defer 清理 | 避免 Neo4j 内存积累，异常路径安全 |
| 颜色计算位置 | **前端 JS 计算** | 分数归一化更灵活，减少传输量，后端只返回原始分数 |
| 结果着色方式 | `updateItem` 实时更新 | 不触发重新布局，保持用户视角 |
| 右侧面板集成 | 共用 detail-panel，Tab 切换 | 保持三栏布局，画布宽度不压缩 |
| GDS 不可用展示 | disabled 而非隐藏 | 让用户感知功能存在，了解升级路径 |

---

## 新增/修改文件

### 后端

```
graph/backend/internal/
├── models/analysis.go          # [新增] 所有算法 DTO
├── service/analysis_service.go # [新增] 算法核心服务
├── api/analysis_handler.go     # [新增] HTTP Handler
├── api/router.go               # [修改] 注册分析路由
└── cmd/server/main.go          # [修改] 初始化 AnalysisService/Handler
```

### 前端

```
graph/frontend/src/
├── api/analysis.js              # [新增] API 调用封装
├── components/AnalysisPanel.vue # [新增] 分析面板主组件
├── components/GraphCanvas.vue   # [修改] 新增 applyScoreColors/resetNodeColors
└── views/GraphBrowser.vue       # [修改] 集成 AnalysisPanel
```

---

## DTO 设计（models/analysis.go）

> **规范要求**：字段名统一使用 snake_case（通过 `json:"..."` tag 实现）

```go
type AlgorithmCapabilities struct {
    GDSAvailable bool     `json:"gds_available"`
    GDSVersion   string   `json:"gds_version,omitempty"`
    CypherAlgos  []string `json:"cypher_algos"`  // 始终返回
    GDSAlgos     []string `json:"gds_algos"`     // 不可用时返回空列表
}

type AlgorithmRunRequest struct {
    Algorithm  string                 `json:"algorithm"   binding:"required"`
    Params     map[string]interface{} `json:"params"`
    NodeLabels []string               `json:"node_labels"` // GDS 投影过滤（空=全部）
    RelTypes   []string               `json:"rel_types"`   // GDS 投影过滤（空=全部）
    Limit      int                    `json:"limit"`       // Top-N，默认50，最大200
}

type NodeScore struct {
    NodeID      string  `json:"node_id"`
    DisplayName string  `json:"display_name"`
    EntityType  string  `json:"entity_type"`
    Score       float64 `json:"score"`
    Rank        int     `json:"rank"`         // 1-based
    CommunityID int64   `json:"community_id"` // 仅社区算法填充
}

type AlgorithmResult struct {
    Algorithm     string                 `json:"algorithm"`
    AlgorithmName string                 `json:"algorithm_name"` // 中文展示名
    NodeScores    []NodeScore            `json:"node_scores"`    // 中心性/排名算法
    Subgraph      *SubgraphResult        `json:"subgraph,omitempty"` // 路径/邻居算法
    Metadata      map[string]interface{} `json:"metadata"`       // 耗时/社区数等
    Warning       string                 `json:"warning,omitempty"`
}
```

---

## API 端点设计

> **规范遵从**：HTTP 状态码优先，直接返回数据对象（不包 code/message/data 三层），错误用 `{"error": "..."}` 格式

路由挂载于现有 `graphs.Group("/:id")` 下：

```
GET  /api/v1/graph/graphs/:id/analysis/capabilities  → AlgorithmCapabilities（HTTP 200）
POST /api/v1/graph/graphs/:id/analysis/run           → AlgorithmResult（HTTP 200）
```

**POST run 请求示例**：
```json
{
  "algorithm": "pagerank",
  "node_labels": ["Person", "Organization"],
  "rel_types": [],
  "limit": 50
}
```

**错误响应**（遵循规范）：

| 场景 | HTTP 状态 | 响应体 |
|------|-----------|--------|
| algorithm 未知或参数缺失 | 400 | `{"error": "未知算法：xxx"}` |
| 请求 GDS 算法但 GDS 不可用 | 400 | `{"error": "该算法需要 Neo4j GDS 插件，当前实例未安装"}` |
| 查询执行超时 | 500 | `{"error": "算法执行超时（30s）"}` |
| 服务器内部错误 | 500 | `{"error": "执行失败：..."}` |

> 注：GDS 不可用使用 400（客户端配置问题），而非 503（服务器临时不可用），更准确表达语义。

**Handler 实现要点**：
- 使用 `context.WithTimeout(30s)` 防止大图阻塞
- 利用现有 `parseUintParam(c, "id")` 和 `getTenantID(c)` 提取参数（复用已有 helper）

---

## 后端算法实现（analysis_service.go）

### 复用 neo4j_service.go 的关键能力

AnalysisService 与 Neo4jService 持有相同依赖（graphRepo / ontologyRepo / systemClient），共同使用 `getGraphAndEngine()` 模式获取 Neo4j 引擎，`buildSubgraph()` 供路径/邻居算法构建返回值，`dbbridge.ExecuteGraphQuery()` 执行 Cypher，`escapeCypher()` 安全处理。

### GDS 能力检测

```cypher
CALL gds.version() YIELD version RETURN version
```
失败则 GDS 不可用。检测结果在 service 实例级缓存（含版本号），避免每次请求重复探测。

### Cypher 算法

**度中心性（degree_centrality）**：
```cypher
MATCH (n) OPTIONAL MATCH (n)-[r]-()
WITH n, count(r) AS degree
RETURN elementId(n) AS node_id, degree AS score
ORDER BY degree DESC LIMIT $limit
```
可选加 `WHERE n:$label` 过滤。

**K跳邻居（khop_neighbors）**：
```cypher
MATCH path = (start)-[*1..$hops]-(n)
WHERE elementId(start) = $node_id
RETURN DISTINCT n, relationships(path) LIMIT $limit
```
hops 默认 2，最大 4。返回 `subgraph`（不生成 node_scores）。

**多路最短路径（multi_path）**：
对每对 (source, target) 执行 `allShortestPaths`，最多 5 对，结果合并去重返回 `subgraph`：
```cypher
MATCH p = allShortestPaths((a)-[*..10]-(b))
WHERE elementId(a) = $src AND elementId(b) = $tgt
RETURN p LIMIT 5
```

### GDS 算法（含投影管理）

**投影命名**：`addp_tmp_{graphID}_{tenantID}_{unixTimestamp}`（含时间戳避免并发冲突）

**创建投影**（算法开始）：
```cypher
CALL gds.graph.project($proj_name, $node_labels, $rel_types)
YIELD graphName, nodeCount, relationshipCount
```
`node_labels`/`rel_types` 为空时传 `['*']`。

**defer 清理**（异常路径安全）：
```cypher
CALL gds.graph.drop($proj_name, false)
```

| 算法 | GDS Cypher | 说明 |
|------|-----------|------|
| PageRank | `gds.pageRank.stream($proj, {maxIterations:20, dampingFactor:0.85})` | score 越高越重要 |
| Louvain | `gds.louvain.stream($proj)` | communityId 填入 NodeScore.community_id |
| WCC | `gds.wcc.stream($proj) YIELD nodeId, componentId` | componentId 作为 community_id |
| Betweenness | `gds.betweenness.stream($proj)` | score 越高越是"桥接节点" |

---

## 路由注册（router.go 修改）

在 `/:id` 组下增加：
```go
analysis := graphGroup.Group("/analysis")
{
    analysis.GET("/capabilities", analysisHandler.GetCapabilities)
    analysis.POST("/run", analysisHandler.RunAlgorithm)
}
```

---

## 前端实现

### api/analysis.js

```javascript
export const getCapabilities = (graphId) =>
  client.get(`/graphs/${graphId}/analysis/capabilities`)

export const runAlgorithm = (graphId, params) =>
  client.post(`/graphs/${graphId}/analysis/run`, params)
```

---

### AnalysisPanel.vue

**布局三区域（垂直排列，整体风格遵从前端设计规范）**：

**区域一（顶部）：算法选择**
- `el-select` 按 group 分组（「Cypher 算法」/「GDS 算法」）
- GDS 条目在 `gds_available=false` 时 disabled，附 tooltip 「需要 Neo4j GDS 插件」
- 选中算法后显示一行简短描述

**区域二（中部）：参数配置（v-if 按算法切换）**
- `degree_centrality`：可选节点标签多选
- `khop_neighbors`：起始节点 ID 输入（从 `selectedNodeId` prop 自动填入）+ 跳数步进器（1-4）
- `multi_path`：最多5行 source-target 对，动态增删
- `pagerank`/`betweenness`：标签/关系类型过滤 + Top-N 数量
- `louvain`/`wcc`：标签/关系类型过滤
- 「执行」按钮（loading 防重复提交）

**区域三（底部）：结果**
- 元信息行：算法名 + 耗时 + 统计（如「发现 12 个社区，共 1024 个节点」）
- 「在画布中着色」/「清除着色」按钮（主/次操作）
- `el-table` 展示：rank / 节点名 / 类型 / 分数（4列）
- 点击行 `emit('focus-node', nodeId)` 通知父组件定位节点

**Props / Emits**：
```javascript
props: {
  graphId: { type: Number, required: true },
  selectedNodeId: { type: String, default: '' }  // 画布选中节点 ID，自动填入参数
}
emits: ['apply-scores', 'clear-scores', 'focus-node']
```

**样式规范**（遵从前端设计规范，禁止硬编码颜色）：
```css
/* ✅ 正确 */
.analysis-panel {
  background: var(--addp-bg-primary) !important;
  border-left: 1px solid var(--addp-border-color);
}
.section-title {
  color: var(--addp-text-secondary);
  font-size: 12px;
}
.result-meta {
  color: var(--addp-text-primary);
  background: var(--addp-bg-secondary) !important;
}
```

---

### GraphBrowser.vue 修改点

1. 右侧面板增加 Tab 切换：「详情」（现有 NodePanel）↔ 「分析」（AnalysisPanel）
2. 工具栏增加「图分析」切换按钮（图标：DataAnalysis），控制 `activeRightPanel`
3. 传 `:selected-node-id="selectedNode?.id"` 给 AnalysisPanel
4. 监听 `@apply-scores(nodeScores, mode)` → `canvasRef.value.applyScoreColors(nodeScores, mode)`
5. 监听 `@clear-scores` → `canvasRef.value.resetNodeColors()`
6. 监听 `@focus-node(nodeId)` → `canvasRef.value.focusNodes([nodeId])`
7. 维护 `analysisActive` / `analysisAlgoName`：工具栏显示「已着色：{算法名}」标签 + 「清除」按钮

---

### GraphCanvas.vue 修改点

**新增 `applyScoreColors(nodeScores, mode, sizeMapping)` 方法**（`defineExpose` 暴露）：

- 遍历 nodeScores，对 `graphInstance.findById(nodeId)` 存在的节点执行 `graphInstance.updateItem(nodeId, { style: { fill: colorFn(score) } })`
- `mode='gradient'`：蓝→红渐变（JS 计算，t = (score-min)/(max-min)，插值 `#3b82f6` → `#ef4444`）
  > 注：G6 节点填充色是动态 JS 值，不走 CSS 变量；面板 UI 部分必须用 CSS 变量
- `mode='community'`：按 `communityId % 12` 从预定义色板取色（12色高对比度色板）
- `sizeMapping=true` 时同步调整节点 r 属性：20px（低分）→ 40px（高分）

**新增 `resetNodeColors()` 方法**（`defineExpose` 暴露）：
- 遍历所有节点，从 `model._meta.color` 恢复原始本体颜色，恢复 `r` 为默认值 28
- 无需重新请求后端

**社区色板（12色）**：
```javascript
const COMMUNITY_COLORS = [
  '#5B8FF9', '#61DDAA', '#F6BD16', '#E8684A',
  '#9270CA', '#FF99C3', '#6DC8EC', '#7CEFA0',
  '#F6903D', '#B9C0CA', '#D81E00', '#1DB446'
]
```

---

## 实施顺序

1. `models/analysis.go` — 定义 DTO，前后端接口契约对齐
2. `service/analysis_service.go` — 先实现 `CheckCapabilities` + `DegreeCentrality`，验证完整链路
3. `analysis_handler.go` + `router.go` + `main.go` — 路由注册，端对端联调
4. 其余 Cypher 算法（khop_neighbors、multi_path）
5. GDS 算法（pagerank、louvain、wcc、betweenness）
6. `api/analysis.js` — 前端 API 客户端
7. `GraphCanvas.vue` 扩展 — applyScoreColors / resetNodeColors
8. `AnalysisPanel.vue` — 分析面板组件
9. `GraphBrowser.vue` 集成 — 面板嵌入、事件联通

---

## 验证方案

### 后端验证

```bash
# 重启服务
./scripts/dev/restart.sh -graph

# 1. 能力探测
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8186/api/v1/graph/graphs/1/analysis/capabilities
# 预期：{"gds_available": false, "cypher_algos": [...], "gds_algos": []}

# 2. 度中心性
curl -X POST -H "Authorization: Bearer $TOKEN" -H "Content-Type: application/json" \
  -d '{"algorithm":"degree_centrality","limit":10}' \
  http://localhost:8186/api/v1/graph/graphs/1/analysis/run
# 预期：{"algorithm":"degree_centrality","node_scores":[{"rank":1,"score":...},...]}

# 3. GDS 算法不可用时的错误
curl -X POST ... -d '{"algorithm":"pagerank"}' ...
# 预期：HTTP 400，{"error":"该算法需要 Neo4j GDS 插件，当前实例未安装"}

# 4. K跳邻居（需先从浏览器获取一个节点 elementId）
curl -X POST ... -d '{"algorithm":"khop_neighbors","params":{"node_id":"<elementId>","hops":2}}' ...
# 预期：{"subgraph":{"nodes":[...],"edges":[...]},"node_scores":[]}
```

### 前端验证

1. 打开图谱浏览器 → 工具栏出现「图分析」按钮
2. 点击按钮 → 右侧面板切换到分析标签（Cypher 算法可选，GDS disabled）
3. 选度中心性 → 执行 → 结果列表展示 Top-N
4. 点击「在画布中着色」→ 节点渐变着色（蓝→红）
5. 工具栏出现「已着色：度中心性」标签，点「清除」→ 恢复本体颜色
6. 切换主题（深色/蓝色/紫色）→ 分析面板 UI 跟随主题变化（CSS 变量生效）
7. 画布点击节点 → 分析标签打开时 K跳邻居「起始节点」自动填入
8. 结果列表点击行 → 画布定位并高亮该节点

---

## 关键文件路径

- `graph/backend/internal/service/neo4j_service.go` — 参考 getGraphAndEngine/buildSubgraph 模式
- `graph/backend/internal/api/router.go` — 路由注册位置
- `graph/backend/internal/models/browse.go` — SubgraphResult DTO（算法结果复用）
- `graph/frontend/src/views/GraphBrowser.vue` — 集成 AnalysisPanel
- `graph/frontend/src/components/GraphCanvas.vue` — 扩展着色方法
- `docs/spec/addp-API设计规范.md` — API 设计规范
- `common-frontend/docs/addp前端风格设计规范.md` — 前端风格规范
- `graph/CLAUDE.md` — 实施完成后需更新阶段4状态
