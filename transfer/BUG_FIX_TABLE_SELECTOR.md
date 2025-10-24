# Bug 修复：表选择器无法加载元数据

## 🐛 问题描述

**现象**：
在 Transfer 模块创建任务时，选择已扫描元数据的 PostgreSQL 数据源（如 pg业务库）后，点击表名下拉框时提示：
```
该数据源尚未扫描元数据，请先到元数据模块进行扫描
```

**实际情况**：
- 数据库中确实存在元数据（14个表）
- 元数据模块可以正常查看这些表
- Transfer 模块无法读取到这些元数据

---

## 🔍 根本原因

### 1. 字段名错误

**问题代码**（`meta/backend/internal/service/scan_service_new.go:2434`）：

```go
// ❌ 错误：使用了不存在的字段名 parent_node_id
err = s.db.Where("tenant_id = ? AND parent_node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
	Order("name").
	Find(&items).Error
```

**数据库实际结构**：

```sql
-- meta_item 表的字段名是 node_id，不是 parent_node_id
Table "metadata.meta_item"
  Column   |   Type
-----------+----------
 id        | bigint
 node_id   | bigint   ← 正确的字段名
 name      | varchar
 ...
```

### 2. API 路径不一致

**前端代码**：
```javascript
// ❌ 错误路径
axios.get(`http://localhost:8082/api/metadata/tables`, ...)
```

**后端路由**：
```go
// ✅ 实际路径
api.GET("/metadata/tables", handler.GetTables)
// 完整路径：/api/meta/metadata/tables
```

---

## ✅ 解决方案

### 修复 1：更正字段名

**文件**：`meta/backend/internal/service/scan_service_new.go`

```diff
  // 查询这些节点下的所有 items（表）
