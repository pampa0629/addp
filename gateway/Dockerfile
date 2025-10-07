# 构建阶段
FROM golang:1.23-alpine AS builder

ENV GOPROXY=https://goproxy.cn,direct

WORKDIR /app

# 复制依赖文件
COPY go.mod go.sum ./
RUN go mod download

# 复制源代码
COPY . .

# 编译
RUN CGO_ENABLED=0 GOOS=linux go build -o gateway cmd/gateway/main.go

# 运行阶段
FROM alpine:latest

WORKDIR /app

# 从构建阶段复制二进制文件
COPY --from=builder /app/gateway .

EXPOSE 8000

CMD ["./gateway"]
