ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /app

COPY quality/frontend/package.json quality/frontend/package-lock.json ./

RUN npm ci

COPY common-frontend /common-frontend

RUN ln -s /app/node_modules /common-frontend/node_modules

COPY quality/frontend/index.html quality/frontend/vite.config.js ./
COPY quality/frontend/src ./src

RUN npm run build

FROM localhost:5001/nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY quality/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
