FROM alpine:latest

RUN apk --no-cache add ca-certificates tzdata curl

WORKDIR /app

# 从项目根目录的 dist/ 复制预编译的二进制
COPY dist/develop-backend ./server

RUN chmod +x ./server

ENV TZ=Asia/Shanghai

EXPOSE 8185

CMD ["./server"]
