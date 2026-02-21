# Model 模块 Phase 2 扩展：码值集管理

## 设计目标

在 Phase 2（数据建模）基础上，增加**码值集管理（Code Set Management）**功能，实现：
- 统一业务码值定义（性别、状态、分类等枚举值）
- 与数据元集成，支持数据质量枚举校验
- 支持系统内置和自定义码值集

---

## 数据库设计

### 1. code_sets（码值集表）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGSERIAL PRIMARY KEY | |
| tenant_id | BIGINT NOT NULL | 租户隔离 |
| code | VARCHAR(100) NOT NULL | 唯一标识符（如 gender, industry_code） |
| name | VARCHAR(200) NOT NULL | 显示名称（如"性别"、"行业分类"） |
| type | VARCHAR(50) DEFAULT 'custom' | system/custom（系统内置/自定义） |
| description | TEXT | 说明 |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |
| **约束** | UNIQUE(tenant_id, code) | 同一租户内 code 唯一 |

### 2. code_items（码值项表）

| 字段 | 类型 | 说明 |
|------|------|------|
| id | BIGSERIAL PRIMARY KEY | |
| code_set_id | BIGINT NOT NULL | 外键 code_sets(id) ON DELETE CASCADE |
| code | VARCHAR(100) NOT NULL | 编码值（如 "1", "M", "110000"） |
| value | VARCHAR(200) NOT NULL | 显示值（如 "男"、"北京市"） |
| description | TEXT | 说明 |
| sort_order | INT DEFAULT 0 | 排序 |
| is_active | BOOLEAN DEFAULT true | 是否启用 |
| parent_id | BIGINT | 支持树形结构（可选，本期不实现树形） |
| created_at | TIMESTAMP | |
| updated_at | TIMESTAMP | |
| **约束** | UNIQUE(code_set_id, code) | 同一码值集内 code 唯一 |

### 3. elements 表修改

**新增字段：**
- `code_set_id BIGINT` - 外键 code_sets(id)，可选
- 当数据元关联码值集后，前端展示码值项列表供参考

---

## 后端实现

### Step 1：数据模型

**新增文件：model/backend/internal/models/code_set.go**

```go
type CodeSet struct {
    ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    TenantID    int64     `gorm:"not null;index:idx_codeset_tenant" json:"tenant_id"`
    Code        string    `gorm:"size:100;not null;uniqueIndex:idx_codeset_tenant_code" json:"code"`
    Name        string    `gorm:"size:200;not null" json:"name"`
    Type        string    `gorm:"size:50;default:custom" json:"type"` // system/custom
    Description string    `gorm:"type:text" json:"description"`
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

func (CodeSet) TableName() string { return "model.code_sets" }

type CodeItem struct {
    ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
    CodeSetID   int64     `gorm:"not null;index:idx_codeitem_set" json:"code_set_id"`
    Code        string    `gorm:"size:100;not null;uniqueIndex:idx_codeitem_set_code" json:"code"`
    Value       string    `gorm:"size:200;not null" json:"value"`
    Description string    `gorm:"type:text" json:"description"`
    SortOrder   int       `gorm:"default:0" json:"sort_order"`
    IsActive    bool      `gorm:"default:true" json:"is_active"`
    ParentID    *int64    `json:"parent_id"` // 预留树形结构，本期不实现
    CreatedAt   time.Time `json:"created_at"`
    UpdatedAt   time.Time `json:"updated_at"`
}

func (CodeItem) TableName() string { return "model.code_items" }

// DTOs
type CreateCodeSetRequest struct {
    Code        string `json:"code" binding:"required"`
    Name        string `json:"name" binding:"required"`
    Type        string `json:"type"`
    Description string `json:"description"`
}

type UpdateCodeSetRequest struct {
    Name        string `json:"name" binding:"required"`
    Type        string `json:"type"`
    Description string `json:"description"`
}

type CreateCodeItemRequest struct {
    Code        string `json:"code" binding:"required"`
    Value       string `json:"value" binding:"required"`
    Description string `json:"description"`
    SortOrder   int    `json:"sort_order"`
    IsActive    bool   `json:"is_active"`
}

type UpdateCodeItemRequest struct {
    Value       string `json:"value" binding:"required"`
    Description string `json:"description"`
    SortOrder   int    `json:"sort_order"`
    IsActive    bool   `json:"is_active"`
}
```

### Step 2：Repository

**新增文件：model/backend/internal/repository/code_set_repository.go**

标准 CRUD + Items 管理：
- `Create/GetByID/List/Update/Delete`
- `GetItems(codeSetID int64) ([]CodeItem, error)`
- `CreateItem/UpdateItem/DeleteItem`
- `ExistsByCode(tenantID int64, code string) (bool, error)`

### Step 3：Service

**新增文件：model/backend/internal/service/code_set_service.go**

业务逻辑：
- Code 唯一性校验
- Type 校验（只允许 system/custom）
- Items 的 code 唯一性校验
- 租户隔离校验

### Step 4：Handler

**新增文件：model/backend/internal/api/code_set_handler.go**

路由：
```
GET    /code-sets          - 列表
POST   /code-sets          - 创建
GET    /code-sets/:id      - 详情
PUT    /code-sets/:id      - 更新
DELETE /code-sets/:id      - 删除
GET    /code-sets/:id/items        - 获取码值项
POST   /code-sets/:id/items        - 创建码值项
PUT    /code-sets/:id/items/:iid   - 更新码值项
DELETE /code-sets/:id/items/:iid   - 删除码值项
```

### Step 5：Router + Main

**修改文件：**
- `router.go` - 注册 codeSetSvc + 路由
- `main.go` - AutoMigrate(&models.CodeSet{}, &models.CodeItem{}) + 依赖注入

