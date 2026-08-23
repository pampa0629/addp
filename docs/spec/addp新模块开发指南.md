## 新服务的开发指南

## ⚠️ 关键原则（必读）

新模块开发必须遵循以下原则，确保与现有模块的一致性：

1. **配置加载统一性**
   - ✅ 必须使用 `common/config.LoadEnv()` 加载环境变量
   - ✅ 必须继承 `commonConfig.BaseConfig`
   - ❌ 禁止硬编码 `.env` 文件路径（如 `godotenv.Load("../../.env")`）
   - ❌ 禁止重复定义通用配置字段

2. **代码复用原则**
   - ✅ 通用功能必须提取到 `common/` 或 `common-frontend/`
   - ✅ 参考现有模块的实现模式（推荐：manager、meta）
   - ❌ 禁止在新模块中重复实现已有的通用功能

3. **Schema 隔离原则**
   - ✅ 每个模块使用独立的 PostgreSQL schema
   - ✅ 在代码中自动创建 schema（不依赖初始化脚本）
   - ✅ DSN 连接字符串必须包含 `search_path` 参数

4. **脚本集成原则**
   - ✅ 新模块必须同步修改 `start.sh`、`restart.sh`、`detect-common.sh`
   - ✅ 支持独立启动模式（`-your-module`）
   - ✅ 支持全量启动模式（默认行为）

5. **国际化原则**
   - ✅ 新模块必须按 [ADDP 国际化开发规范](addp国际化开发规范.md) 创建前后端翻译文件
   - ✅ 前端用户可见文本必须使用 Vue I18n，不得硬编码中文或英文
   - ✅ 后端用户可见错误消息必须通过 `common/middleware/i18n` 翻译
   - ✅ Swagger 注解必须使用 `中文 | English` 双语格式

6. **Console 页面描述原则**
   - ✅ 侧边栏固定菜单页由 Console 自动记录最近访问；若侧边栏短标签依赖模块上下文，必须在 `SIDEBAR_MENUS` 中另设可独立识别的 `recentLabel`
   - ✅ 稳定动态页面在业务对象加载后使用 `useConsolePageDescriptor()` 声明页面标题和对象名称
   - ✅ 页面标题和对象名称必须国际化，并在语言切换后自动更新
   - ✅ 页面描述的 `title` 必须是脱离当前模块和页面上下文后仍可独立识别的业务对象类型，如“传输任务”“质量检查执行”；“任务详情”“执行详情”“服务信息”等局部页面标题不得直接复用
   - ✅ 页面描述的 `subject` 只承载具体对象名称、标题或业务编码；不得依赖对象名称中的偶然动词来补足 `title` 语义
   - ❌ Console 不解析模块动态路由，不跨模块查询业务对象
   - ❌ 创建、编辑、登录、OAuth 回调等临时页面不得进入最近访问

7. **启动依赖边界**
   - ✅ Backend 和附属 Worker 必须支持零 Engine Instance 启动并进入各自正常空闲状态
   - ✅ 模块可以把自身必需 Infra 作为启动或 readiness 条件；Infra 不属于 Engine Instance 启动解耦范围
   - ✅ 引擎缺失、离线或能力不匹配只失败实际使用该引擎的请求或 execution
   - ✅ Engine Runtime 在自身服务就绪后异步自注册，注册失败不得阻塞 Runtime readiness
   - ❌ 模块启动、健康检查或构造函数不得查询、连接、等待或隐式拉起可选 Engine Instance / Engine Runtime

**违反以上原则的代码将无法通过 Code Review。**

---

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
   - 使用 GORM 作为 ORM；数据库初始化只能由模块自身的启动迁移路径负责
   - 在 `scripts/infra/init-postgresql.sql` 登记模块 schema 与注释，表、约束和索引由模块迁移创建
   - 使用 `updated_at` 触发器进行时间戳跟踪

