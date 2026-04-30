FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

# 从项目根目录的 dist/ 复制预编译的二进制
COPY dist/manager-backend ./server
COPY manager/backend/plugins ./manager/backend/plugins

RUN chmod +x ./server

ENV TZ=Asia/Shanghai
ENV PROJECT_ROOT=/app

EXPOSE 8081

CMD ["./server"]
