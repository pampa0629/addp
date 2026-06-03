# Manager 模块数据预览与资源树重构方案

## 📋 重构概述

本次重构彻底改进了 Manager 模块的数据预览和资源树功能，解决了以下核心问题：

1. **固定 3 层限制**: 旧架构固定为 engine/schema/table 三层，无法支持任意深度路径
2. **节点标识混乱**: 使用 `makeNodeId()` 拼接字符串，容易冲突且难以追溯
3. **前端代码冗余**: DataExplorer.vue 710 行代码，大量重复逻辑
4. **职责不清**: 资源树构建逻辑散落在各个模块

## 🎯 重构成果

### 核心架构变化

| 项目 | 重构前 | 重构后 | 收益 |
|------|-------|--------|------|
| **节点标识** | `makeNodeId('table',1,'public','users')` | `addp://engine/1/path/public/users?type=table` | 全局唯一、可追溯 |
| **路径深度** | 固定 3 层 | 任意深度 | 灵活性 ↑ |
| **资源树构建** | Manager 内部 | Common 模块 | 4 个模块复用 |
| **前端代码** | 710 行 | 204 行 | -71% |
| **状态管理** | 组件内 ref | Pinia Store | 集中管理 |
| **API 设计** | 3 个参数 | 1 个 locator URI | 简洁统一 |

### 新架构分层

```
┌─────────────────────────────────────────┐
│         Frontend (DataExplorer.vue)      │
│              204 行 (-71%)               │
│  ┌─────────────────────────────────┐   │
│  │   explorerStore (Pinia)          │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         Manager Backend API              │
│      /api/explorer/* (新端点)            │
│  ┌─────────────────────────────────┐   │
│  │   ExplorerHandler                 │   │
│  │   ├── GetTree()                   │   │
│  │   ├── RefreshNode()               │   │
│  │   └── Preview()                   │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         Service Layer                    │
│  ┌─────────────────────────────────┐   │
│  │   ExplorerService (协调层)       │   │
│  │   ├── 调用 TreeBuilder            │   │
│  │   └── 调用 PreviewResolver        │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │   PreviewResolver (预览编排)      │   │
│  │   └── 选择合适的 PreviewProvider │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │   EngineConnector (连接管理)     │   │
│  │   └── 数据库连接池 + 缓存         │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
                    ↓
┌─────────────────────────────────────────┐
│         Common Module (复用层)          │
│  ┌─────────────────────────────────┐   │
│  │   ResourceLocator                 │   │
│  │   ├── ParseURI()                  │   │
│  │   ├── ToURI()                     │   │
│  │   └── PathString()                │   │
│  └─────────────────────────────────┘   │
│  ┌─────────────────────────────────┐   │
│  │   TreeBuilder                     │   │
│  │   ├── BuildFromMeta()             │   │
│  │   └── ConvertNodeToTree()         │   │
│  └─────────────────────────────────┘   │
└─────────────────────────────────────────┘
```

## 🔧 技术实现

### 1. ResourceLocator URI 系统

**格式**: `addp://engine/{engine_id}/path/{resource_path}?type={type}&node_id={node_id}` 或 `...&item_id={item_id}`

**示例**:
```
addp://engine/1/path/?type=database&node_id=10                    # 根节点
addp://engine/1/path/public?type=schema&node_id=11                # Schema 节点
addp://engine/1/path/public/users?type=table&item_id=100          # Table 数据项
addp://engine/1/path/bucket/folder/file.txt?type=object&item_id=101  # 对象存储数据项
```

**优势**:
- ✅ 全局唯一，不会冲突
- ✅ 支持任意深度路径
- ✅ 自包含完整上下文
- ✅ URL 编码处理特殊字符（中文、空格等）

**实现**:
- 后端: `common/resourcetree/locator.go` (148 行)
- 前端: `common-frontend/basic/src/types/resourceLocator.js`
- 测试: 45 个单元测试全部通过

### 2. 新 API 端点

#### 引擎列表
```
GET /api/explorer/engines
Authorization: Bearer {token}

Response:
{
  "data": [
    {
      "id": 59,
      "name": "pg库",
      "engine_type": "postgresql",
      "engine_origin": "general",
      ...
    }
  ]
}
```

#### 资源树
```
GET /api/explorer/tree/:engine_id?expand_depth=2
Authorization: Bearer {token}

Response:
{
  "data": {
    "tree": {
      "locator": "addp://engine/59/path/?type=database",
      "label": "pg库",
      "type": "engine",
      "icon": "Database",
      "metadata": {
        "engine_id": 59,
        "engine_type": "postgresql"
      },
      "children": [...]
    }
  }
}
```