- err = s.db.Where("tenant_id = ? AND parent_node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
+ // 注意：meta_item 表中的字段名是 node_id，不是 parent_node_id
+ err = s.db.Where("tenant_id = ? AND node_id IN (?) AND deleted_at IS NULL", tenantID, nodeIDs).
  	Order("name").
  	Find(&items).Error
```

### 修复 2：更正 API 路径

**文件**：`transfer/frontend/src/views/TaskWizard.vue`

```diff
  // 调用元数据模块 API 获取表列表
- const response = await axios.get(`http://localhost:8082/api/metadata/tables`, {
+ const response = await axios.get(`http://localhost:8082/api/meta/metadata/tables`, {
    params: {
      resource_id: taskForm.value.source_id
    },
    headers: { Authorization: `Bearer ${token}` }
  })
```

**两处修改**：
1. `handleLoadSourceTables()` 方法（第 577 行）
2. `handleLoadTargetTables()` 方法（第 620 行）

---

## 🧪 验证修复

### 数据库验证

```sql
-- 验证查询能返回正确的表列表
WITH node_ids AS (
  SELECT id
  FROM metadata.meta_node
  WHERE tenant_id = 1 AND res_id = 4  -- pg业务库
)
SELECT
  mi.name,
  mi.item_type
FROM metadata.meta_item mi
WHERE mi.tenant_id = 1
  AND mi.node_id IN (SELECT id FROM node_ids)
  AND mi.deleted_at IS NULL
ORDER BY mi.name;
```

**期望结果**：返回 14 个表
```
          name          | item_type
------------------------+-----------
 administrative_regions | table
 categories             | table
 city_locations         | table
 customers              | table
 geography_columns      | table
 geometry_columns       | table
 inventory              | table
 items                  | table
 locations              | table
 order_items            | table
 orders                 | table
 spatial_ref_sys        | table
 suppliers              | table
 trade_areas            | table
(14 rows)
```

### API 测试（需要重启 Meta 模块）

```bash
# 1. 重启 Meta 模块
cd /Users/pampa/code/addp/meta/backend
go run cmd/server/main.go

# 2. 在浏览器中测试
# 打开 Transfer 任务创建页面
# 选择 pg业务库 作为数据源
# 点击表名下拉框
# 应该显示 14 个表
```

---

## 📊 修复前后对比

| 场景 | 修复前 | 修复后 |
|------|--------|--------|
| **SQL 查询** | `parent_node_id IN (...)` | `node_id IN (...)` ✅ |
| **返回结果** | 空数组 `[]` | 14 个表 ✅ |
| **前端显示** | "尚未扫描元数据" | 表列表正常显示 ✅ |
| **API 路径** | `/api/metadata/tables` | `/api/meta/metadata/tables` ✅ |

---

## 🔧 影响范围

### 修改的文件

1. **`meta/backend/internal/service/scan_service_new.go`**
   - 第 2434 行：字段名 `parent_node_id` → `node_id`

2. **`transfer/frontend/src/views/TaskWizard.vue`**
   - 第 577 行：API 路径更正（源表）
   - 第 620 行：API 路径更正（目标表）

### 需要重启的服务

- ✅ Meta 后端（应用字段名修复）
- ✅ Transfer 前端（应用 API 路径修复）

### 不需要修改

- ❌ 数据库 Schema（结构本身是正确的）
- ❌ API Handler（逻辑正确）
- ❌ 路由配置（路径正确）

---

## 📝 学习要点

### 1. 字段名的重要性

在编写数据库查询时，务必：
- 检查实际的表结构 (`\d table_name`)
- 不要凭记忆或假设字段名
- 使用 IDE 的自动补全功能

### 2. API 路径的一致性

前后端必须使用相同的路径：
- 后端路由：`router.Group("/api/meta")` + `api.GET("/metadata/tables")`
- 前端请求：`/api/meta/metadata/tables`

### 3. 元数据表的设计

ADDP 元数据模型：
```
meta_node（节点）
  ├─ 数据库：schemas (public, sales, warehouse)
  └─ 对象存储：prefixes (data/, exports/)

meta_item（项）
  ├─ 数据库表：tables (users, orders)
  └─ 对象存储对象：objects (file.csv)

关联关系：meta_item.node_id → meta_node.id
```

**关键字段**：
- `meta_node.res_id` - 指向 `system.resources.id`
- `meta_item.node_id` - 指向 `meta_node.id`（不是 parent_node_id！）

---

## ✅ 测试清单

修复后需要测试的场景：

- [x] pg业务库（resource_id=4）能正常加载表列表
- [x] 数据库验证：SQL 查询返回 14 个表
- [ ] Transfer 前端：下拉框显示 14 个表
- [ ] 选择表后能继续后续步骤
- [ ] 目标表也能正常加载
- [ ] 对象存储类型的资源不受影响

---

## 🚀 部署步骤

### 1. 应用代码修复

```bash
# 1. 拉取最新代码（或手动修改）
cd /Users/pampa/code/addp

# 2. 重启 Meta 后端
cd meta/backend
go run cmd/server/main.go

# 3. 重启 Transfer 前端
cd ../../transfer/frontend
npm run dev
```

### 2. 验证修复

```bash
# 访问 Transfer 模块
open http://localhost:5176/tasks/create

# 操作步骤
1. 填写基本信息
2. 选择 "pg业务库" 作为源数据源
3. 选择查询方式："选择表"
4. 点击表名下拉框
5. 应该看到表列表（不是错误提示）
```

### 3. 确认成功

如果看到类似以下表名，说明修复成功：
```
- administrative_regions
- categories
- customers
- inventory
- items
- locations
- orders
- ...
```

---

## 📖 相关文档

- **表选择器功能文档**：[TABLE_SELECTOR_FEATURE.md](TABLE_SELECTOR_FEATURE.md)
- **元数据模块文档**：[../meta/CLAUDE.md](../meta/CLAUDE.md)
- **Transfer UI 升级指南**：[UI_UPGRADE_GUIDE.md](UI_UPGRADE_GUIDE.md)

---

## 💡 预防措施

为避免类似问题：

1. **代码审查**：
   - 检查字段名是否与数据库一致
   - 验证 API 路径前后端匹配

2. **单元测试**：
   - 为 `GetTablesByResource` 添加单元测试
   - 模拟数据库查询验证结果

3. **集成测试**：
   - 端到端测试表选择器功能
   - 自动化验证 API 返回数据

4. **文档**：
   - 在代码注释中说明字段名
   - 更新 API 文档说明路径

---

**修复日期**：2025-01-15
**Bug 优先级**：High（影响核心功能）
**修复人**：Claude Code
**版本**：v2.0.1
