# Nginx 配置说明

## 配置文件

- `nginx.conf` - Nginx 主配置文件

## 使用方式

### 方式 1: Docker Compose（推荐）

在根目录的 `docker-compose.yml` 中已配置 nginx 服务（使用 profile）：

```bash
# 启动包含 Nginx 的完整环境
docker-compose --profile nginx up -d

# 或者单独启动 Nginx
docker-compose up -d nginx
```

### 方式 2: 独立 Docker 容器

```bash
# 1. 构建前端
cd system/frontend
npm run build

# 2. 启动 Nginx
docker run -d \
  --name addp-nginx \
  -p 80:80 \
  -v $(pwd)/nginx/nginx.conf:/etc/nginx/conf.d/default.conf \
  -v $(pwd)/system/frontend/dist:/usr/share/nginx/html \
  --network addp-network \
  nginx:alpine
```

### 方式 3: 本地 Nginx

如果系统已安装 Nginx：

```bash
# macOS
brew install nginx

# Ubuntu/Debian
sudo apt install nginx

# 复制配置
sudo cp nginx/nginx.conf /usr/local/etc/nginx/servers/addp.conf

# 测试配置
sudo nginx -t

# 重载配置
sudo nginx -s reload
```

## 配置说明

### 静态文件路径

```nginx
location / {
    root /usr/share/nginx/html;  # 前端文件位置
    try_files $uri $uri/ /index.html;
}
```

### API 代理

```nginx
location /api/ {
    proxy_pass http://gateway_backend;  # 代理到 Gateway
}
```

### 缓存策略

```nginx
# HTML - 不缓存
location ~* \.html$ {
    add_header Cache-Control "no-cache";
}

# 静态资源 - 缓存 1 年
location ~* \.(js|css|png|jpg)$ {
    expires 1y;
}
```

## 验证配置

```bash
# 测试配置文件语法
nginx -t

# 查看 Nginx 版本
nginx -v

# 查看配置详情
nginx -T
```

## 常见问题

### 1. 端口已被占用

```bash
# 查看占用 80 端口的进程
lsof -i :80

# 修改配置文件中的端口
listen 8080;  # 改为其他端口
```

### 2. 权限问题

```bash
# 确保 Nginx 用户有权限访问文件
chmod -R 755 system/frontend/dist
```

### 3. 日志查看

```bash
# Docker 容器日志
docker logs addp-nginx

# 本地 Nginx 日志
tail -f /var/log/nginx/addp-access.log
tail -f /var/log/nginx/addp-error.log
```

## HTTPS 配置

### 使用 Let's Encrypt 免费证书

```bash
# 安装 certbot
brew install certbot  # macOS
sudo apt install certbot  # Ubuntu

# 获取证书
sudo certbot certonly --nginx -d addp.example.com

# 证书会保存在
/etc/letsencrypt/live/addp.example.com/fullchain.pem
/etc/letsencrypt/live/addp.example.com/privkey.pem
```

### 配置 HTTPS

取消 `nginx.conf` 中 HTTPS 部分的注释，并更新证书路径。

## 性能优化

### 启用 Gzip 压缩

```nginx
gzip on;
gzip_types text/css application/javascript application/json;
```

### 启用 HTTP/2

```nginx
listen 443 ssl http2;
```

### 调整工作进程

```nginx
# nginx.conf 顶部添加
worker_processes auto;
worker_connections 1024;
```

## 监控

### 启用状态页面

```nginx
location /nginx_status {
    stub_status on;
    access_log off;
    allow 127.0.0.1;
    deny all;
}
```

访问 `http://localhost/nginx_status` 查看状态。

## 相关文档

- [Nginx 配置指南](../docs/NGINX_GUIDE.md) - 详细说明
- [根目录 README](../README.md) - 项目整体文档
- [Gateway 架构](../gateway/ARCHITECTURE.md) - Gateway 说明