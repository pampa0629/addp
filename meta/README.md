# Meta 元数据模块

> 全域数据平台的元数据管理和扫描服务

## 📖 文档说明

- **README.md** (本文件) - 快速入门和功能概览
- **[CLAUDE.md](./CLAUDE.md)** - 详细技术文档，包含架构设计、开发指南、常见场景和调试方法

## 🎯 核心功能

- **元数据扫描**: 自动扫描数据库和对象存储，生成层级元数据目录
- **定时调度**: 基于 Cron 表达式的定时扫描任务
- **全文检索**: Meilisearch 索引支持快速搜索（通过 Manager 模块）
- **内容识别与增强**: 按已登记格式规则识别 data item，并在授权允许的扫描深度内补充 attributes
- **扫描任务管理**: 配置可复用扫描任务、创建执行并查看监控
- **能力扩展**: 通过共享 detector、parser 和格式能力扩展识别与增强；向量化任务由 Manager 管理

## 🚀 快速开始

### 方式 1: 开发模式（推荐）

```bash
# 从仓库根目录启动基础设施
bash scripts/infra/up.sh

# 启动 Meta 模块
bash scripts/dev/start.sh -meta
```

开发首选地址为 Backend `http://localhost:8082`、模块前端 `http://localhost:5175`；实际端口以标准启动输出和 `.dev-state/ports.env` 为准，Console 与公开访问入口分别核对。端口归属见[端口规范](../docs/spec/addp端口分配.md)，模块依赖选择见[模块选择指南](../scripts/docs/模块选择启动指南.md)。Meta Backend 与 Worker 的职责见[服务身份与 System 调用](./CLAUDE.md#服务身份与-system-调用)。

### 方式 2: Docker 部署

本地容器联调使用 [Local README](../scripts/local/README.md)，产品部署使用 [Prod README](../scripts/prod/README.md#startsh)。两者均从仓库根目录沿标准生命周期启动，平台服务通过统一 Nginx 入口访问；开发修改继续使用上方原生入口。容器 running／healthy 不代表扫描、认证或公开 HTTPS 已通过业务验收。

## 🏗️ 关键功能

### 扫描任务

`ScanTask` 是可复用定义，一次性手动扫描只创建 execution。定义创建使用 `POST /scan/tasks`，更新使用 `PUT /scan/tasks/:task_id`，分别要求 `meta.scan_task.create` 与 `meta.scan_task.update`；字段见[请求 DTO](backend/internal/models/dto.go)与[任务表](docs/tables/scan_tasks表.md)。集中治理任务当前仅覆盖 PG 整引擎 basic，采集资格与实际源读取要求沿[正式规范](../docs/spec/addp元数据扫描机制规范.md#采集资格与两种范围语义)，保存定义不表示可执行或已获批。

以下示例为当前 Tenant User 的一次性手动扫描，需要 `meta.scan_task.execute`，源读取仍受本人当前权限约束。`META_BASE_URL` 表示本次实际 Backend 地址（不含 API 前缀），`TOKEN` 为该用户凭据；将引擎 ID `1` 和 `public` 替换为已核对、获授权的实际目标。这里的 `catalog_paths` 明确选择该路径范围；单项或叶子目标使用完整 `targets`，不扩大为父目录，具体形状见[API 约束](../docs/spec/addp元数据扫描机制规范.md#api-约束)。

```bash
# 创建一次手动扫描执行
curl -X POST "$META_BASE_URL/api/v1/meta/scan/run/manual" \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "engine_id": 1,
    "catalog_paths": ["public"],
    "scan_depth": "basic",
    "force": false,
    "trigger_type": "manual",
    "source": "meta"
  }'

# 查看当前用户可读的运行历史，需要 meta.scan_task.read
curl -H "Authorization: Bearer $TOKEN" \
  "$META_BASE_URL/api/v1/meta/scan/runs?page=1&page_size=20"
```

创建响应中的 `execution_id` 用于查询 `GET /api/v1/meta/executions/:execution_id`；受理成功不表示扫描完成。历史与详情的权限、本人一次性执行范围和安全投影见[专业读取契约](../docs/spec/tasks/执行监控.md#meta-专业执行读取与响应)，跨任务监控从 Console 的 Monitor 进入。示例未执行，不计为当前链路验收。

`trigger_type` 只表达 `manual` / `scheduled`；手动入口和来源标记的规则统一见[核心维度](../docs/spec/addp元数据扫描机制规范.md#核心维度)。Console 保存引擎后的立即扫描与定时策略、`schedule_mode` 和持久 Cron `schedule` 的分工见 [ScanTask 与 Execution 边界](../docs/spec/addp元数据扫描机制规范.md#scantask-与-execution-边界)，不把引擎保存、任务定义保存或到期调度当作执行成功。

## 📡 主要 API 端点

用户 API 与独立 TaskProvider 路由统一见 [CLAUDE.md 核心 API](./CLAUDE.md#核心-api)；具体请求与响应以模块 Swagger 为准，任务更新和删除使用携带任务 ID 的路径。扫描历史与详情的身份、权限和安全响应见[Meta 专业执行读取与响应](../docs/spec/tasks/执行监控.md#meta-专业执行读取与响应)，验证分工与固定版本证据从[开发与验证](./CLAUDE.md#开发与验证)进入。

## 🔧 文档提取器支持

Meta 通过共享 detector／parser 识别和增强内容，格式范围与扩展入口统一见[数据类型与格式能力规范](../docs/spec/addp数据类型与格式能力规范.md)、[数据项探测器规范](../docs/spec/addp数据项探测器规范.md)和[Basic / Deep 边界](../docs/spec/addp元数据扫描机制规范.md#basic--deep-边界)。具体格式的独有约束与验收以对应专题为准，类型或后缀声明不等于所有文件均已实际验收。

## 🐛 常见问题

### 扫描提示"scan already in progress"？

同范围提交冲突的现行响应为 HTTP `409`／`scan_scope_active`。先核对目标范围、现有 execution 的状态与进度，再查看 Meta Backend／Worker 的[实例日志](../scripts/infra/docs/运行日志.md#模块实例运行日志)和[扫描排查专题](../docs/guide/troubleshooting/元数据扫描与检索.md)。

锁粒度、owner 校验和失败释放统一见[扫描去重锁规范](../docs/spec/addp元数据扫描机制规范.md#扫描去重锁)；冲突不代表原扫描失败，不创建新的 execution，也不释放原 owner 的锁。不要将按引擎 ID 猜测 key 或直接删除 Redis 锁作为通用恢复步骤。

### 如何启用文档向量化？

向量化的配置、任务与结果由 Manager 管理，模型调用统一经过 Inference Runtime；Meta 负责扫描与内容事实。操作见 [Manager 向量化与混合检索](../manager/README.md#向量化与混合检索)，场景绑定、权限及凭据归属见[Manager 向量化配置](../docs/spec/configuration/Manager配置.md#manager-向量化配置)。

## 📚 相关文档

- **[CLAUDE.md](./CLAUDE.md)** - 完整技术文档（架构、开发指南、常见场景）
- [专业扫描读取契约](../docs/spec/tasks/执行监控.md#meta-专业执行读取与响应) - 任务历史、本人一次性执行、机器身份与安全投影
- [扫描读取门禁](../docs/spec/testing/Owner补充门禁.md#meta-扫描与结果归属门禁)、[历史验收证据](../docs/spec/testing/evidence/资源读取与预览历史证据.md#meta-专业扫描读取--t4) - 区分当前标准入口、固定版本结果及未覆盖范围
- **[../docs/spec/addp元数据扫描机制规范.md](../docs/spec/addp元数据扫描机制规范.md)** - 扫描入口、ScanTask、Execution 和调度边界
- **[../docs/spec/addp数据项探测器规范.md](../docs/spec/addp数据项探测器规范.md)** - data item 识别、refs 和 layout 规则
- **[../docs/spec/addp元数据attributes规范.md](../docs/spec/addp元数据attributes规范.md)** - attributes 写入边界
- **[../docs/spec/addp技术栈规约.md](../docs/spec/addp技术栈规约.md)** - 技术栈和依赖版本

---

Copyright © 2025 ADDP Team
