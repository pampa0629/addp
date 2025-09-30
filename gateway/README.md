# Gateway 网关模块

## 概述

Gateway 是全域数据平台的 API 网关服务，负责处理外部请求并路由到相应的内部服务。

## 核心功能

- **请求路由**: 将外部 API 请求路由到对应的内部微服务
- **API 聚合**: 支持聚合多个内部服务的响应
- **认证传递**: 统一处理认证信息并传递给后端服务
- **限流控制**: 实现 API 限流和熔断机制
- **协议转换**: 支持 HTTP/REST 与 gRPC 之间的协议转换

## 技术栈

- **语言**: Go 1.21+
- **框架**: (待定，可选 Gin/Fiber/Go-Gateway)
- **通信**: HTTP/REST, gRPC (服务间)

## 项目结构

```
gateway/
├── cmd/
│   └── gateway/
│       └── main.go          # 网关入口
├── internal/
│   ├── config/              # 配置管理
│   ├── router/              # 路由配置
│   ├── middleware/          # 中间件（认证、限流等）
│   ├── proxy/               # 代理逻辑
│   └── registry/            # 服务发现与注册
├── pkg/
│   └── client/              # 内部服务客户端
├── config/
│   └── routes.yaml          # 路由配置文件
├── Dockerfile
├── go.mod
└── README.md
```

## 路由规则设计

```yaml
# 示例路由配置
routes:
  - path: /api/auth/*
    service: system
    target: http://system:8080/api/auth

  - path: /api/data/*
    service: manager
    target: http://manager:8081/api/data

  - path: /api/metadata/*
    service: meta
    target: http://meta:8082/api/metadata

  - path: /api/transfer/*
    service: transfer
    target: http://transfer:8083/api/transfer
```

## 开发计划

### 阶段 1: 基础路由
- [ ] 实现基本的 HTTP 代理功能
- [ ] 配置文件驱动的路由规则
- [ ] 健康检查和服务发现

### 阶段 2: 认证与安全
- [ ] JWT 认证传递
- [ ] API 密钥管理
- [ ] HTTPS/TLS 支持

### 阶段 3: 高级特性
- [ ] 请求限流和熔断
- [ ] API 聚合
- [ ] 请求/响应转换
- [ ] 监控和日志

## 配置说明

### 环境变量

```bash
# 网关端口
GATEWAY_PORT=8000

# 服务地址
SYSTEM_SERVICE_URL=http://system:8080
MANAGER_SERVICE_URL=http://manager:8081
META_SERVICE_URL=http://meta:8082
TRANSFER_SERVICE_URL=http://transfer:8083

# 限流配置
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW=60s
```

## 运行方式

```bash
# 开发模式
go run cmd/gateway/main.go

# 编译
go build -o bin/gateway cmd/gateway/main.go

# 运行
./bin/gateway
```

## Docker 部署

```bash
# 构建镜像
docker build -t addp-gateway .

# 运行容器
docker run -d -p 8000:8000 \
  -e SYSTEM_SERVICE_URL=http://system:8080 \
  addp-gateway
```

## 待补充内容

- 具体的 API 路由规则
- 限流策略细节
- 服务发现机制选型
- 监控指标定义
- 错误处理规范