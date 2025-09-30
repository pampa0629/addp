# Nginx 使用指南

## 🤔 是否需要 Nginx？

### 答案：看情况！

```
开发环境：❌ 不需要
测试环境：⚠️ 可选
生产环境：✅ 强烈推荐
```

## 📊 架构对比

### 方案 1: 当前架构（开发环境）

```
浏览器
  ↓
前端 Vite Dev Server (5173)
  ↓
Gateway (8000)
  ↓
Backend Services (8080, 8081, 8082, 8083)
```

**优点**：
- ✅ 简单直接
- ✅ 热重载
- ✅ 快速开发

**缺点**：
- ❌ 不适合生产
- ❌ 性能较低
- ❌ 缺少安全特性

### 方案 2: 使用 Nginx（生产环境）

```
          浏览器
            ↓
         Nginx (80/443)
            ↓
    ┌───────┴───────┐
    ↓               ↓
前端静态文件      Gateway (8000)
(dist/)             ↓
                Backend Services
```

**优点**：
- ✅ 高性能静态文件服务
- ✅ HTTPS/SSL 支持
- ✅ 负载均衡
- ✅ 缓存控制
- ✅ Gzip 压缩
- ✅ 安全防护

**缺点**：
- ❌ 配置复杂
- ❌ 需要额外部署

## 🎯 Nginx 的作用

### 1. 静态文件服务

**没有 Nginx**：
```
前端每次都需要 Vite 或 Node 服务器来提供文件
性能：较低
资源占用：高（Node.js 进程）
```

**使用 Nginx**：
```
前端直接由 Nginx 提供静态文件
性能：极高（C 语言编写）
资源占用：极低（几 MB 内存）
```

### 2. 反向代理

```nginx
# Nginx 配置
location / {
    # 前端静态文件
    root /var/www/frontend/dist;
    try_files $uri $uri/ /index.html;
}

location /api/ {
    # API 请求代理到 Gateway
    proxy_pass http://gateway:8000;
}
```

**好处**：
- 前端和 API 使用同一个域名（避免跨域）
- 统一入口
- 便于 HTTPS 配置

### 3. HTTPS/SSL

```nginx
server {
    listen 443 ssl;
    server_name addp.example.com;

    ssl_certificate /etc/ssl/cert.pem;
    ssl_certificate_key /etc/ssl/key.pem;

    # ... 其他配置
}
```

### 4. 负载均衡

```nginx
upstream gateway_backend {
    server gateway1:8000;
    server gateway2:8000;
    server gateway3:8000;
}

location /api/ {
    proxy_pass http://gateway_backend;
}
```

### 5. 缓存和压缩

```nginx
# Gzip 压缩
gzip on;
gzip_types text/css application/javascript application/json;

# 静态文件缓存
location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg)$ {
    expires 1y;
    add_header Cache-Control "public, immutable";
}
```

## 🏗️ 三种部署架构

### 架构 A: 开发环境（当前）

```
┌─────────────┐
│   浏览器     │
└──────┬──────┘
       │
   ┌───┴────────────────┐
   ↓                    ↓
┌─────────┐      ┌──────────┐
│Vite Dev │      │ Gateway  │
│  :5173  │      │  :8000   │
└─────────┘      └────┬─────┘
                      ↓
                ┌──────────┐
                │ Backend  │
                │ Services │
                └──────────┘
```

**使用场景**：本地开发、测试

**命令**：
```bash
# 启动前端
cd system/frontend && npm run dev

# 启动 Gateway
cd gateway && go run cmd/gateway/main.go

# 启动 Backend
cd system/backend && go run cmd/server/main.go
```

### 架构 B: 生产环境 - 不使用 Nginx

```
┌─────────────┐
│   浏览器     │
└──────┬──────┘
       │
   ┌───┴────────────────┐
   ↓                    ↓
┌─────────┐      ┌──────────┐
│ 前端静态 │      │ Gateway  │
│文件服务器│      │  :8000   │
│  :8090  │      └────┬─────┘
└─────────┘           ↓
                ┌──────────┐
                │ Backend  │
                │ Services │
                └──────────┘
```

