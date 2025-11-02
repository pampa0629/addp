FROM golang:1.23-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /build

# 安装编译依赖
RUN apk add --no-cache gcc musl-dev

# 复制整个项目结构（从项目根目录）
COPY common /build/common
COPY system/backend /build/system/backend

# 切换到 system/backend 目录
WORKDIR /build/system/backend

# 下载依赖
RUN go mod download

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -o server ./cmd/server

# 运行阶段
FROM alpine:latest

WORKDIR /app

# 复制编译好的二进制文件
COPY --from=builder /build/system/backend/server .

# 暴露端口
EXPOSE 8080

# 运行
CMD ["./server"]
