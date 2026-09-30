ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder
WORKDIR /app
COPY ontology/frontend/package.json ontology/frontend/package-lock.json ./
RUN npm ci
COPY common-frontend /common-frontend
RUN ln -s /app/node_modules /common-frontend/node_modules
COPY ontology/frontend/index.html ontology/frontend/vite.config.js ./
COPY ontology/frontend/src ./src
RUN npm run build
FROM localhost:5001/nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
COPY ontology/frontend/nginx.conf /etc/nginx/conf.d/default.conf
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