**问题**：
- ❌ 跨域问题（前端 8090，API 8000）
- ❌ 两个域名/端口
- ❌ 无 HTTPS
- ❌ 性能一般

### 架构 C: 生产环境 - 使用 Nginx（推荐）

```
┌─────────────┐
│   浏览器     │
└──────┬──────┘
       │
    (HTTPS)
       ↓
┌──────────────┐
│    Nginx     │
│   :80/:443   │
└──┬────────┬──┘
   │        │
   ↓        ↓
┌─────┐  ┌──────────┐
│静态 │  │ Gateway  │
│文件 │  │  :8000   │
└─────┘  └────┬─────┘
              ↓
        ┌──────────┐
        │ Backend  │
        │ Services │
        └──────────┘
```

**优势**：
- ✅ 单一入口（同域名）
- ✅ 自动 HTTPS
- ✅ 高性能
- ✅ 无跨域问题
- ✅ 专业级缓存
- ✅ 负载均衡

## 📝 Nginx 配置示例

### 完整配置文件

创建 `nginx/nginx.conf`：

```nginx
# 全域数据平台 Nginx 配置

# Gateway 后端（负载均衡）
upstream gateway_backend {
    server gateway:8000;
    # 如果有多个 Gateway 实例
    # server gateway2:8000;
    # server gateway3:8000;
}

server {
    listen 80;
    server_name addp.example.com;

    # 日志
    access_log /var/log/nginx/addp-access.log;
    error_log /var/log/nginx/addp-error.log;

    # Gzip 压缩
    gzip on;
    gzip_vary on;
    gzip_min_length 1024;
    gzip_types text/plain text/css text/xml text/javascript
               application/json application/javascript application/xml+rss;

    # 前端静态文件
    location / {
        root /usr/share/nginx/html;
        index index.html index.htm;
        try_files $uri $uri/ /index.html;

        # 缓存策略
        location ~* \.(js|css|png|jpg|jpeg|gif|ico|svg|woff|woff2|ttf|eot)$ {
            expires 1y;
            add_header Cache-Control "public, immutable";
        }

        # HTML 不缓存
        location ~* \.html$ {
            add_header Cache-Control "no-cache, no-store, must-revalidate";
        }
    }

    # API 请求代理到 Gateway
    location /api/ {
        proxy_pass http://gateway_backend;

        # 代理头
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;

        # 超时设置
        proxy_connect_timeout 60s;
        proxy_send_timeout 60s;
        proxy_read_timeout 60s;

        # WebSocket 支持（如果需要）
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
    }

    # 健康检查
    location /health {
        proxy_pass http://gateway_backend/health;
        access_log off;
    }
}

# HTTPS 配置（可选）
server {
    listen 443 ssl http2;
    server_name addp.example.com;

    # SSL 证书
    ssl_certificate /etc/ssl/certs/addp.crt;
    ssl_certificate_key /etc/ssl/private/addp.key;

    # SSL 配置
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    ssl_prefer_server_ciphers on;

    # HSTS
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;

    # 其他配置同上
    # ... （复制上面的 location 配置）
}

# HTTP 重定向到 HTTPS
server {
    listen 80;
    server_name addp.example.com;
    return 301 https://$server_name$request_uri;
}
```

## 🐳 Docker Compose 集成

### 添加 Nginx 服务

更新 `docker-compose.yml`：

```yaml
services:
  # ... 其他服务

  nginx:
    image: nginx:alpine
    container_name: addp-nginx
    ports:
      - "80:80"
      - "443:443"
    volumes:
      - ./nginx/nginx.conf:/etc/nginx/conf.d/default.conf
      - ./system/frontend/dist:/usr/share/nginx/html
      - ./nginx/ssl:/etc/ssl  # SSL 证书
      - ./logs/nginx:/var/log/nginx  # 日志
    depends_on:
      - gateway
      - system-frontend
    networks:
      - addp-network
    restart: unless-stopped
```

