# Portal 页面空白问题修复指南

## 问题描述

**症状**:
- ✅ 登录功能正常
- ✅ 登录后能看到 Portal 首页的四个模块卡片
- ❌ 点击任何模块后,页面变成空白

**根本原因**:
Portal 前端代码硬编码了开发环境的 localhost URL:

```javascript
// 错误的配置 (portal/frontend/src/views/Portal.vue 第207-212行)
const moduleUrls = {
  system: 'http://localhost:5173',    // ❌ 生产环境无法访问
  manager: 'http://localhost:5174',   // ❌ 生产环境无法访问
  meta: 'http://localhost:5175',      // ❌ 生产环境无法访问
  transfer: 'http://localhost:5176'   // ❌ 生产环境无法访问
}
```

当用户点击模块时,Portal 尝试在 iframe 中加载这些 localhost 地址,但在生产服务器上这些地址不存在,导致 iframe 无法加载内容。

## 修复方案

### ✅ 已实施的修复

修改 `portal/frontend/src/views/Portal.vue`,使用环境检测:

```javascript
// 正确的配置 (已修复)
const isDevelopment = import.meta.env.DEV
const moduleUrls = {
  system: isDevelopment ? 'http://localhost:5173' : '/system',
  manager: isDevelopment ? 'http://localhost:5174' : '/manager',
  meta: isDevelopment ? 'http://localhost:5175' : '/meta',
  transfer: isDevelopment ? 'http://localhost:5176' : '/transfer'
}
```

**工作原理**:
- **开发环境** (`npm run dev`): 使用 localhost 端口 (5173, 5174, etc.)
- **生产环境** (`npm run build`): 使用 Nginx 路由的相对路径 (/system, /manager, etc.)

### Nginx 路由配置验证

确认 Nginx 已正确配置路由 ([nginx/nginx.conf](../nginx/nginx.conf)):

```nginx
# System Frontend
location /system/ {
    proxy_pass http://system-frontend/;
}

# Manager Frontend
location /manager/ {
    proxy_pass http://manager-frontend/;
}

# Meta Frontend
location /meta/ {
    proxy_pass http://meta-frontend/;
}

# Transfer Frontend
location /transfer/ {
    proxy_pass http://transfer-frontend/;
}
```

✅ 这些配置已存在于 `nginx/nginx.conf` 中

## 快速修复步骤

### 方法 1: 使用自动化脚本 (推荐)

```bash
# 在开发机上运行
cd /Users/pampa/code/addp

# 执行修复脚本 (重新构建并部署 Portal)
./scripts/fix-portal-blank.sh pampa@192.168.1.182
```

**脚本执行步骤**:
1. 重新构建 Portal 镜像 (AMD64 + ARM64)
2. 推送镜像到本地仓库
3. 创建 multi-arch manifest
4. SSH 到服务器,停止并删除旧的 Portal 容器
5. 拉取并启动新的 Portal 容器
6. 验证修复

**预计耗时**: 约 3-5 分钟

### 方法 2: 手动执行

#### 步骤 1: 在开发机上重新构建 Portal

```bash
cd /Users/pampa/code/addp

# 构建 AMD64 镜像
docker build --platform linux/amd64 \
    -t localhost:5001/addp-portal:latest-amd64 \
    -f portal/frontend/Dockerfile \
    portal/frontend

# 构建 ARM64 镜像
docker build --platform linux/arm64 \
    -t localhost:5001/addp-portal:latest-arm64 \
    -f portal/frontend/Dockerfile \
    portal/frontend

# 推送镜像
docker push localhost:5001/addp-portal:latest-amd64
docker push localhost:5001/addp-portal:latest-arm64

# 创建 multi-arch manifest
docker buildx imagetools create \
    --tag localhost:5001/addp-portal:latest \
    localhost:5001/addp-portal:latest-amd64 \
    localhost:5001/addp-portal:latest-arm64
```

#### 步骤 2: 在服务器上更新 Portal

```bash
# SSH 到服务器
ssh pampa@192.168.1.182

# 进入部署目录
cd ~/addp

# 停止并删除旧的 Portal
docker compose -f docker-compose.prod.yml stop portal
docker compose -f docker-compose.prod.yml rm -f portal

# 拉取新镜像
docker compose -f docker-compose.prod.yml pull portal

# 启动新 Portal
docker compose -f docker-compose.prod.yml up -d portal

# 查看日志
docker compose -f docker-compose.prod.yml logs -f portal
```

## 验证修复

### 1. 浏览器验证

1. 访问 `http://192.168.1.182:8000/`
2. 使用默认账号登录:
   - 用户名: `SuperAdmin`
   - 密码: `20251001#SuperAdmin`
3. 登录后点击任意模块卡片 (系统管理、数据管理、元数据管理、数据传输)
4. **预期结果**: iframe 应该加载对应模块的页面,不再空白

### 2. 浏览器开发者工具验证

按 `F12` 打开开发者工具:

#### Console 检查
应该看到类似的日志:
```
Portal: Menu selected: /system/users
Portal: Parsed - module: system page: users
Portal: Setting iframe URL: /system/users?token=eyJ...
```

**关键点**: iframe URL 应该是相对路径 (`/system/users`),而不是 `http://localhost:5173/users`

#### Network 检查
1. 切换到 Network 标签
2. 点击一个模块
3. 应该看到对 `/system/`, `/manager/` 等的请求
4. 状态码应该是 `200 OK`

#### Elements 检查
检查 iframe 元素:
```html
<iframe src="/system/users?token=..." frameborder="0" class="module-iframe"></iframe>
```

**src 应该是相对路径,不是 localhost URL**