#### 节点刷新
```
POST /api/explorer/tree/:engine_id/refresh?locator={uri}
Authorization: Bearer {token}

Response:
{
  "data": {
    "tree": {...}
  }
}
```

#### 数据预览
```
GET /api/v1/manager/preview?locator={uri}&page=1&page_size=20
Authorization: Bearer {token}

Response:
{
  "data": {
    "preview_type": "table",
    "data": {...},
    "table_info": {...},
    "metadata": {...}
  }
}
```

容器和多组件格式的选择参数必须保持语义独立：

| 参数 | 语义 | 示例 |
|---|---|---|
| `child_name` | 当前容器第一层 child | `Sheet1`、`cities`、`inner.zip` |
| `ref_path` | multi 格式 child 的ref 路径 | `roads.shp`、`roads.dbf` |
| `nested_child_path` | 当前 child 是容器时，其内部 child 相对路径 | `data/cities.csv`、`middle.zip/data/cities.csv` |

示例：

```text
GET /api/v1/manager/preview?locator={zip-uri}&child_name=inner.zip&nested_child_path=data/cities.csv
GET /api/v1/manager/preview?locator={zip-uri}&child_name=roads.shp&ref_path=roads.dbf
```

### 3. 前端 Pinia Store

**文件**: `manager/frontend/src/stores/explorer.js`

**状态**:
```javascript
{
  engines: [],              // 引擎列表
  currentEngineId: null,    // 当前引擎 ID
  tree: null,               // 资源树
  selectedLocator: null,    // 选中的 ResourceLocator URI
  expandedLocators: Set,    // 展开的节点
  refreshingLocators: Set,  // 正在刷新的节点
  selectedChildName: '',       // 当前容器第一层 child
  selectedRefPath: '',   // multi 格式ref 路径
  selectedNestedChildPath: '', // 嵌套容器内部 child 路径
  previewData: null,           // 预览数据
  previewLoading: false,       // 预览加载状态
  pagination: {...}            // 分页信息
}
```

**Actions**:
```javascript
loadEngines()              // 加载引擎列表
loadTree(engineId)         // 加载资源树
refreshNode(locator)       // 刷新节点
loadPreview(locator, page, childName, refPath, nestedChildPath) // 加载预览
selectNode(locator)        // 选择节点
expandNode(locator)        // 展开节点
```

### 4. 后端服务层

#### ExplorerService (协调层)
```go
type ExplorerService struct {
    engineRepo    *repository.EngineRepository
    metaClient    *commonClient.MetaClient
    treeBuilder   *resource.TreeBuilder
    previewResolver *PreviewResolver
}

// 获取资源树（调用 Common 的 TreeBuilder）
func (s *ExplorerService) GetTree(ctx context.Context, tenantID *uint, engineID uint, expandDepth int) (*resource.TreeNode, error)

// 刷新节点
func (s *ExplorerService) RefreshNode(ctx context.Context, tenantID *uint, locatorURI string) (*resource.TreeNode, error)
```

#### PreviewResolver (预览编排)
```go
type PreviewResolver struct {
    registry        *PreviewRegistry
    engineRepo      *repository.EngineRepository
    metaClient      *commonClient.MetaClient
    engineConnector *EngineConnector
}

// 根据 locator URI 和可选 child/ref/nested child 选择预览数据
func (r *PreviewResolver) PreviewFromURIWithSelection(ctx context.Context, locatorURI string, page, pageSize int, childName, refPath, nestedChildPath string, tenantID *uint) (*PreviewResult, error)
```

#### EngineConnector (连接管理)
```go
type EngineConnector struct {
    engineRepo  *repository.EngineRepository
    connections map[uint]*gorm.DB  // 连接池
    mu          sync.RWMutex
}

// 获取数据库连接（带缓存）
func (c *EngineConnector) GetConnection(engineID uint) (*gorm.DB, error)
```

## 📦 新增文件清单

### 后端 (Common)
- `common/resourcetree/locator.go` - ResourceLocator 核心实现
- `common/resourcetree/locator_test.go` - 单元测试 (45 个测试)
- `common/resourcetree/tree_builder.go` - TreeBuilder 资源树构建器
- `common/resourcetree/tree_builder_test.go` - 单元测试

### 后端 (Manager)
- `manager/backend/internal/service/explorer_service.go` - ExplorerService
- `manager/backend/internal/preview/preview_resolver.go` - PreviewResolver
- `manager/backend/internal/service/engine_connector.go` - EngineConnector
- `manager/backend/internal/api/explorer_handler.go` - ExplorerHandler
- `manager/backend/plugins/manifest.json` - 插件清单

### 前端
- `manager/frontend/src/stores/explorer.js` - Pinia Store
- `manager/frontend/src/views/DataExplorer.vue` - 重写 (710→204 行)
- `common-frontend/basic/src/types/resourceLocator.js` - 前端工具

