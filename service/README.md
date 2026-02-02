# Service 数据服务模块

> 数据服务和 OGC 空间服务发布中心

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、API 详解和注意事项

## 🎯 核心功能

- **外部服务注册**: 管理 OGC 服务（WMS/WFS/WMTS）、REST API 等第三方数据服务
- **数据查询服务**: 统一的数据查询 API，支持多源数据查询
- **服务健康检查**: 定时检测注册服务可用性，自动更新服务状态
- **服务元数据管理**: 自动获取并缓存 OGC GetCapabilities 响应

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Service 模块
bash scripts/dev/start.sh -service
```

- 后端: http://localhost:8086
- 通过 Gateway: http://localhost:8000/api/service/

### 方式 2: Docker 部署

```bash
cd service
docker-compose up -d
```

## 📋 主要 API 端点

```
服务注册: POST/GET/PUT/DELETE /api/service/registry/services
服务刷新: POST /api/service/registry/services/:id/refresh
健康检查: POST /api/service/registry/services/:id/health
服务目录: GET /api/service/catalog
数据查询: POST /api/service/data/query
服务代理: GET /api/service/proxy/:id/*path
```

完整 API 文档请查看 [CLAUDE.md#常见开发场景](./CLAUDE.md#常见开发场景)

## 🏗️ 数据库设计

**Schema**: `service`

| 表名 | 说明 |
|-----|------|
| `external_services` | 注册的外部服务（WMS、WFS 等），支持租户隔离 |
| `external_service_layers` | 外部服务的图层信息（几何类型、坐标系、边界框等） |

详细字段定义请查看 [CLAUDE.md#关键架构](./CLAUDE.md#关键架构)

## 🔐 安全特性

- **租户隔离**: 所有服务按租户 ID 隔离
- **认证**: 所有 API 端点需要 JWT token 认证
- **API Key 加密**: 外部服务的敏感信息加密存储
- **HTTPS 优先**: 建议使用 HTTPS 协议连接外部服务

## 🐛 常见问题

### 如何注册外部 OGC 服务？

```bash
curl -X POST http://localhost:8086/api/service/registry/services \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "示例 WMS 服务",
    "service_type": "wms",
    "url": "https://example.com/wms",
    "auth_type": "none"
  }'
```

详细示例请查看 [CLAUDE.md#场景-1-注册外部-ogc-服务](./CLAUDE.md#场景-1注册外部-ogc-服务)

### 如何手动触发健康检查？

```bash
curl -X POST http://localhost:8086/api/service/registry/services/:id/health \
  -H "Authorization: Bearer <token>"
```

更多问题请查看 [CLAUDE.md](./CLAUDE.md)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发工作流、场景示例）
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../docs/addp配置介绍.md](../docs/addp配置介绍.md)** - 配置中心说明
- **[system/CLAUDE.md](../system/CLAUDE.md)** - System 模块（获取数据库连接信息）

---

Copyright © 2025 ADDP Team