### 3. 服务器日志验证

```bash
ssh pampa@192.168.1.182
cd ~/addp

# 查看 Portal 日志
docker compose -f docker-compose.prod.yml logs --tail=50 portal

# 查看 Nginx 日志 (应该有对 /system, /manager 的请求)
docker compose -f docker-compose.prod.yml logs --tail=50 nginx

# 查看 System Frontend 日志 (应该有请求被路由过来)
docker compose -f docker-compose.prod.yml logs --tail=50 system-frontend
```

## 故障排查

### 问题 1: 修复后页面仍然空白

**可能原因**: 浏览器缓存了旧的 Portal 代码

**解决方案**:
```bash
# 清除浏览器缓存
# Chrome: Ctrl+Shift+Delete → 清除缓存和 Cookie
# 或使用无痕模式: Ctrl+Shift+N

# 强制刷新页面: Ctrl+Shift+R (Windows) 或 Cmd+Shift+R (Mac)
```

### 问题 2: Console 显示 404 错误

**症状**:
```
GET http://192.168.1.182:8000/system/ 404 (Not Found)
```

**原因**: System Frontend 服务未启动

**解决方案**:
```bash
ssh pampa@192.168.1.182
cd ~/addp

# 检查 System Frontend 状态
docker compose -f docker-compose.prod.yml ps system-frontend

# 重启 System Frontend
docker compose -f docker-compose.prod.yml restart system-frontend
```

### 问题 3: iframe 显示认证错误

**症状**: iframe 中显示 "401 Unauthorized" 或自动跳转到登录页

**原因**: Token 传递失败或 System Backend 不可用

**解决方案**:
```bash
ssh pampa@192.168.1.182
cd ~/addp

# 检查 System Backend 状态
docker compose -f docker-compose.prod.yml ps system-backend

# 查看 System Backend 日志
docker compose -f docker-compose.prod.yml logs --tail=100 system-backend

# 重启 System Backend
docker compose -f docker-compose.prod.yml restart system-backend
```

### 问题 4: 构建失败

**症状**: `docker build` 时出现 npm 错误

**解决方案**:
```bash
# 清除 node_modules 并重新安装
cd portal/frontend
rm -rf node_modules package-lock.json
npm install
cd ../..

# 重新构建
docker build --platform linux/amd64 \
    -t localhost:5001/addp-portal:latest-amd64 \
    -f portal/frontend/Dockerfile \
    portal/frontend
```

## 技术细节

### import.meta.env.DEV 说明

Vite 提供的环境变量:
- `import.meta.env.DEV`: `true` in development, `false` in production
- `import.meta.env.PROD`: `true` in production, `false` in development

在开发环境 (`npm run dev`) 时:
- `isDevelopment = true`
- 使用 `http://localhost:5173` 等开发端口

在生产构建 (`npm run build`) 时:
- `isDevelopment = false`
- 使用 `/system`, `/manager` 等相对路径

### 为什么使用相对路径?

1. **跨域问题**: 相对路径请求与主页面同源,避免 CORS
2. **灵活性**: 无需硬编码服务器 IP 或域名
3. **Nginx 路由**: 利用 Nginx 的 location 规则进行智能路由
4. **统一入口**: 所有请求通过同一个端口 (8000),简化防火墙配置

### 请求流程

```
浏览器访问 http://192.168.1.182:8000/
  ↓
Nginx (80 → 8000)
  ↓
Portal (显示首页,点击"系统管理")
  ↓
iframe src="/system/users"
  ↓
浏览器请求 http://192.168.1.182:8000/system/users
  ↓
Nginx location /system/ 规则匹配
  ↓
proxy_pass http://system-frontend/
  ↓
System Frontend 容器 (80 端口)
  ↓
返回 System 模块的 users 页面
  ↓
在 Portal 的 iframe 中显示
```

## 预防措施

### 1. 环境配置规范

为所有前端项目添加环境检测:

```javascript
// 推荐模式
const BASE_URL = import.meta.env.DEV
  ? 'http://localhost:5173'
  : '/system'

// 或使用环境变量
const BASE_URL = import.meta.env.VITE_BASE_URL || '/system'
```

### 2. 构建前检查清单

- [ ] 确认没有硬编码的 localhost URL
- [ ] 确认使用了 `import.meta.env` 环境检测
- [ ] 确认 Nginx 配置了相应的路由规则
- [ ] 本地测试 `npm run build` 后的产物

### 3. 部署前测试

```bash
# 本地构建并测试
cd portal/frontend
npm run build
npx serve dist  # 在本地 3000 端口测试

# 访问 http://localhost:3000
# 应该看到 404 (因为没有后端),但不应该看到 localhost:5173 的请求
```

## 相关文件

- **修复的源文件**: [portal/frontend/src/views/Portal.vue](../portal/frontend/src/views/Portal.vue) (第207-215行)
- **修复脚本**: [scripts/fix-portal-blank.sh](../scripts/fix-portal-blank.sh)
- **Nginx 配置**: [nginx/nginx.conf](../nginx/nginx.conf) (第88-122行)
- **Docker Compose**: [docker-compose.prod.yml](../docker-compose.prod.yml)

## 总结

### 问题
Portal 硬编码了开发环境的 localhost URL,导致生产环境中 iframe 无法加载模块页面

### 解决方案
使用 `import.meta.env.DEV` 环境检测,开发环境用 localhost,生产环境用相对路径

### 快速修复
```bash
./scripts/fix-portal-blank.sh pampa@192.168.1.182
```

### 验证方法
登录 Portal → 点击模块 → 应该正常显示内容 (不再空白)