3. **配置管理**（必须遵循）:

   **核心原则**: 所有新模块**必须**使用 `common/config` 统一配置加载器，禁止自行加载 .env 文件。

   **正确的配置结构**:
   ```go
   // internal/config/config.go
   package config

   import (
       "fmt"
       "os"
       commonConfig "github.com/addp/common/config"
   )

   type Config struct {
       commonConfig.BaseConfig  // 继承通用配置 (DB、JWT、加密密钥等)

       // 模块特有配置
       Port     string
       DBSchema string

       // Redis 配置
       RedisHost     string
       RedisPort     string
       RedisPassword string
       RedisDB       int

       // 其他模块 URL
       SystemURL          string
       ServiceClientSecret string
   }

   // LoadConfig 加载配置
   func LoadConfig() (*Config, error) {
       // ✅ 正确：使用 common/config 自动发现并加载 .env
       commonConfig.LoadEnv()

       // ❌ 错误：禁止硬编码相对路径
       // godotenv.Load("../../.env")  // 不要这样做！

       cfg := &Config{
           Port:     commonConfig.GetEnv("MODULE_BACKEND_PORT", "8110"),
           DBSchema: "module_name",  // 使用模块名作为 schema

           RedisHost:     commonConfig.GetEnv("REDIS_HOST", "localhost"),
           RedisPort:     commonConfig.GetEnv("REDIS_PORT", "16379"),
           RedisPassword: os.Getenv("REDIS_PASSWORD"),
           RedisDB:       commonConfig.GetEnvInt("REDIS_DB", 0),

           SystemURL:           commonConfig.GetEnv("SYSTEM_URL", "http://localhost:8180"),
           ServiceClientSecret: os.Getenv("MODULE_SERVICE_CLIENT_SECRET"),
       }

       commonConfig.LoadDeploymentConfig(&cfg.BaseConfig)

       cfg.BaseConfig.SystemServiceURL = cfg.SystemURL

       return cfg, nil
   }

   // GetDatabaseDSN 获取数据库连接字符串（支持 schema 隔离）
   func (c *Config) GetDatabaseDSN() string {
       return fmt.Sprintf(
           "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
           c.DBHost,    // 来自 BaseConfig
           c.DBPort,    // 来自 BaseConfig
           c.DBUser,    // 来自 BaseConfig
           c.DBPassword,// 来自 BaseConfig
           c.DBName,    // 来自 BaseConfig
           c.DBSchema,  // 模块特有配置
       )
   }
   ```

   **关键点**:
   - ✅ 使用 `commonConfig.LoadEnv()` 自动发现项目根目录的 .env 文件
   - ✅ 继承 `commonConfig.BaseConfig` 复用通用配置字段
   - ✅ 使用 `commonConfig.GetEnv()` 等辅助函数获取环境变量
   - ✅ Bootstrap 部署配置只从根 `.env` 或进程环境读取
   - ✅ DSN 包含 `search_path` 参数实现 schema 隔离
   - ❌ 禁止硬编码 `godotenv.Load("../../.env")`
   - ❌ 禁止重复定义 BaseConfig 中已有的字段

   **配置来源**:
   1. 端口、数据库连接、基础设施地址和 Secret 从根 `.env` 或进程环境读取
   2. owner 普通运行配置从 owner 自己的持久化配置读取
   3. 不允许从 System 共享配置 API 获取 Secret，也不允许环境变量 fallback 双轨

4. **认证**:

	- 复用 `common/middleware/auth` 调用 System AuthContext API
	- 从 AuthContext 读取 Principal、会话模式、唯一当前 Tenant、Scope 和必要授权事实并传递给服务层
	- 使用稳定 Permission 执行功能授权，禁止新增 `user_type` helper 或角色名称硬编码分支
	- 不读取 `JWT_SECRET`，不在模块内自行解析用户 Token
5. **国际化集成**:

   - 注册 `common/middleware/i18n.I18nMiddleware()`
   - 创建 `<module>/backend/i18n/i18n.go`
   - 创建 `<module>/backend/i18n/locales/zh-cn.toml` 和 `en.toml`
   - Handler 中的用户可见错误消息使用 `commoni18n.T(c, modulei18n.MsgXxx)`
   - Swagger 注解使用 `中文 | English` 格式

   详细要求参见 [ADDP 国际化开发规范](addp国际化开发规范.md)。

6. **Docker 集成**:

   - 在服务根目录创建 Dockerfile
   - 使用 `profile: full` 将服务添加到 `docker-compose.yml`
   - 使用健康检查进行依赖管理
   - 连接到 `addp-network` 进行服务间通信
   - `depends_on` 和健康检查只登记模块自身必需 Infra 与真实控制面依赖；不得为了可选功能把 Engine Runtime 设为 Backend/Worker 启动条件

