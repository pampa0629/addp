ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /app

# 复制 package.json
COPY monitor/frontend/package.json monitor/frontend/package-lock.json ./

RUN npm ci

# 复制 common-frontend (Vite 别名需要)
COPY common-frontend /common-frontend

# 创建符号链接，让 common-frontend 能访问 node_modules
RUN ln -s /app/node_modules /common-frontend/node_modules

# 复制源代码
COPY monitor/frontend/index.html monitor/frontend/vite.config.js ./
COPY monitor/frontend/src ./src

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
