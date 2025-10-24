# Transfer 模块表选择器功能

## 📌 功能说明

在 Transfer 模块创建任务时，当用户选择数据库类型的数据源（PostgreSQL/MySQL）后，在配置源/目标步骤中：

- **旧版**：需要手动输入表名（容易输错）
- **新版**：从元数据模块自动加载可用表列表，通过下拉框选择

## ✅ 已实现的改进

### 1. 前端改进（TaskWizard.vue）

#### 步骤 3：配置源

```vue
<!-- 旧版：手动输入 -->
<el-input v-model="sourceConfig.table" placeholder="输入表名，如：users" />

<!-- 新版：下拉选择 -->
<el-select
  v-model="sourceConfig.table"
  placeholder="选择表"
  filterable
  :loading="loadingSourceTables"
  @focus="handleLoadSourceTables"
>
  <el-option
    v-for="table in availableSourceTables"
    :key="table"
    :label="table"
    :value="table"
  />
</el-select>
<div class="hint">
  从元数据模块自动获取可用表列表。
  <el-button type="primary" link size="small" @click="handleLoadSourceTables">
    刷新列表
  </el-button>
</div>
```

#### 步骤 5：配置目标

```vue
<el-select
  v-model="targetConfig.table"
  placeholder="选择表"
  filterable
  allow-create  ← 允许手动输入新表名
  :loading="loadingTargetTables"
  @focus="handleLoadTargetTables"
>
  <el-option
    v-for="table in availableTargetTables"
    :key="table"
    :label="table"
    :value="table"
  />
</el-select>
```

**注意**：目标表支持 `allow-create`，用户可以手动输入一个不存在的表名（用于创建新表）。

#### 新增数据和方法

