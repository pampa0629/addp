ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /repo/addp/orchestrator/frontend

# 复制 package.json
COPY common-frontend/package.json /repo/addp/common-frontend/package.json
COPY orchestrator/frontend/package.json orchestrator/frontend/package-lock.json ./

RUN npm ci

# 复制 common-frontend (用于内部依赖解析)
COPY common-frontend /repo/addp/common-frontend

# 共享源码复用模块依赖；本地包链接由 npm ci 按锁文件创建
RUN ln -s /repo/addp/orchestrator/frontend/node_modules /repo/addp/common-frontend/node_modules

# 复制源代码
COPY orchestrator/frontend/index.html orchestrator/frontend/vite.config.js ./
COPY orchestrator/frontend/src ./src

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /repo/addp/orchestrator/frontend/dist /usr/share/nginx/html

# 复制 nginx 配置
COPY orchestrator/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
