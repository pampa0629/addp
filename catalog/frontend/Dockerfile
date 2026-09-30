ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /app

COPY catalog/frontend/package.json catalog/frontend/package-lock.json ./

RUN npm ci

COPY common-frontend /common-frontend

RUN ln -s /app/node_modules /common-frontend/node_modules

COPY catalog/frontend/index.html catalog/frontend/vite.config.js ./
COPY catalog/frontend/src ./src

RUN npm run build

FROM localhost:5001/nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY catalog/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
