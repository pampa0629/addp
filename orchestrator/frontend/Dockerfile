FROM localhost:5001/node:18.20.5-alpine AS builder

WORKDIR /app

# 复制 package.json
COPY orchestrator/frontend/package.json ./

# 安装依赖 (不使用 lock 文件以避免 npm bug)
# 显式安装 rollup 的本地依赖
RUN npm install --no-package-lock && \
    npm install --no-save @rollup/rollup-linux-arm64-musl && \
    npm cache clean --force

# 复制 common-frontend (用于内部依赖解析)
COPY common-frontend /common-frontend

# 创建符号链接:
# 1. 让 common-frontend 能访问 node_modules
# 2. 让 @addp/common-frontend 指向 /common-frontend
RUN ln -s /app/node_modules /common-frontend/node_modules && \
    mkdir -p /app/node_modules/@addp && \
    ln -s /common-frontend /app/node_modules/@addp/common-frontend

# 复制源代码
COPY orchestrator/frontend .

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /app/dist /usr/share/nginx/html

# 复制 nginx 配置
COPY orchestrator/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