---

## 前端实现

### Step 1：API

**修改文件：model/frontend/src/api/model.js**

增加 `codeSetAPI`：
```js
export const codeSetAPI = {
  list: (params) => client.get('/code-sets', { params }),
  create: (data) => client.post('/code-sets', data),
  get: (id) => client.get(`/code-sets/${id}`),
  update: (id, data) => client.put(`/code-sets/${id}`, data),
  delete: (id) => client.delete(`/code-sets/${id}`),

  getItems: (id) => client.get(`/code-sets/${id}/items`),
  createItem: (id, data) => client.post(`/code-sets/${id}/items`, data),
  updateItem: (id, itemId, data) => client.put(`/code-sets/${id}/items/${itemId}`, data),
  deleteItem: (id, itemId) => client.delete(`/code-sets/${id}/items/${itemId}`)
}
```

### Step 2：路由

**修改文件：model/frontend/src/router/index.js**

新增路由：
```js
{
  path: 'standard/code-sets',
  name: 'CodeSetList',
  component: () => import('../views/CodeSetList.vue'),
  meta: { requiresAuth: true, title: '码值集管理' }
},
{
  path: 'standard/code-sets/:id',
  name: 'CodeSetDetail',
  component: () => import('../views/CodeSetDetail.vue'),
  meta: { requiresAuth: true, title: '码值集详情' }
}
```

### Step 3：视图

**新增文件：model/frontend/src/views/CodeSetList.vue**

功能：
- 列表：码值集编码、名称、类型（Tag）、创建时间
- 筛选：关键词、类型（system/custom）
- 操作：查看详情、删除（系统内置禁止删除）
- 新建对话框：编码（必填）、名称（必填）、类型、描述

**新增文件：model/frontend/src/views/CodeSetDetail.vue**

布局：
- 顶部：返回、码值集名称、类型 Tag、保存按钮
- Card 1（基本信息）：编码（只读）、名称、类型、描述
- Card 2（码值项）：
  - 表格：编码、显示值、排序、启用状态（Switch）、描述、操作
  - "添加码值项"按钮
  - 添加/编辑对话框：编码、显示值、排序、启用、描述

### Step 4：导航菜单

**修改文件：model/frontend/src/components/Layout.vue**

在"数据标准"子菜单中增加：
```vue
<el-menu-item index="/standard/code-sets">
  <el-icon><Grid /></el-icon>
  <span>码值集管理</span>
</el-menu-item>
```

**修改文件：portal/frontend/src/views/Portal.vue**

同步更新"数据标准"菜单：
```vue
<el-menu-item index="/standard/code-sets">
  <el-icon><Grid /></el-icon>
  <span>码值集管理</span>
</el-menu-item>
```

更新 `standardPageMap`：
```js
const standardPageMap = {
  'domains': 'standard/domains',
  'glossaries': 'standard/glossaries',
  'elements': 'standard/elements',
  'code-sets': 'standard/code-sets',  // 新增
  '': 'standard/domains'
}
```

---

## Element 集成

### 后端：Element 模型扩展

**修改文件：model/backend/internal/models/element.go**

```go
type Element struct {
    // ... 现有字段
    CodeSetID   *int64 `json:"code_set_id"` // 新增：关联码值集
}

type CreateElementRequest struct {
    // ... 现有字段
    CodeSetID   *int64 `json:"code_set_id"` // 新增
}

type UpdateElementRequest struct {
    // ... 现有字段
    CodeSetID   *int64 `json:"code_set_id"` // 新增
}
```

**修改文件：model/backend/cmd/server/main.go**

AutoMigrate 后增加自动迁移列：
```go
// 为 elements 表添加 code_set_id 列（如果不存在）
db.Exec("ALTER TABLE model.elements ADD COLUMN IF NOT EXISTS code_set_id BIGINT REFERENCES model.code_sets(id)")
```

### 前端：ElementDetail 集成

**修改文件：model/frontend/src/views/ElementDetail.vue**

1. 基本信息表单中增加：
```vue
<el-form-item label="关联码值集">
  <el-select v-model="form.code_set_id" clearable style="width:100%">
    <el-option v-for="cs in codeSets" :key="cs.id" :label="`${cs.name} (${cs.code})`" :value="cs.id" />
  </el-select>
</el-form-item>
```

2. 如果关联了码值集，显示码值项列表（只读）：
```vue
<el-col :span="24" v-if="form.code_set_id" style="margin-top:16px">
  <el-card shadow="never">
    <template #header><span class="card-title">关联的码值项（参考）</span></template>
    <el-table :data="codeItems" size="small">
      <el-table-column label="编码" prop="code" width="120" />
      <el-table-column label="显示值" prop="value" width="150" />
      <el-table-column label="描述" prop="description" show-overflow-tooltip />
    </el-table>
  </el-card>
</el-col>
```

---

## 实施顺序

1. **后端模型 + 迁移** - code_set.go, main.go AutoMigrate
2. **后端 Repository + Service** - code_set_repository.go, code_set_service.go
3. **后端 Handler + Router** - code_set_handler.go, router.go
4. **前端 API + 路由** - api/model.js, router/index.js
5. **前端视图** - CodeSetList.vue, CodeSetDetail.vue
6. **导航菜单更新** - Layout.vue, Portal.vue
7. **Element 集成** - Element 模型扩展 + ElementDetail.vue 增强
8. **重启验证** - `bash scripts/dev/restart.sh -model`

---

## 验证方式

1. 创建码值集"性别"（code: gender），添加码值项：1-男、2-女、0-未知
2. 创建数据元"员工性别"，关联"性别"码值集
3. 在 ElementDetail 页面查看关联的码值项列表
4. 测试码值集的增删改查、码值项的增删改查