```javascript
// 数据
const availableSourceTables = ref([])
const availableTargetTables = ref([])
const loadingSourceTables = ref(false)
const loadingTargetTables = ref(false)

// 方法
const handleLoadSourceTables = async () => {
  // 调用元数据模块 API
  const response = await axios.get(`http://localhost:8082/api/metadata/tables`, {
    params: { resource_id: taskForm.value.source_id },
    headers: { Authorization: `Bearer ${token}` }
  })

  availableSourceTables.value = response.data.map(item => item.name || item)
  ElMessage.success(`已加载 ${availableSourceTables.value.length} 个表`)
}
```

---

### 2. 后端改进（Meta 模块）

#### 新增 API 端点

**文件**：`meta/backend/internal/api/handler.go`

```go
// GetTables 获取资源的表列表（用于Transfer模块字段选择）
// GET /api/metadata/tables?resource_id=1
func (h *Handler) GetTables(c *gin.Context) {
	tenantID := middleware.GetTenantID(c)

	resourceIDStr := c.Query("resource_id")
	resourceID, err := strconv.ParseUint(resourceIDStr, 10, 32)
	// ...

	// 获取该资源下的所有表
	tables, err := h.scanService.GetTablesByResource(uint(resourceID), tenantID)

	// 返回表名列表
	tableNames := make([]string, len(tables))
	for i, table := range tables {
		tableNames[i] = table.Name
	}

	c.JSON(http.StatusOK, tableNames)
}
```

#### 路由注册

**文件**：`meta/backend/internal/api/router_new.go`

```go
// 元数据相关
api.GET("/metadata/object", handler.GetObjectMetadata)
api.POST("/metadata/extract", handler.ExtractObjectMetadata)
api.GET("/metadata/tables", handler.GetTables)  ← 新增
```

#### Service 层新增方法

**文件**：`meta/backend/internal/service/scan_service_new.go`

```go
// GetTablesByResource 获取资源下所有的表（用于Transfer模块）
func (s *ScanServiceNew) GetTablesByResource(resourceID, tenantID uint) ([]models.MetaItem, error) {
	// 先获取该资源下的所有节点ID（schemas/prefixes）
	var nodeIDs []uint
	s.db.Model(&models.MetaNode{}).
		Where("tenant_id = ? AND res_id = ?", tenantID, resourceID).
		Pluck("id", &nodeIDs)

	// 查询这些节点下的所有 items（表）
	var items []models.MetaItem
	s.db.Where("tenant_id = ? AND parent_node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
		Order("name").
		Find(&items)

	return items, nil
}
```

---

## 🔄 工作流程

### 用户操作流程

```
1. 进入 Transfer 任务创建向导
   ↓
2. 步骤 2：选择源数据源
   选择 PostgreSQL 数据源（如：pg业务库）
   ↓
3. 步骤 3：配置源
   选择查询方式：选择表
   点击表名下拉框
   ↓
4. 自动触发 @focus 事件
   → 调用 handleLoadSourceTables()
   ↓
5. 前端请求元数据 API
   GET http://localhost:8082/api/metadata/tables?resource_id=1
   ↓
6. 元数据模块返回表列表
   ["users", "orders", "products", ...]
   ↓
7. 下拉框显示可用表
   用户选择 "users"
   ↓
8. 继续后续配置步骤
```

### 数据流

```
Transfer Frontend (TaskWizard.vue)
  ↓ HTTP GET /api/metadata/tables?resource_id=1
Meta Backend (API Handler)
  ↓ GetTables()
Meta Service (ScanServiceNew)
  ↓ GetTablesByResource()
PostgreSQL metadata schema
  ↓ Query meta_node + meta_item
[
  {id: 1, name: "users", parent_node_id: 10},
  {id: 2, name: "orders", parent_node_id: 10},
  ...
]
  ↓ Extract names
["users", "orders", "products"]
  ↓ Return JSON
Transfer Frontend
  ↓ Display in el-select
```

---

## 📝 使用示例

### 示例 1：从元数据加载表列表

```javascript
// 用户操作
1. 选择源数据源："pg业务库 (postgresql)"
2. 点击"表名"下拉框
3. 系统自动加载表列表

// 后台请求
GET http://localhost:8082/api/metadata/tables?resource_id=1
Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5...

// 响应
[
  "users",
  "orders",
  "products",
  "categories"
]

// 用户选择
选择 "users" → sourceConfig.table = "users"
```

### 示例 2：元数据未扫描的情况

```javascript
// 用户操作
1. 选择一个从未扫描的数据源
2. 点击表名下拉框

// 后台响应
{
  "error": "未找到元数据"
}

// 前端提示
ElMessage.warning('该数据源尚未扫描元数据，请先到元数据模块进行扫描')

// 用户应该
→ 打开元数据模块（http://localhost:5175）
→ 同步该数据源
→ 扫描 Schema
→ 回到 Transfer 模块重新加载
```

### 示例 3：目标表允许手动输入

```javascript
// 场景：导出到新表
1. 选择目标数据源："pg数据仓库 (postgresql)"
2. 点击目标表名下拉框
3. 看到现有表列表：["archive", "backup"]
4. 手动输入新表名："users_2024_export"
5. 系统接受新表名 → targetConfig.table = "users_2024_export"

// allow-create 属性使得这成为可能
```

---

## 🚀 优势

### 1. 用户体验改进

| 对比项 | 旧版 | 新版 |
|--------|------|------|
| **输入方式** | 手动键入 | 下拉选择 |
| **错误率** | 高（拼写错误） | 低（从列表选择） |
| **发现性** | 需要查阅文档 | 自动显示可用表 |
| **效率** | 需要记忆表名 | 即时查看所有表 |

### 2. 与元数据模块集成

- ✅ 复用元数据模块已扫描的表信息
- ✅ 避免重复扫描数据库
- ✅ 统一的元数据管理
- ✅ 跨模块数据共享

### 3. 智能提示

- 如果表列表为空，提示用户先扫描元数据
- 支持刷新按钮，重新加载最新表列表
- 支持搜索过滤（filterable）
- 目标表支持手动输入新表名（创建新表场景）

---

## 🔧 技术细节

### 前端实现要点

#### 1. 按需加载

```javascript
// 使用 @focus 事件，只在用户点击时加载
<el-select @focus="handleLoadSourceTables">
```

**优势**：
- 不会在页面加载时立即请求（减少不必要的 API 调用）
- 用户明确需要时才加载

#### 2. 加载状态

```javascript
:loading="loadingSourceTables"
```

**效果**：下拉框显示 Loading 图标

#### 3. 错误处理

```javascript
try {
  const response = await axios.get(...)
  availableSourceTables.value = response.data
} catch (error) {
  if (error.response?.status === 404) {
    ElMessage.warning('该数据源尚未扫描元数据...')
  } else {
    ElMessage.error('加载表列表失败: ' + error.message)
  }
}
```

#### 4. 数据源变更时清空

```javascript
const handleSourceResourceChange = (resourceId) => {
  selectedSourceResource.value = resources.value.find(r => r.id === resourceId)
  // 清空表列表，等待用户手动加载
  availableSourceTables.value = []
}
```

### 后端实现要点

#### 1. 多租户隔离

```go
tenantID := middleware.GetTenantID(c)
tables, err := h.scanService.GetTablesByResource(resourceID, tenantID)
```

#### 2. 两级查询优化

```go
// 第一步：获取节点 ID（schemas/prefixes）
var nodeIDs []uint
db.Model(&MetaNode{}).
  Where("tenant_id = ? AND res_id = ?", tenantID, resourceID).
  Pluck("id", &nodeIDs)

// 第二步：查询这些节点下的所有表
var items []MetaItem
db.Where("tenant_id = ? AND parent_node_id IN (?)", tenantID, nodeIDs).
  Order("name").
  Find(&items)
```

**优势**：
- 只返回已扫描的表
- 自动排除软删除的表（deleted_at IS NULL）
- 按名称排序

#### 3. 简洁的 API 响应

```go
// 直接返回表名数组
c.JSON(http.StatusOK, []string{"users", "orders", "products"})

// 而不是复杂的对象结构
```

---

## 📋 待改进事项

### 1. 性能优化（未来）

- [ ] 添加缓存机制（Redis）
- [ ] 支持分页加载（表数量>1000时）
- [ ] 增量更新表列表

### 2. 功能增强（未来）

- [ ] 显示表的额外信息（记录数、大小、最后更新时间）
- [ ] 支持表预览（点击表名预览前几行数据）
- [ ] 支持表搜索（模糊搜索表名）
- [ ] 自动推荐常用表

### 3. 用户体验（未来）

- [ ] 表分组显示（按 Schema 分组）
- [ ] 标记新增表（最近扫描的）
- [ ] 支持收藏常用表

---

## 🐛 故障排除

### 问题 1：下拉框无法加载表列表

**现象**：点击下拉框后，显示 Loading，但没有数据

**可能原因**：
1. 元数据模块未启动
2. 该数据源从未扫描
3. JWT Token 过期

**解决方法**：
```bash
# 1. 检查元数据模块
curl http://localhost:8082/health

# 2. 检查该数据源是否已扫描
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8082/api/metadata/tables?resource_id=1"

# 3. 重新登录获取新 Token
```

### 问题 2：提示"该数据源尚未扫描元数据"

**解决步骤**：
1. 打开元数据模块：http://localhost:5175
2. 在数据源列表中找到该数据源
3. 点击"同步"按钮
4. 选择要扫描的 Schema
5. 点击"扫描"
6. 等待扫描完成
7. 回到 Transfer 模块，点击"刷新列表"

### 问题 3：表列表不完整

**可能原因**：部分 Schema 未扫描

**解决方法**：
1. 到元数据模块查看该数据源的扫描状态
2. 扫描缺失的 Schema
3. 回到 Transfer 刷新列表

---

## 📖 相关文档

- **Transfer UI 升级指南**：[UI_UPGRADE_GUIDE.md](UI_UPGRADE_GUIDE.md)
- **Meta 模块文档**：[../meta/CLAUDE.md](../meta/CLAUDE.md)
- **API 设计文档**：[../docs/API_DESIGN.md](../docs/API_DESIGN.md)

---

## 💬 总结

这次改进实现了：

1. ✅ **前端改进**：手动输入 → 下拉选择
2. ✅ **后端新增 API**：`GET /api/metadata/tables`
3. ✅ **Service 层方法**：`GetTablesByResource()`
4. ✅ **元数据集成**：复用已扫描的表信息
5. ✅ **错误提示**：引导用户先扫描元数据

**用户现在可以**：
- 在 Transfer 创建任务时，从下拉框选择表
- 自动加载已扫描的表列表
- 减少输入错误
- 提升创建任务的效率

**下一步**：
- 实现字段列表的自动加载（用于字段映射）
- 支持表预览功能
- 添加性能优化（缓存、分页）

---

**创建日期**: 2025-01-15
**版本**: v1.0.0
**作者**: Claude Code
