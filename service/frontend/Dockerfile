FROM localhost:5001/node:18.20.5-alpine AS builder

WORKDIR /app

COPY service/frontend/package.json ./

RUN npm install --no-package-lock && \
    npm install --no-save "@rollup/rollup-linux-$(node -p 'process.arch')-musl" && \
    npm cache clean --force

COPY common-frontend /common-frontend

RUN ln -s /app/node_modules /common-frontend/node_modules

COPY service/frontend .

RUN npm run build

FROM localhost:5001/nginx:alpine

COPY --from=builder /app/dist /usr/share/nginx/html
COPY service/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
