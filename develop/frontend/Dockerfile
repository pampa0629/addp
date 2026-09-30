ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /repo/addp/develop/frontend

# 复制 package.json
COPY common-frontend/package.json /repo/addp/common-frontend/package.json
COPY develop/frontend/package.json develop/frontend/package-lock.json ./

RUN npm ci

# 复制 common-frontend (Vite 别名需要)
COPY common-frontend /repo/addp/common-frontend

# 创建符号链接，让 common-frontend 能访问 node_modules
RUN ln -s /repo/addp/develop/frontend/node_modules /repo/addp/common-frontend/node_modules

# 复制源代码
COPY develop/frontend/index.html develop/frontend/vite.config.js ./
COPY develop/frontend/src ./src

# 构建
RUN npm run build

# 运行阶段
FROM localhost:5001/nginx:alpine

# 复制构建产物
COPY --from=builder /repo/addp/develop/frontend/dist /usr/share/nginx/html

# 复制 nginx 配置
COPY develop/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