7. **开发脚本集成**（新模块必做）:

   新模块需要修改开发脚本以支持独立启动和重启：

   **a. 修改 scripts/dev/start.sh**:

   1. 添加模块启动标志（约第167行）:
      ```bash
      START_YOUR_MODULE_BACKEND=false
      START_YOUR_MODULE_FRONTEND=false
      ```

   2. 添加到帮助信息（约第19行）:
      ```bash
      echo "  -your-module  启动 YourModule 模块 (公共依赖: System Backend + Meta Backend/Worker + Gateway + Console)"
      ```

   3. 添加到参数解析（约第135行）:
      ```bash
      -system|-manager|-meta|-transfer|-your-module|...)
      ```

   4. 添加全量启动逻辑（约第199行）:
      ```bash
      START_YOUR_MODULE_BACKEND=true
      START_YOUR_MODULE_FRONTEND=true
      ```

   5. 添加模块启动逻辑（参考其他模块的 case 分支）。只写模块本体和真实额外依赖；System Backend、Meta Backend、Meta Worker、Gateway 和 Console 由 `enable_single_module_common_dependencies` 统一处理，不要在每个分支重复写：
      ```bash
      your-module)
        START_YOUR_MODULE_BACKEND=true
        START_YOUR_MODULE_FRONTEND=true
        ;;
      ```

      模块分支不得隐式设置 `START_*_ENGINE`、`START_*_WORKFLOW`、`START_DUCKDB_RUNTIME` 或 `START_INFERENCE_BACKEND`。Engine Runtime 只由它自己的显式启动参数或全量部署组合选择。

   6. 添加编译逻辑（约第690行）:
      ```bash
      if [ "$START_YOUR_MODULE_BACKEND" = true ]; then
        build_service "your-module" "your-module/backend" &
      fi
      ```

   7. 添加启动逻辑（约第805行）:
      ```bash
      if [ "$START_YOUR_MODULE_BACKEND" = true ]; then
        if check_service_running "your-module" "8110"; then
          .dev-bins/addp-your-module > logs/your-module-backend.log 2> logs/your-module-backend-stderr.log &
          YOUR_MODULE_PID=$!
          echo $YOUR_MODULE_PID > .dev-pids/your-module.pid
        fi
      fi
      ```

   8. 添加前端配置（约第1764行）:
      ```bash
      if [ "$START_YOUR_MODULE_FRONTEND" = true ]; then
        FRONTEND_CONFIGS+=("your-module:${YOUR_MODULE_FE_PORT}:your-module/frontend")
      fi
      ```

   **b. 修改 scripts/dev/restart.sh**:

   1. 添加到帮助信息（第6行）:
      ```bash
      echo "用法: $0 [-all] [-system] ... [-your-module] ..."
      ```

   2. 添加到帮助选项列表（第20行）:
      ```bash
      echo "  -your-module     强制重新编译 YourModule 模块"
      ```

   3. 添加到参数解析（第64行）:
      ```bash
      -system|-manager|-meta|-your-module|-...)
      ```

   **c. 修改 scripts/utils/detect-common.sh**（如果模块依赖 common）:

   添加到 `GO_MODULES` 数组:
   ```bash
   GO_MODULES=(
     "system/backend"
     "manager/backend"
     ...
     "your-module/backend"
   )
   ```

   **验证步骤**:
   ```bash
   # 测试单模块启动；应同时带起公共依赖
   bash scripts/dev/start.sh -your-module

   # 测试重启
   bash scripts/dev/restart.sh -your-module

   # 测试全量启动
   bash scripts/dev/start.sh
   ```

8. **前端集成**:

   - 创建独立的 `<module>/frontend/` 目录
   - 从 `system/frontend/` 复制结构 (Vue 3 + Pinia + Element Plus)
   - 创建 `<module>/frontend/src/i18n/zh-cn.json` 和 `en.json`
   - 使用 `common-frontend/basic` 的 `createAddpI18n()` 初始化 Vue I18n
   - iframe 模块启用 Console 语言切换监听，请求统一携带 `Accept-Language`
   - Element Plus locale 必须跟随 ADDP 当前语言
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
	       EngineType     string                 `json:"engine_type" binding:"required"`
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

