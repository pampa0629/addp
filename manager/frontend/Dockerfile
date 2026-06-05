FROM localhost:5001/node:18.20.5-alpine AS builder

WORKDIR /repo/addp/manager/frontend

# 复制依赖清单；common-frontend 是 file:../../common-frontend，本阶段只需要 package.json 参与 npm ci
COPY common-frontend/package.json /repo/addp/common-frontend/package.json
COPY manager/frontend/package.json ./package.json
COPY manager/frontend/package-lock.json ./package-lock.json

# 按 lockfile 安装依赖，避免镜像构建时依赖版本漂移
RUN npm ci && npm cache clean --force

# 复制 common-frontend (Vite 别名需要)
COPY common-frontend /repo/addp/common-frontend

# 创建符号链接，让 common-frontend 能访问 node_modules
RUN ln -sfn /repo/addp/manager/frontend/node_modules /repo/addp/common-frontend/node_modules

# 复制构建所需源代码，避免把本地 node_modules/dist/log 带入镜像
COPY manager/frontend/index.html manager/frontend/vite.config.js ./
COPY manager/frontend/src ./src
COPY manager/frontend/public ./public

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /repo/addp/manager/frontend/dist /usr/share/nginx/html

# 复制 nginx 配置
COPY manager/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
