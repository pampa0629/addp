FROM node:24-alpine AS builder
WORKDIR /app
COPY ontology/frontend/package.json ontology/frontend/package-lock.json ./
RUN npm ci && npm install --no-save "@rollup/rollup-linux-$(node -p 'process.arch')-musl" && npm cache clean --force
COPY common-frontend /common-frontend
RUN ln -s /app/node_modules /common-frontend/node_modules
COPY ontology/frontend .
RUN npm run build
FROM localhost:5001/nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY ontology/frontend/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
