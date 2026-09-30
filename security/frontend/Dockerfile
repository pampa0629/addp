ARG NODE_VERSION
FROM localhost:5001/node:${NODE_VERSION}-alpine AS builder
WORKDIR /app
COPY security/frontend/package.json security/frontend/package-lock.json ./
RUN npm ci
COPY common-frontend /common-frontend
RUN ln -s /app/node_modules /common-frontend/node_modules
COPY security/frontend/index.html security/frontend/vite.config.js ./
COPY security/frontend/src ./src
RUN npm run build
FROM localhost:5001/nginx:alpine
COPY --from=builder /app/dist /usr/share/nginx/html
EXPOSE 80
CMD ["nginx", "-g", "daemon off;"]
