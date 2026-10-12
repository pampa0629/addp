# Transfer 前端

Transfer 模块的前端界面，基于 Vue 3 + Element Plus 开发。

## 功能特性

- ✅ 任务列表 - 查看所有数据传输任务
- ✅ 任务创建/编辑 - 配置数据源和传输规则
- ✅ 任务详情 - 查看任务配置和执行历史
- ✅ 执行监控 - 实时查看任务执行进度
- ✅ 执行日志 - 查看详细的执行日志
- ✅ 统一执行监控 - 从任务页面按 Transfer 范围进入 Monitor

## 技术栈

- **Vue 3** - 渐进式 JavaScript 框架
- **Vue Router** - 官方路由管理器
- **Pinia** - 状态管理
- **Element Plus** - Vue 3 UI 组件库
- **Axios** - HTTP 客户端
- **Vite** - 下一代前端构建工具

## 开发

### 安装依赖

```bash
npm install
```

### 启动开发服务器

```bash
npm run dev
```

访问 http://localhost:5176

### 构建生产版本

```bash
npm run build
```

## Docker 部署

### 构建镜像

```bash
docker build -t addp-transfer-frontend .
```

### 运行容器

```bash
docker run -d -p 8093:80 addp-transfer-frontend
```

访问 http://localhost:8093/transfer/

## 集成到 Console

Transfer 前端可以独立部署，也可以集成到 Console 中通过 iframe 加载。

### Console 集成配置

在 Console 的导航配置中添加：

```javascript
{
  path: '/transfer',
  name: 'Transfer',
  component: () => import('@/views/IframeView.vue'),
  meta: {
    title: '数据传输',
    icon: 'Connection',
    iframeUrl: 'http://localhost:8093/transfer/'
  }
}
```

## 目录结构

```
transfer/frontend/
├── src/
│   ├── api/                # API 接口
│   │   ├── client.js       # Axios 客户端
│   │   └── tasks.js        # 任务 API
│   ├── components/         # 公共组件
│   ├── views/              # 页面视图
│   │   ├── TaskList.vue    # 任务列表
│   │   ├── TaskForm.vue    # 任务表单
│   │   ├── TaskDetail.vue  # 任务详情
│   │   └── ExecutionDetail.vue   # 传输领域执行详情
│   ├── router/             # 路由配置
│   ├── store/              # 状态管理
│   ├── utils/              # 工具函数
│   ├── App.vue             # 根组件
│   └── main.js             # 入口文件
├── public/                 # 静态资源
├── index.html              # HTML 模板
├── vite.config.js          # Vite 配置
├── package.json            # 项目配置
├── Dockerfile              # Docker 配置
└── nginx.conf              # Nginx 配置
```

## API 访问

前端通过共享 API Client 使用统一 `/api/v1` 路径，不维护模块级 `.env` 或 `.env.local`。开发和部署参数统一由仓库根 `.env` 与标准启动脚本注入。

### 向导配置检查

任务向导最后一步提供手动“检查配置”，以保存时同一份完整请求调用 `POST /api/v1/transfer/task-config-checks`。检查需要当前租户的 `transfer.task.create`、`meta.catalog.read`、`system.engine.read` 三项权限；只展示已有结构和引擎事实的报告，不保存、执行任务或扫描数据。报告分别展示 supported、unsupported、unknown、字段配置位置、未检查项和运行时仍需校验项；请求失败独立显示错误。定义受支持不代表配置可执行，检查不改变现有保存行为。配置、用户、租户或所需权限变化后清除报告，并丢弃迟到的旧响应。

标准验证为 `make test-transfer-frontend`，包含单测、受控 API 夹具的浏览器交互和构建。浏览器通过共享启动器分配独立回环端口，使用合成配置，不访问个人登录态或业务服务；Platform CI 的 Transfer 前端矩阵执行同一入口并收集失败证据。

真实只读链路复用手动 Hosted `transfer-relational-sql-etl` suite：普通用户经 Console 登录，在未保存的创建向导中验证 string→string 受支持、string→json 尚无法确定、非法 JSON 默认值不支持。预检段使用当次夹具的已保存源结构，目标为专用合成表名；拦截除预检和正常认证刷新外的修改请求及业务预览，核对任务与执行身份集合不变，取消草稿，并由夹具核对目标表未创建。`system.engine.read` 不允许租户自定义角色持有；预检由独立临时用户通过已有租户基础设施管理员内置角色取得该权限，另赋 `transfer.task.create`、`meta.catalog.read`，仅引用当次合成夹具。原传输用户权限不扩张，不修改生产权限规范。确定性门禁为 `make test-transfer-online-runner`，由现有 `make test-online-runner` 与 Platform CI 聚合；此扩展首次 Hosted 实际通过前，不计为 T4 通过。

## 主要页面

### 任务列表（TaskList.vue）
- 显示所有任务
- 统计卡片（总任务、运行中、成功、失败）
- 搜索和过滤
- 启动/停止/删除操作
- 实时刷新（每5秒）

### 任务表单（TaskForm.vue）
- 创建/编辑任务
- 配置任务基本信息
- 通过资源树选择源和目标资源
- JSON 配置编辑器
- 表单验证

### 任务详情（TaskDetail.vue）
- 显示任务配置
- 查看执行历史
- 启动/停止任务
- 查看执行记录

### 执行详情（ExecutionDetail.vue）
- 显示执行信息
- 实时进度
- 执行日志查看

## 开发规范

### 组件命名
- 使用 PascalCase 命名组件文件
- 使用 kebab-case 命名 HTML 标签

### API 调用
- 使用 `api/tasks.js` 中定义的方法
- 统一错误处理在 axios 拦截器中

### 状态管理
- 使用 Pinia store 管理共享状态
- 组件内状态使用 ref/reactive

### 样式
- 使用 scoped 样式避免污染
- 使用 Element Plus 主题定制

## 常见问题

### 1. API 请求跨域

开发环境已配置 Vite 代理，生产环境使用 Nginx 反向代理。

### 2. Token 认证

Token 存储在 localStorage 中，axios 拦截器自动添加到请求头。

### 3. 页面刷新问题

使用 Vue Router 的 history 模式，Nginx 需要配置 `try_files`。

## 后续优化

- [ ] 添加字段映射可视化编辑器
- [ ] 添加连接器配置向导
- [ ] 添加实时进度图表
- [ ] 添加任务模板功能
- [ ] 添加批量操作功能

---

**版本**: v0.1.0
**最后更新**: 2025-10-21