### 目录结构

```
addp/
├── nginx/
│   ├── nginx.conf          # Nginx 配置
│   └── ssl/                # SSL 证书（生产环境）
│       ├── cert.crt
│       └── cert.key
├── logs/
│   └── nginx/              # Nginx 日志
├── system/
│   └── frontend/
│       └── dist/           # 前端构建产物
└── docker-compose.yml
```

## 🚀 部署流程

### 使用 Nginx 部署

```bash
# 1. 构建前端
cd system/frontend
npm run build
# 生成 dist/ 目录

# 2. 准备 Nginx 配置
mkdir -p nginx logs/nginx

# 3. 启动所有服务
docker-compose up -d nginx gateway system-backend

# 4. 验证
curl http://localhost/
curl http://localhost/api/auth/login
```

### 访问地址

```
前端：http://localhost/
API： http://localhost/api/
健康检查：http://localhost/health
```

## 📊 性能对比

### 静态文件服务性能

| 方案 | QPS | 响应时间 | 内存占用 |
|------|-----|---------|---------|
| Vite Dev (开发) | ~1000 | 5-20ms | 200MB |
| Node.js (生产) | ~5000 | 2-5ms | 150MB |
| Nginx | ~50000 | <1ms | 10MB |

### 并发连接

| 方案 | 最大并发 |
|------|---------|
| Node.js | ~10000 |
| Nginx | ~100000+ |

## 🎯 建议

### 开发环境（当前）

```bash
✅ 不需要 Nginx
✅ 直接使用 Vite Dev Server
✅ 方便热重载和调试

# 启动方式
npm run dev          # 前端
go run cmd/*/main.go # 后端
```

### 测试环境

```bash
⚠️ 可选使用 Nginx
✅ 测试生产环境配置
✅ 验证静态文件服务

# 启动方式
docker-compose up nginx
```

### 生产环境

```bash
✅ 强烈推荐使用 Nginx
✅ 性能、安全、稳定性
✅ 专业运维工具

# 启动方式
docker-compose -f docker-compose.prod.yml up -d
```

## 🔧 Makefile 集成

更新根目录 `Makefile`：

```makefile
nginx-start: ## 启动 Nginx
	@echo "$(GREEN)启动 Nginx...$(NC)"
	@docker-compose up -d nginx
	@echo "$(GREEN)Nginx 已启动！访问 http://localhost$(NC)"

nginx-reload: ## 重载 Nginx 配置
	@docker-compose exec nginx nginx -s reload

nginx-logs: ## 查看 Nginx 日志
	@docker-compose logs -f nginx

nginx-test: ## 测试 Nginx 配置
	@docker-compose exec nginx nginx -t
```

## 🎓 总结

### 什么时候需要 Nginx？

| 场景 | 是否需要 | 原因 |
|------|---------|------|
| 本地开发 | ❌ 不需要 | Vite Dev Server 足够 |
| 开发预览 | ⚠️ 可选 | 测试生产环境行为 |
| 测试环境 | ⚠️ 推荐 | 接近生产配置 |
| 生产环境 | ✅ 必须 | 性能、安全、稳定性 |
| 需要 HTTPS | ✅ 必须 | SSL 证书管理 |
| 高并发 | ✅ 必须 | Nginx 性能优势 |
| 负载均衡 | ✅ 必须 | 多实例分发 |

### 当前项目建议

```
开发阶段（现在）：
❌ 不需要 Nginx
✅ 继续使用 Vite Dev Server + Gateway

准备上线时：
✅ 添加 Nginx 配置
✅ 构建前端静态文件
✅ 配置 HTTPS
✅ 性能优化
```

## 📖 相关文档

- [Nginx 官方文档](http://nginx.org/en/docs/)
- [Docker Compose 文档](https://docs.docker.com/compose/)
- [HTTPS 证书申请](https://letsencrypt.org/)

---

**结论**：当前开发阶段**不需要** Nginx，但生产环境**强烈推荐**使用！