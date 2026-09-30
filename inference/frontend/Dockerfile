ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /app

COPY inference/frontend/package.json inference/frontend/package-lock.json ./
RUN npm ci

COPY common-frontend /common-frontend
RUN ln -s /app/node_modules /common-frontend/node_modules

COPY inference/frontend/index.html inference/frontend/vite.config.js ./
COPY inference/frontend/src ./src
RUN npm run build

FROM localhost:5001/nginx:alpine

COPY inference/frontend/nginx.conf /etc/nginx/conf.d/default.conf
COPY --from=builder /app/dist /usr/share/nginx/html

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