数据库结构由 owner 模块自身的启动迁移路径管理，完成后才能开放 API。新增模块应沿用其模块确定的唯一迁移机制，并提供模块级 migration 测试及对应的根 `Makefile`/CI 标准入口；禁止另建根目录集中 SQL、手工集中迁移入口或第二套并行迁移路线。

采用版本化 migration 的模块必须新增向前 migration，不得修改已经发布的 migration；仍采用 `AutoMigrate` 的模块不得再叠加手工 SQL 迁移入口。迁移机制的全平台统一应作为独立架构专题推进，不能在单个模块变更中局部宣称完成。

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
  - Console 嵌入模式：通过 `isInIframe` 检测，仅显示 `<router-view>`
- **路由结构**: 使用嵌套路由，Layout 作为父组件包裹所有需要认证的页面
- **主题适配**（必须）: Layout.vue 中的背景色、边框色**禁止使用硬编码颜色**，必须使用 CSS 变量：
  ```css
  /* ✅ 正确 - 使用 CSS 变量 */
  .header  { background: var(--addp-bg-primary) !important; border-bottom: 1px solid var(--addp-border-color); }
  .sidebar { background: var(--addp-bg-primary) !important; border-right:  1px solid var(--addp-border-color); }
  .content { background: var(--addp-bg-secondary) !important; }
  .content-only { background: var(--addp-bg-secondary) !important; }

  /* ❌ 错误 - 硬编码颜色，切换主题时无法响应 */
  .header  { background: #fff; border-bottom: 1px solid #e4e7ed; }
  .sidebar { border-right: 1px solid #e4e7ed; }
  .content { background: #f5f7fa; }
  ```

> 详细规范参见 [common-frontend/docs/addp前端风格设计规范.md](../../common-frontend/docs/addp前端风格设计规范.md)

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
  <!-- Console 嵌入模式：只显示内容 -->
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

### 快速开始: Console + 所有模块

```bash
# 终端 1: 启动 Console (控制台)
cd console/frontend
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
# 通过单一 console 界面访问所有模块
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

# Console (端口 5170)
cd console/frontend
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
   - `src/components/Layout.vue`: 更新模块名称和侧边栏菜单项，**背景色必须使用 CSS 变量**（见下方要求）
   - `src/api/client.js`: 将 baseURL 指向 meta 后端 (8082)
   - 保持 `src/api/auth.js` 指向 System 后端 (8180)
   - **`src/main.js`: 必须添加主题初始化**（否则无法跟随 Console 切换主题）:
     ```javascript
     // 必须导入 Element Plus 深色模式 CSS（在 element-plus/dist/index.css 之后）
     import 'element-plus/theme-chalk/dark/css-vars.css'
     // 必须导入统一主题 CSS
     import '@common-ui/styles/theme.css'
     // 导入主题管理
     import { useTheme } from '@common-ui'

     // 在 app.use() 之后、app.mount() 之前初始化
     const { init: initTheme } = useTheme({ listenToConsole: true, storageKey: 'theme-mode' })
     initTheme()
     app.mount('#app')
     ```
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
import { StorageEngineForm } from '@common-ui'
import { formatFileSize, FieldType } from '@common-ui'

// 对于启用文件预览的模块（Manager）
import { ImagePreview } from '@common-ui/previews'

// 对于启用地图的模块（Manager, Service）
import { TablePreview, GeoJsonPreview } from '@common-ui-map'

const resourceForm = ref({
  engine_type: 'postgresql',
  name: '',
  connection_info: {}
})
</script>

<template>
  <StorageEngineForm v-model="resourceForm" />
  <TablePreview :data="tableData" />
</template>
```

## 常见错误和排查

### 错误 1: ".env file not found"

**现象**:
```
Warning: .env file not found: open ../../.env: no such file or directory
```

**原因**: 硬编码了相对路径 `godotenv.Load("../../.env")`

**解决方案**:
```go
// ❌ 错误
godotenv.Load("../../.env")

// ✅ 正确
commonConfig.LoadEnv()  // 自动发现项目根目录
```

### 错误 2: "schema does not exist"

**现象**:
```
Failed to migrate database: ERROR: schema "your_module" does not exist (SQLSTATE 3F000)
```

