## 新服务的开发指南

实现或扩展服务时:

1. **遵循 System 模块模式**:

   ```
   service/backend/
   ├── cmd/server/main.go       # 入口点
   ├── internal/
   │   ├── api/                 # HTTP 处理器
   │   ├── service/             # 业务逻辑
   │   ├── repository/          # 数据访问
   │   ├── models/              # 数据结构
   │   ├── middleware/          # 认证、日志
   │   └── config/              # 配置
   └── pkg/utils/               # 共享工具
   ```
2. **数据库约定**:

   - 使用 PostgreSQL schema 隔离 (所有模块使用 PostgreSQL 和专用 schemas)
   - 使用 GORM 作为 ORM,带 AutoMigrate
   - 将 schemas 添加到 `scripts/infra/init-postgresql.sql`
   - 使用 `updated_at` 触发器进行时间戳跟踪
3. **配置**:

   - 通过 `internal/config/config.go` 从环境变量读取
   - 支持开发和 Docker 部署模式
   - 为缺失的环境变量设置默认值
4. **认证**:

   - 重用 System 模块的 JWT 验证逻辑
   - 从 System 导入 auth 中间件或创建相同的
   - 从 JWT 声明中提取 user_id 并传递给服务层
5. **Docker 集成**:

   - 在服务根目录创建 Dockerfile
   - 使用 `profile: full` 将服务添加到 `docker-compose.yml`
   - 使用健康检查进行依赖管理
   - 连接到 `addp-network` 进行服务间通信
6. **前端集成**:

   - 创建独立的 `<module>/frontend/` 目录
   - 从 `system/frontend/` 复制结构 (Vue 3 + Pinia + Element Plus)
   - 创建 `api/client.js` 指向模块后端
   - 创建 `api/auth.js` 指向 System 后端 (8180) 进行认证
   - 从 System 模块复制 auth store 模式 (独立副本,非共享)
   - 在 `vite.config.js` 中设置唯一的开发端口 (System: 5173, Manager: 5174 等)
   - 配置路由基础路径 (例如 Manager 模块的 `/manager/`)
   - 创建 Dockerfile 和 nginx.conf 用于生产部署
   - 使用唯一端口和 `profile: full` 添加到 docker-compose.yml

## 开发工作流

### 添加新的 API 端点

遵循代码库中使用的分层架构模式:

1. **在 `internal/models/` 中定义数据模型**:

   ```go
   type CreateResourceRequest struct {
       Name           string                 `json:"name" binding:"required"`
       ResourceType   string                 `json:"resource_type" binding:"required"`
       ConnectionInfo map[string]interface{} `json:"connection_info"`
   }
   ```
2. **在 `internal/repository/` 中添加仓库方法**:

   ```go
   func (r *ResourceRepository) Create(resource *models.Resource) error {
       return r.db.Create(resource).Error
   }
   ```
3. **在 `internal/service/` 中实现业务逻辑**:

   ```go
   func (s *ResourceService) CreateResource(req *CreateResourceRequest) (*Resource, error) {
       // 验证、加密、业务规则
       return s.repo.Create(resource)
   }
   ```
4. **在 `internal/api/` 中创建 HTTP 处理器**:

   ```go
   func (h *EngineHandler) Create(c *gin.Context) {
       var req CreateEngineRequest
       if err := c.ShouldBindJSON(&req); err != nil {
           c.JSON(400, gin.H{"error": err.Error()})
           return
       }
       engine, err := h.service.CreateEngine(&req)
       c.JSON(201, engine)
   }
   ```
5. **在 `internal/api/router.go` 中注册路由**:

   ```go
   protected.POST("/engines", engineHandler.Create)
   ```

**示例 PR**: 参见 system 模块资源管理实现

### 数据库迁移

GORM AutoMigrate 自动处理 schema 更改:

1. **在 `internal/models/` 中修改模型结构**:

   ```go
   type Resource struct {
       ID             uint      `gorm:"primaryKey"`
       Name           string    `gorm:"not null"`
       NewField       string    `gorm:"default:''" json:"new_field"` // 添加新字段
   }
   ```
2. **在 `internal/repository/database.go` 中添加到 AutoMigrate**:

   ```go
   db.AutoMigrate(
       &models.Resource{},
       &models.User{},
       // 在此添加新模型
   )
   ```
3. **重启应用** - 迁移在启动时运行

**对于复杂迁移**:

- 在 `scripts/migrations/` 中创建 SQL 脚本用于数据转换
- 在部署新版本前通过 `make db-migrate` 手动运行
- 在 PR 描述中记录破坏性更改

**Meta 模块特殊性**:
统一的元数据模型 (resource/node/item) 需要协调更新:

- [meta/backend/internal/models/](meta/backend/internal/models/) 中的模型结构
- `meta_dictionary` 表中的字典验证
- 如果结构更改,`attributes` 字段中的 JSON schema 版本
- 可能需要现有元数据的数据迁移脚本

### 添加前端页面

**重要**: 根据功能将页面添加到正确的前端:

- System 功能 (用户、日志、资源) → `system/frontend/`
- Manager 功能 (数据源、目录) → `manager/frontend/`
- Meta 功能 (元数据、血缘) → `meta/frontend/`
- Transfer 功能 (任务、执行) → `transfer/frontend/`

每个前端的步骤:

1. 在 `<module>/frontend/src/views/` 中创建 Vue 组件
2. 在 `<module>/frontend/src/api/` 中添加 API 函数
3. 在 `<module>/frontend/src/router/index.js` 中注册路由（作为 Layout 的子路由）
4. 在 `<module>/frontend/src/components/Layout.vue` 中添加导航链接

**Layout.vue 和路由结构要求**:

所有模块前端**必须**在 `<module>/frontend/src/components/Layout.vue` 中创建布局组件：

- **位置**: `components/Layout.vue`（不是 `views/Layout.vue`）
- **功能**: 提供双模式支持
  - 独立访问模式：显示完整的 header + sidebar + content 布局
  - Portal 嵌入模式：通过 `isInIframe` 检测，仅显示 `<router-view>`
- **路由结构**: 使用嵌套路由，Layout 作为父组件包裹所有需要认证的页面

路由配置示例:

```javascript
// <module>/frontend/src/router/index.js
import Layout from '../components/Layout.vue'

const routes = [
  {
    path: '/login',
    name: 'Login',
    component: () => import('../views/Login.vue'),
    meta: { public: true }
  },
  {
    path: '/',
    component: Layout,  // Layout 作为父组件
    redirect: '/main-page',
    meta: { requiresAuth: true },
    children: [  // 所有页面作为 children
      {
        path: 'main-page',
        name: 'MainPage',
        component: () => import('../views/MainPage.vue'),
        meta: { requiresAuth: true, title: '主页面' }
      },
      {
        path: 'detail/:id',
        name: 'DetailPage',
        component: () => import('../views/DetailPage.vue'),
        meta: { requiresAuth: true, title: '详情页' }
      }
    ]
  }
]
```

Layout.vue 基本结构:

```vue
<template>
  <!-- Portal 嵌入模式：只显示内容 -->
  <div v-if="isInIframe" class="content-only">
    <router-view />
  </div>

  <!-- 独立访问模式：显示完整布局 -->
  <div v-else class="layout">
    <el-header class="header">
      <div class="header-left">
        <h1>模块名称</h1>
      </div>
      <div class="header-right">
        <el-dropdown @command="handleCommand">
          <span class="user-dropdown">
            <el-icon><User /></el-icon>
            {{ authStore.user?.username || '用户' }}
            <el-icon class="el-icon--right"><ArrowDown /></el-icon>
          </span>
          <template #dropdown>
            <el-dropdown-menu>
              <el-dropdown-item command="logout">
                <el-icon><SwitchButton /></el-icon>
                退出登录
              </el-dropdown-item>
            </el-dropdown-menu>
          </template>
        </el-dropdown>
      </div>
    </el-header>

    <el-container class="main-container">
      <el-aside class="sidebar" width="200px">
        <el-menu :default-active="activeMenu" router class="sidebar-menu">
          <el-menu-item index="/main-page">
            <el-icon><Document /></el-icon>
            <span>主页面</span>
          </el-menu-item>
        </el-menu>
      </el-aside>

      <el-main class="content">
        <router-view />
      </el-main>
    </el-container>
  </div>
</template>

<script setup>
import { ref, computed, onMounted } from 'vue'
import { useRouter, useRoute } from 'vue-router'
import { useAuthStore } from '../store/auth'
import { User, ArrowDown, SwitchButton, Document } from '@element-plus/icons-vue'

const router = useRouter()
const route = useRoute()
const authStore = useAuthStore()
const isInIframe = ref(false)

onMounted(() => {
  isInIframe.value = window.self !== window.top
})

// 子页面（详情、表单）激活父级菜单项
const activeMenu = computed(() => {
  const path = route.path
  if (path.startsWith('/main-page')) return '/main-page'
  return path
})

const handleCommand = (command) => {
  if (command === 'logout') {
    authStore.logout()
    router.push('/login')
  }
}
</script>
```

