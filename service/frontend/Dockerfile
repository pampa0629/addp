ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder

WORKDIR /repo/addp/service/frontend

COPY common-frontend/package.json /repo/addp/common-frontend/package.json
COPY service/frontend/package.json service/frontend/package-lock.json ./

RUN npm ci

COPY common-frontend /repo/addp/common-frontend

RUN ln -s /repo/addp/service/frontend/node_modules /repo/addp/common-frontend/node_modules

COPY service/frontend/index.html service/frontend/vite.config.js ./
COPY service/frontend/src ./src

RUN npm run build

FROM localhost:5001/nginx:alpine

COPY --from=builder /repo/addp/service/frontend/dist /usr/share/nginx/html
COPY service/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