**原因**: 未在代码中创建 schema

**解决方案**:
先在 `scripts/infra/init-postgresql.sql` 登记 owner schema，再确认模块唯一的启动迁移路径会在 repository 和 API 初始化前完成。

### 错误 3: 重启脚本不识别新模块

**现象**:
```
❌ 未知参数: -your-module
```

**原因**: 未在 `restart.sh` 和 `start.sh` 中添加模块支持

**解决方案**: 参考"开发脚本集成"章节完成脚本修改

### 错误 4: 配置字段重复定义

**现象**: 代码中同时定义了 `DatabaseHost`、`DatabasePort` 等字段，与 `BaseConfig` 重复

**解决方案**:
```go
// ❌ 错误：重复定义
type Config struct {
    DatabaseHost string
    DatabasePort string
    DatabaseUser string
    DatabasePassword string
    DatabaseName string
    // ...
}

// ✅ 正确：继承 BaseConfig
type Config struct {
    commonConfig.BaseConfig  // 包含 DBHost、DBPort、DBUser、DBPassword、DBName 等
    Port     string          // 模块特有配置
    DBSchema string
}
```

### 错误 5: DSN 缺少 search_path 参数

**现象**: 表创建到了 public schema 而不是模块专用 schema

**解决方案**:
```go
// ❌ 错误：缺少 search_path
func (c *Config) GetDatabaseDSN() string {
    return fmt.Sprintf(
        "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable",
        c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName,
    )
}

// ✅ 正确：包含 search_path
func (c *Config) GetDatabaseDSN() string {
    return fmt.Sprintf(
        "host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
        c.DBHost, c.DBPort, c.DBUser, c.DBPassword, c.DBName, c.DBSchema,
    )
}
```

### 错误 6: 模块启动但未注册到 Gateway

**现象**: 模块服务正常运行，但 Gateway 无法路由请求

**原因**: 未向 System 服务注册模块

**解决方案**:
在 `cmd/server/main.go` 中添加模块注册逻辑：
```go
// 创建模块独立的 OAuth Service Token Source 和 System Service Client
serviceTokenSource, err := commonClient.NewOAuthServiceTokenSource(
    cfg.SystemURL, "addp-your-module", cfg.ServiceClientSecret, nil,
)
if err != nil {
    log.Fatalf("Service Token Source 初始化失败: %v", err)
}
systemClient := commonClient.NewSystemServiceClient(cfg.SystemURL, serviceTokenSource, nil)

// 使用 Platform Service Access Token 注册模块并维持心跳
serviceURL := fmt.Sprintf("http://localhost:%s", cfg.Port)
systemClient.RegisterAndHeartbeat(context.Background(), &commonClient.ModuleRegistrationRequest{
    ModuleName:     "your-module",
    ModuleURL:      serviceURL,
    RoutePrefix:    "/your-module",
    HealthCheckURL: serviceURL + "/health",
    Metadata: map[string]interface{}{
        "module": "your-module",
    },
})
```

### 错误 7: 前端 API 路径包含重复的 /api 前缀

**现象**:
```
GET http://localhost:5181/api/api/model/domains 503 (Service Unavailable)
```

**原因**: `createAPIClient` 的 baseURL 默认是 `/api`，如果 API 函数的路径又以 `/api/` 开头，就会拼接成 `/api/api/...`

**规范**: 前端 API 函数中的路径**只写模块前缀以后的部分**，不加 `/api`

```javascript
// ❌ 错误：多写了 /api
export const domainAPI = {
  list() {
    return client.get('/api/model/domains')  // 实际请求变成 /api/api/model/domains
  }
}

// ✅ 正确：路径从模块名开始
export const domainAPI = {
  list() {
    return client.get('/model/domains')  // 实际请求是 /api/model/domains
  }
}
```

**完整路径链路说明**:
```
前端 API 函数     →  baseURL + path  →  Gateway 接收   →  转发到后端
client.get('/model/domains')  →  /api/model/domains  →  后端 /api/model/domains
```

**参考其他模块的正确写法**:
```javascript
// manager 模块
client.get('/manager/engines')          // ✅ 正确

// meta 模块
client.get('/meta/datasources')         // ✅ 正确

// model 模块
client.get('/model/domains')            // ✅ 正确
```