## 前端开发工作流

### 快速开始: Portal + 所有模块

```bash
# 终端 1: 启动 Portal (统一入口)
cd portal/frontend
npm install
npm run dev
# 访问: http://localhost:5170

# 终端 2: 启动 System 前端
cd system/frontend
npm install
npm run dev

# 终端 3: 启动 Manager 前端
cd manager/frontend
npm install
npm run dev

# 现在访问 http://localhost:5170 获得统一体验
# 通过单一 portal 界面访问所有模块
```

### 运行单个前端 (独立模式)

```bash
# System 前端 (端口 5173)
cd system/frontend
npm run dev
# 访问: http://localhost:5173

# Manager 前端 (端口 5174)
cd manager/frontend
npm run dev
# 访问: http://localhost:5174

# Portal (端口 5170)
cd portal/frontend
npm run dev
# 访问: http://localhost:5170
```

### 开发中的前端-后端连接

**所有前端请求统一通过 Gateway (localhost:8000)**，开发和生产环境保持一致：

- vite.config.js 中配置 `/api` proxy 转发到 Gateway (8000)
- API Client 使用默认 `baseURL = '/api'`，无需关心各模块后端端口
- Token 刷新使用 System 后端 (localhost:8180)，由 `createAPIClient` 内部处理

```javascript
// vite.config.js - 所有模块统一配置
proxy: {
  '/api': {
    target: 'http://localhost:8000', // 统一通过 Gateway 访问
    changeOrigin: true
  }
}

// api/client.js - 所有模块统一模式
const client = createAPIClient(() => useAuthStore(), {
  moduleName: 'Manager'  // 无需指定 baseURL，默认为 /api
})
```

### 创建新模块前端

实现新模块 (例如 Meta) 时,遵循以下步骤:

1. **复制前端结构**:

   ```bash
   cp -r system/frontend meta/frontend
   cd meta/frontend
   ```
2. **更新配置**:

   - `package.json`: 将名称更改为 `meta-frontend`
   - `vite.config.js`: 将端口更改为唯一数字 (例如 5175)
   - `index.html`: 更新标题
   - `src/router/index.js`: 将基础路径设置为 `/meta/`，并确保使用嵌套路由结构（Layout 作为父组件）
   - `src/components/Layout.vue`: 更新模块名称和侧边栏菜单项
   - `src/api/client.js`: 将 baseURL 指向 meta 后端 (8082)
   - 保持 `src/api/auth.js` 指向 System 后端 (8180)
3. **配置 common-frontend 别名** (根据模块需求选择):

   对于**无地图功能**的模块 (System, Meta, Transfer):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui': resolve(__dirname, '../../common-frontend/basic/src')
     }
   }
   ```

   对于**有地图功能**的模块 (Manager, Service):
   ```javascript
   // vite.config.js
   resolve: {
     alias: {
       '@': resolve(__dirname, 'src'),
       '@common-ui': resolve(__dirname, '../../common-frontend/basic/src'),
       '@common-ui-map': resolve(__dirname, '../../common-frontend/map/src')
     }
   }

   // package.json - 添加地图依赖
   {
     "dependencies": {
       "ol": "^9.2.4",
       "@amap/amap-jsapi-loader": "^1.0.1"
     }
   }
   ```
4. **更新视图和组件**以匹配模块的功能
5. **添加 Dockerfile 和 nginx.conf** (从 manager/frontend 复制作为模板)
6. **添加到 docker-compose.yml**:

   ```yaml
   meta-frontend:
     build:
       context: ./meta/frontend
     ports:
       - "8092:80"
     profiles:
       - full
   ```

**使用 Common Frontend 组件**:

```vue
<script setup>
// 对于基础模块（所有模块都包含）
import { StorageEngineForm, ImagePreview } from '@common-ui'
import { formatFileSize, FieldType } from '@common-ui'

// 对于启用地图的模块（Manager, Service）
import { TablePreview, GeoJsonPreview, ShapefilePreview } from '@common-ui-map'

const resourceForm = ref({
  resource_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <StorageEngineForm v-model="resourceForm" />
  <TablePreview :data="tableData" />
</template>
```