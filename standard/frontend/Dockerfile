FROM localhost:5001/node:18.20.5-alpine AS builder

WORKDIR /app

# 复制 package.json
COPY standard/frontend/package.json ./

# 安装依赖 (不使用 lock 文件以避免 npm bug)
# 显式安装 rollup 的本地依赖
RUN npm install --no-package-lock && \
    npm install --no-save @rollup/rollup-linux-arm64-musl && \
    npm cache clean --force

# 复制 common-frontend (Vite 别名需要)
COPY common-frontend /common-frontend

# 创建符号链接，让 common-frontend 能访问 node_modules
RUN ln -s /app/node_modules /common-frontend/node_modules

# 复制源代码
COPY standard/frontend .

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
