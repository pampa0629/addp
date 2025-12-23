FROM nginx:alpine

# 从项目根目录的 dist/ 复制预编译的前端文件
COPY dist/service-frontend /usr/share/nginx/html

# 复制自定义 Nginx 配置
COPY service/frontend/nginx.conf /etc/nginx/conf.d/default.conf

EXPOSE 80

CMD ["nginx", "-g", "daemon off;"]
