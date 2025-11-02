# 构建阶段
FROM node:18-alpine AS builder

WORKDIR /app

# 复制 common-frontend（依赖）
COPY common-frontend /app/common-frontend

# 复制 transfer frontend
COPY transfer/frontend/package*.json ./transfer/frontend/
WORKDIR /app/transfer/frontend
RUN npm install

# 复制源代码
COPY transfer/frontend/ ./

# 构建
RUN npm run build

# 生产阶段
FROM nginx:alpine

# 复制构建产物到 nginx html 根目录
COPY --from=builder /app/transfer/frontend/dist /usr/share/nginx/html

# 复制 nginx 配置
COPY transfer/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