## 🗑️ 已删除/退出

### 删除的代码
- ✅ `manager/frontend/src/utils/treeTransform.js` - 旧的树转换逻辑
- ✅ `common-frontend/basic/src/types/tree.js` 中的 `makeNodeId()` 和 `sanitizeNodeId()`

### 旧 API 端点

旧的 `engine_id/schema/table` 参数式端点不作为当前契约保留。Manager 预览、资源树刷新和跳转统一以标准 ResourceLocator 作为定位入口；如仍有旧端点或旧 Swagger path 残留，应直接删除或迁移到 locator 契约。

## 🚀 迁移指南

### 前端代码迁移

**旧代码**:
```javascript
import { makeNodeId } from '@addp/common-frontend'

const nodeId = makeNodeId('table', engineId, schema, table)
const params = {
  engine_id: engineId,
  schema: schema,
  table: table,
  page: 1,
  page_size: 20
}
const response = await dataExplorerAPI.getPreview(params)
```

**新代码**:
```javascript
import { buildLocator } from '@addp/common-frontend'
import { useExplorerStore } from '@/stores/explorer'

const store = useExplorerStore()

const locator = buildLocator({
  engineId,
  path: [schema, table],
  type: 'table',
  itemId
})
// 结果: addp://engine/1/path/public/users?type=table&item_id=100

await store.loadPreview(locator, 1)
```

### 后端集成示例

```go
import (
    "github.com/addp/common/resourcetree"
    commonModels "github.com/addp/common/models"
)

itemID := uint(100)

// 解析 locator URI
loc, err := resourcetree.ParseURI("addp://engine/1/path/public/users?type=table&item_id=100")
// loc.EngineID = 1
// loc.Path = ["public", "users"]
// loc.Type = "table"

// 构建 locator URI
loc := &resourcetree.ResourceLocator{
    EngineID: 1,
    Path:     []string{"public", "users"},
    Type:     resourcetree.TypeTable,
    ItemID:   &itemID,
}
uri := loc.ToURI()
// 结果: addp://engine/1/path/public/users?type=table&item_id=100
```

## 📊 性能对比

| 指标 | 重构前 | 重构后 | 提升 |
|------|-------|--------|------|
| 前端代码量 | 710 行 | 204 行 | 71% ↓ |
| API 参数数量 | 3-5 个 | 1 个 (locator) | 简化 80% |
| 节点 ID 冲突概率 | 中等 (字符串拼接) | 零 (URI 唯一) | 100% ↓ |
| 资源树复用性 | 0 模块 | 4 模块 | ∞ |
| 类型安全性 | 弱 (字符串) | 强 (结构化 URI) | 显著提升 |

## 🔍 常见问题

### Q1: 旧 API 什么时候删除？
A: 旧 API 不作为当前契约保留。发现旧入口、旧 Swagger path 或旧参数式调用时，应迁移到标准 ResourceLocator 并清理旧实现。

### Q2: 如何处理中文路径？
A: ResourceLocator 自动进行 URL 编码，支持中文、空格等特殊字符：
```
addp://engine/1/path/数据库/用户表?type=table
```

### Q3: 如何在其他模块复用 TreeBuilder？
A: 导入 Common 模块的 TreeBuilder，调用方先取得 Meta 事实后传入构建方法即可：
```go
import "github.com/addp/common/resourcetree"

treeBuilder := resourcetree.NewTreeBuilder()
tree, err := treeBuilder.BuildFromMeta(engine, metaNodes, expandDepth)
```

### Q4: Preview 的定位入口是什么？
A: Preview 只接受标准 ResourceLocator URI。调用方应先基于 Meta item / node 事实获得 locator，不应继续传递 `engine_id, schema, table` 这类拆散的定位参数。

## 📚 相关文档

- [ADDP 各模块简要介绍](../../docs/concepts/addp各模块功能介绍.md)
- [ADDP API 设计规范](../../docs/addp-api设计规范.md)
- [Manager 模块架构](../CLAUDE.md)

## ✅ 验证清单

- [x] Phase 1: Common 基础设施 (ResourceLocator + TreeBuilder)
- [x] Phase 2: Manager API 重构 (新端点 + 服务层)
- [x] Phase 3: 前端重构 (Pinia Store + DataExplorer.vue)
- [x] Phase 4: 插件系统标准化 (manifest.json)
- [x] Phase 5: 清理废弃代码 (makeNodeId + treeTransform.js)
- [x] 单元测试: 45 个测试全部通过
- [x] API 测试: 所有新端点验证通过
- [x] 编译测试: 前后端编译无错误
- [x] 运行测试: Manager 服务正常启动
