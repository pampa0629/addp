# Meta 元数据模块

> 全域数据平台的元数据管理和扫描服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、常见场景和调试方法

## 🎯 核心功能

- **元数据扫描**: 自动扫描数据库和对象存储，生成层级元数据目录
- **定时调度**: 基于 Cron 表达式的定时扫描任务
- **全文检索**: Meilisearch 索引支持快速搜索（通过 Manager 模块）
- **文档向量化**: 支持多模态文档嵌入（文本、图片、音频、视频）
- **扫描任务管理**: 创建、执行、监控和重试扫描任务
- **插件扩展**: 支持文档提取器插件（文本、图片、音频、视频、PDF）

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 启动基础设施
bash scripts/infra/up.sh

# 启动 Meta 模块
bash scripts/dev/start.sh -meta
```

- 后端: http://localhost:8082
- 前端: http://localhost:5175

### 方式 2: Docker 部署

```bash
cd meta
docker-compose up -d
```

## 🏗️ 关键功能

### 扫描任务

```bash
# 创建定时扫描任务
curl -X POST http://localhost:8082/api/v1/scan-tasks \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "每日扫描",
    "engine_id": 1,
    "schedule_type": "cron",
    "schedule": "0 2 * * *"
  }'

# 手动触发扫描
curl -X POST http://localhost:8082/api/v1/manual-scan \
  -H "Authorization: Bearer <token>" \
  -d '{"engine_id": 1}'

# 查看运行历史
curl -H "Authorization: Bearer <token>" \
  http://localhost:8082/api/v1/scan-runs?task_id=1
```

## 📡 主要 API 端点

```
元数据查询: GET /api/v1/metadata/nodes
扫描执行:   POST /api/v1/manual-scan
任务管理:   GET/POST/PUT/DELETE /api/v1/scan-tasks
运行记录:   GET /api/v1/scan-runs
```

完整 API 文档请查看 [CLAUDE.md](./CLAUDE.md#常见开发场景)

## 🔧 文档提取器支持

| 类型 | 格式 | 提取器 |
|------|------|--------|
| 文本 | txt, json, csv, md | TextExtractor |
| 图片 | jpg, png, gif | ImageExtractor |
| 音频 | mp3, wav, m4a | AudioExtractor |
| 视频 | mp4, avi, mov | VideoExtractor |
| PDF | pdf | PdfExtractor |

## 🐛 常见问题

### 扫描提示"scan already in progress"？

```bash
# 检查扫描锁状态
docker exec -it addp-infra-redis redis-cli KEYS "meta:scan_dedup:*"

# 如果确认没有扫描进程，删除锁
docker exec -it addp-infra-redis redis-cli DEL meta:scan_dedup:1
```

### 如何启用文档向量化？

在 `.env` 中配置 Embedding Service：
```bash
META_EMBEDDING_SERVICE_BASE_URL=http://embedding-service:8080
META_EMBEDDING_SERVICE_API_KEY=your_api_key
```

详细步骤见 [CLAUDE.md#场景-4启用文档向量化功能](./CLAUDE.md)

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、常见场景）
- **[../docs/addp技术栈规约.md](../docs/addp技术栈规约.md)** - 技术栈和依赖版本
- **[../CLAUDE.md](../CLAUDE.md)** - 平台级架构和模块概览

---

Copyright © 2025 ADDP Team