### 错误 8: 前端模块不跟随 Console 主题切换

**现象**: 切换 Console 主题（深色/蓝色/紫色等）时，模块前端的背景和边框颜色不变。

**原因有两处**：

1. **`main.js` 缺少主题初始化**: 未导入主题 CSS 或未调用 `useTheme({ listenToConsole: true })`
2. **`Layout.vue` 使用硬编码颜色**: 背景/边框使用 `#fff`、`#f5f7fa`、`#e4e7ed` 等固定值而非 CSS 变量

**修复 main.js**:
```javascript
import 'element-plus/theme-chalk/dark/css-vars.css'  // 添加
import '@common-ui/styles/theme.css'                  // 添加
import { useTheme } from '@common-ui'                 // 添加

// app.use() 之后
const { init: initTheme } = useTheme({ listenToConsole: true, storageKey: 'theme-mode' })
initTheme()
app.mount('#app')
```

**修复 Layout.vue**:
```css
/* 将所有硬编码颜色替换为 CSS 变量 */
.header       { background: var(--addp-bg-primary) !important;   border-bottom: 1px solid var(--addp-border-color); }
.sidebar      { background: var(--addp-bg-primary) !important;   border-right:  1px solid var(--addp-border-color); }
.content      { background: var(--addp-bg-secondary) !important; }
.content-only { background: var(--addp-bg-secondary) !important; }
```

> 完整的主题变量列表和设计规范参见 [common-frontend/docs/addp前端风格设计规范.md](../../common-frontend/docs/addp前端风格设计规范.md)

## 检查清单

开发新模块时，使用此清单确保所有步骤完成：

**后端开发**:
- [ ] 复制并调整目录结构 (`backend/cmd/server/`, `backend/internal/`)
- [ ] 配置使用 `commonConfig.LoadEnv()` 和 `BaseConfig`
- [ ] DSN 包含 `search_path` 参数
- [ ] 在 `init-postgresql.sql` 中登记 owner schema
- [ ] 模块唯一的启动迁移路径配置正确，并有 migration 测试和 CI 入口
- [ ] 实现健康检查端点 (`/health`)
- [ ] 添加模块注册和心跳逻辑

**前端开发**:
- [ ] 复制并调整前端目录结构
- [ ] 配置唯一端口号
- [ ] 配置路由基础路径
- [ ] 实现 Layout 组件（支持双模式）
- [ ] **Layout.vue 背景/边框使用 CSS 变量（`var(--addp-bg-primary/secondary)`，非硬编码颜色）**
- [ ] **main.js 导入主题 CSS 并调用 `useTheme({ listenToConsole: true })`**
- [ ] 配置 common-frontend 别名
- [ ] API Client 正确配置
- [ ] API 路径不含 `/api` 前缀（格式：`/module-name/resource`）

**脚本集成**:
- [ ] 修改 `scripts/dev/start.sh`（参数、启动标志、全量启动、模块 case、编译、启动、前端配置；单模块公共依赖不要重复写）
- [ ] 修改 `scripts/dev/restart.sh` (3个位置)
- [ ] 修改 `scripts/utils/detect-common.sh`
- [ ] 根 `Makefile` 能自动发现或显式提供模块测试入口，`.github/workflows/` 的路径选择能命中该模块
- [ ] 正式服务、Worker、前端、Python 包、PostgreSQL 门禁和 Dockerfile 已通过 `make test-platform` 的登记完整性检查
- [ ] 运行 `make test-module MODULE=<模块>`；提交前再运行 `make test-changed`
- [ ] 验证独立启动 (`-your-module`)
- [ ] 验证全量启动

**文档和配置**:
- [ ] 在 `init-postgresql.sql` 中登记并注释 owner schema
- [ ] 在 `.env` 中添加模块端口配置
- [ ] 创建模块的 `CLAUDE.md` 文档
- [ ] 更新根目录 `CLAUDE.md` 的模块列表

**测试验证**:
- [ ] 模块独立启动成功
- [ ] 健康检查通过
- [ ] 数据库表创建在正确的 schema
- [ ] 模块注册到 Gateway 成功
- [ ] 前端可以访问后端 API
- [ ] Console 可以嵌入模块前端
- [ ] **切换 Console 主题，模块前端背景/边框随之变化**
