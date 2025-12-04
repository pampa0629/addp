# Transfer 模块使用指南

本文档提供 Transfer 模块的详细使用说明和实际示例。

---

## 📖 目录

1. [快速开始](#快速开始)
2. [核心概念](#核心概念)
3. [使用示例](#使用示例)
4. [API 参考](#api-参考)
5. [配置说明](#配置说明)
6. [故障排除](#故障排除)

---

## 🚀 快速开始

### 1. 启动服务

```bash
# 方式一：使用推荐的启动脚本（会按顺序启动所有服务）
cd /Users/pampa/code/addp
./scripts/dev/start.sh

# 方式二：单独启动 Transfer 后端和 Worker
cd transfer/backend
go run cmd/server/main.go    # Terminal 1: API 服务器
go run cmd/worker/main.go    # Terminal 2: Worker 进程

# 方式三：启动 Transfer 前端
cd transfer/frontend
npm install
npm run dev
```

### 2. 访问方式

- **Portal（推荐）**: http://localhost:5170 → 点击 "数据传输" 卡片
- **直接访问**: http://localhost:5176
- **API 端点**: http://localhost:8083/api

### 3. 第一次使用

```bash
# 1. 登录获取 Token
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.token')

# 2. 检查服务健康状态
curl http://localhost:8083/health

# 3. 查看任务列表（应该为空）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/tasks
```

---

## 💡 核心概念

### 1. Task（任务）

一个 Task 定义了一次数据传输的完整配置：

```json
{
  "name": "用户数据导出",
  "type": "export",              // import | export | sync
  "mode": "batch",               // batch | stream | micro-batch
  "source_id": 1,                // 源数据源（system.resources 的 ID）
  "target_id": 2,                // 目标数据源
  "config": {                    // 详细配置（JSON）
    "source": {
      "query": "SELECT * FROM users WHERE status = 'active'"
    },
    "target": {
      "path": "exports/users.csv"
    }
  },
  "schedule": "0 0 * * *",       // Cron 表达式（每天零点）
  "batch_size": 1000,            // 批处理大小
  "max_parallelism": 4           // 最大并行度
}
```

### 2. TaskExecution（执行记录）

每次任务执行都会创建一个 Execution 记录：

```json
{
  "id": 1,
  "task_id": 1,
  "status": "success",           // pending | running | success | failed | cancelled
  "start_time": "2025-01-15T10:00:00Z",
  "end_time": "2025-01-15T10:05:00Z",
  "records_read": 10000,
  "records_written": 10000,
  "bytes_read": 524288,
  "bytes_written": 512000,
  "trigger_type": "manual"       // manual | schedule | api
}
```

### 3. DataMapping（字段映射）

定义源字段到目标字段的映射：

```json
{
  "source_field": "created_time",
  "target_field": "created_at",
  "transform": "to_timestamp",   // 转换函数
  "field_type": "timestamp",
  "format": "2006-01-02 15:04:05",
  "nullable": false,
  "default_value": "CURRENT_TIMESTAMP"
}
```

---

## 📚 使用示例

### 示例 1：从 PostgreSQL 导出数据到 CSV

#### 场景
将 PostgreSQL 业务数据库中的活跃用户导出为 CSV 文件，存储到 MinIO 对象存储。

#### 步骤 1：准备数据源

```bash
# 登录
TOKEN=$(curl -s -X POST http://localhost:8080/api/auth/login \
  -H "Content-Type: application/json" \
  -d '{"username":"admin","password":"admin123"}' | jq -r '.token')

# 创建 PostgreSQL 源数据库
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "业务数据库",
    "resource_type": "postgresql",
    "connection_info": {
      "host": "localhost",
      "port": 5432,
      "database": "business_db",
      "user": "postgres",
      "password": "postgres123"
    }
  }' | jq .
# 记录返回的 ID，例如 {"id": 1, ...}

# 创建 MinIO 目标存储
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "导出文件存储",
    "resource_type": "minio",
    "connection_info": {
      "endpoint": "localhost:9002",
      "access_key": "minioadmin",
      "secret_key": "minioadmin",
      "bucket": "exports",
      "use_ssl": false
    }
  }' | jq .
# 记录返回的 ID，例如 {"id": 2, ...}
```

#### 步骤 2：创建传输任务

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "活跃用户导出",
    "description": "每天导出活跃用户到 CSV",
    "type": "export",
    "mode": "batch",
    "source_id": 1,
    "target_id": 2,
    "config": {
      "source": {
        "query": "SELECT id, username, email, created_at FROM users WHERE status = '\''active'\''"
      },
      "target": {
        "path": "exports/active_users.csv",
        "format": "csv",
        "headers": true
      }
    },
    "schedule": "0 0 * * *",
    "batch_size": 1000,
    "max_parallelism": 2
  }' | jq .
# 返回任务信息，记录 task_id
```

#### 步骤 3：配置字段映射（可选）

```bash
TASK_ID=1  # 替换为实际的任务 ID

curl -X POST http://localhost:8083/api/tasks/$TASK_ID/mappings \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "source_field": "created_at",
    "target_field": "registration_date",
    "transform": "format_date",
    "format": "2006-01-02",
    "field_type": "string"
  }' | jq .
```

#### 步骤 4：手动触发任务

```bash
curl -X POST http://localhost:8083/api/tasks/$TASK_ID/start \
  -H "Authorization: Bearer $TOKEN" | jq .
# 返回执行 ID: {"execution_id": 1, "status": "running"}
```

#### 步骤 5：监控执行进度

```bash
EXECUTION_ID=1  # 替换为实际的执行 ID

# 查看执行状态
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/$EXECUTION_ID | jq .

# 查看进度（实时）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/$EXECUTION_ID/progress | jq .

# 查看日志
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/$EXECUTION_ID/logs
```

#### 步骤 6：验证结果

```bash
# 使用 MinIO CLI 查看导出的文件
mc ls myminio/exports/
mc cat myminio/exports/active_users.csv | head -n 10
```

---

### 示例 2：从 CSV 导入数据到 MySQL

#### 场景
从 MinIO 上的 CSV 文件批量导入数据到 MySQL 数据库。

#### 步骤 1：准备数据源

```bash
# 创建 MinIO 源存储（假设文件已上传到 imports/users.csv）
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "导入文件存储",
    "resource_type": "minio",
    "connection_info": {
      "endpoint": "localhost:9002",
      "access_key": "minioadmin",
      "secret_key": "minioadmin",
      "bucket": "imports",
      "use_ssl": false
    }
  }' | jq .
# 记录 ID: 3

# 创建 MySQL 目标数据库
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "MySQL目标库",
    "resource_type": "mysql",
    "connection_info": {
      "host": "localhost",
      "port": 3306,
      "database": "target_db",
      "user": "mysql_user",
      "password": "mysql_pass"
    }
  }' | jq .
# 记录 ID: 4
```

#### 步骤 2：创建导入任务

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "CSV批量导入用户",
    "description": "从 CSV 文件导入用户数据",
    "type": "import",
    "mode": "batch",
    "source_id": 3,
    "target_id": 4,
    "config": {
      "source": {
        "path": "imports/users.csv",
        "format": "csv",
        "has_header": true,
        "delimiter": ","
      },
      "target": {
        "table": "users",
        "mode": "insert",
        "conflict_strategy": "skip"
      }
    },
    "batch_size": 5000,
    "max_parallelism": 4,
    "mappings": [
      {
        "source_field": "user_id",
        "target_field": "id",
        "field_type": "integer"
      },
      {
        "source_field": "user_name",
        "target_field": "username",
        "field_type": "varchar"
      },
      {
        "source_field": "email_address",
        "target_field": "email",
        "field_type": "varchar"
      }
    ]
  }' | jq .
```

#### 步骤 3：启动并监控

```bash
TASK_ID=2  # 实际任务 ID

# 启动任务
curl -X POST http://localhost:8083/api/tasks/$TASK_ID/start \
  -H "Authorization: Bearer $TOKEN" | jq .

# 持续监控进度
watch -n 2 "curl -s -H 'Authorization: Bearer $TOKEN' \
  http://localhost:8083/api/tasks/$TASK_ID | jq '.progress'"
```

---

### 示例 3：数据库同步（增量）

#### 场景
定时将 PostgreSQL 源库的变更数据同步到 MySQL 目标库。

```bash
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "订单增量同步",
    "description": "每小时同步新增订单到数据仓库",
    "type": "sync",
    "mode": "batch",
    "source_id": 1,
    "target_id": 5,
    "config": {
      "source": {
        "query": "SELECT * FROM orders WHERE updated_at > ?",
        "incremental_field": "updated_at",
        "incremental_type": "timestamp"
      },
      "target": {
        "table": "orders_warehouse",
        "mode": "upsert",
        "conflict_keys": ["order_id"]
      }
    },
    "schedule": "0 * * * *",
    "batch_size": 2000,
    "retry_policy": {
      "max_retries": 3,
      "backoff_policy": "exponential",
      "initial_delay": "30s",
      "max_delay": "5m"
    }
  }' | jq .
```

---

### 示例 4：对象存储间同步

#### 场景
将 AWS S3 的数据备份到 MinIO 本地存储。

```bash
# 创建 S3 源
curl -X POST http://localhost:8080/api/resources \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "AWS S3 生产环境",
    "resource_type": "s3",
    "connection_info": {
      "endpoint": "s3.amazonaws.com",
      "region": "us-west-2",
      "access_key": "AKIAIOSFODNN7EXAMPLE",
      "secret_key": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
      "bucket": "production-data",
      "use_ssl": true
    }
  }' | jq .

# 创建备份任务
curl -X POST http://localhost:8083/api/tasks \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{
    "name": "S3 数据备份",
    "type": "sync",
    "mode": "batch",
    "source_id": 6,
    "target_id": 2,
    "config": {
      "source": {
        "prefix": "production/",
        "recursive": true
      },
      "target": {
        "prefix": "backup/s3/",
        "preserve_path": true
      },
      "filters": {
        "include_patterns": ["*.json", "*.csv"],
        "exclude_patterns": ["*.tmp", "*.log"]
      }
    },
    "schedule": "0 2 * * *"
  }' | jq .
```

---

## 🔌 API 参考

### 任务管理

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/tasks` | 创建任务 |
| GET | `/api/tasks` | 列出任务（支持分页、筛选） |
| GET | `/api/tasks/:id` | 获取任务详情 |
| PUT | `/api/tasks/:id` | 更新任务配置 |
| DELETE | `/api/tasks/:id` | 删除任务 |
| GET | `/api/tasks/statistics` | 任务统计信息 |

### 任务控制

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/tasks/:id/start` | 手动触发任务 |
| POST | `/api/tasks/:id/stop` | 停止运行中的任务 |
| POST | `/api/tasks/:id/pause` | 暂停任务 |
| POST | `/api/tasks/:id/resume` | 恢复暂停的任务 |

### 字段映射

| 方法 | 路径 | 描述 |
|------|------|------|
| POST | `/api/tasks/:id/mappings` | 创建字段映射 |
| GET | `/api/tasks/:id/mappings` | 列出任务的字段映射 |
| DELETE | `/api/mappings/:id` | 删除字段映射 |

### 执行监控

| 方法 | 路径 | 描述 |
|------|------|------|
| GET | `/api/executions` | 列出所有执行记录 |
| GET | `/api/executions/:id` | 获取执行详情 |
| GET | `/api/executions/:id/progress` | 获取执行进度 |
| GET | `/api/executions/:id/logs` | 获取执行日志 |
| POST | `/api/executions/:id/retry` | 重试失败的执行 |
| POST | `/api/executions/:id/cancel` | 取消运行中的执行 |
| GET | `/api/tasks/:id/executions` | 获取任务的执行历史 |
| GET | `/api/executions/statistics` | 执行统计信息 |

---

## ⚙️ 配置说明

### 环境变量

在 `transfer/backend/.env` 文件中配置：

```bash
# 服务配置
PORT=8083
DB_SCHEMA=transfer
SYSTEM_SERVICE_URL=http://localhost:8080
ENABLE_SERVICE_INTEGRATION=true
INTERNAL_API_KEY=dev-internal-key

# 数据库（PostgreSQL）
DB_HOST=localhost
DB_PORT=5432
DB_USER=addp
DB_PASSWORD=addp_password
DB_NAME=addp

# Redis（任务队列）
REDIS_HOST=localhost
REDIS_PORT=6379
REDIS_PASSWORD=addp_redis

# Worker 配置
WORKER_COUNT=5
CONCURRENT_TASKS=10
MAX_RETRIES=3
RETRY_DELAY=30s

# 任务队列
TASK_QUEUE_NAME=transfer:tasks
```

### 任务配置（Config 字段）

任务的 `config` 字段支持以下配置：

#### 数据库源/目标配置

```json
{
  "source": {
    "query": "SELECT * FROM table WHERE condition",
    "parameters": ["param1", "param2"],
    "incremental_field": "updated_at",
    "incremental_type": "timestamp"
  },
  "target": {
    "table": "target_table",
    "mode": "insert",            // insert | upsert | replace
    "conflict_keys": ["id"],
    "conflict_strategy": "update" // skip | update | error
  }
}
```

#### 文件源/目标配置

```json
{
  "source": {
    "path": "path/to/file.csv",
    "format": "csv",              // csv | json | jsonl
    "has_header": true,
    "delimiter": ",",
    "encoding": "utf-8"
  },
  "target": {
    "path": "output/file.csv",
    "format": "csv",
    "compression": "gzip"         // none | gzip | zip
  }
}
```

#### 对象存储配置

```json
{
  "source": {
    "prefix": "data/",
    "recursive": true,
    "include_patterns": ["*.json"],
    "exclude_patterns": ["*.tmp"]
  },
  "target": {
    "prefix": "backup/",
    "preserve_path": true,
    "overwrite": false
  }
}
```

#### 数据转换配置

```json
{
  "transforms": [
    {
      "type": "filter",
      "conditions": [
        {"field": "status", "operator": "=", "value": "active"}
      ]
    },
    {
      "type": "select",
      "fields": ["id", "name", "email"]
    },
    {
      "type": "rename",
      "mappings": {"old_name": "new_name"}
    }
  ]
}
```

### 重试策略配置

```json
{
  "retry_policy": {
    "max_retries": 3,
    "backoff_policy": "exponential",  // constant | linear | exponential
    "initial_delay": "30s",
    "max_delay": "5m"
  }
}
```

---

## 🔍 故障排除

### 1. 任务无法启动

**问题**：调用 `/api/tasks/:id/start` 返回错误

**可能原因**：
- Worker 进程未启动
- Redis 连接失败
- 数据源配置错误

**解决方法**：
```bash
# 检查 Worker 进程
ps aux | grep "worker/main.go"

# 检查 Redis
redis-cli ping

# 查看任务配置
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/tasks/$TASK_ID | jq .config
```

### 2. 执行失败

**问题**：任务状态变为 `failed`

**解决方法**：
```bash
# 查看错误信息
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/$EXECUTION_ID | jq .error_msg

# 查看详细日志
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/$EXECUTION_ID/logs

# 重试执行
curl -X POST http://localhost:8083/api/executions/$EXECUTION_ID/retry \
  -H "Authorization: Bearer $TOKEN"
```

### 3. 数据源连接失败

**问题**：`error_msg` 显示连接超时或认证失败

**解决方法**：
```bash
# 检查数据源配置
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8080/api/resources/$RESOURCE_ID | jq .connection_info

# 测试数据库连接（以 PostgreSQL 为例）
psql -h localhost -p 5432 -U user -d database

# 测试 MinIO 连接
mc alias set myminio http://localhost:9002 minioadmin minioadmin
mc ls myminio/bucket
```

### 4. 性能问题

**问题**：任务执行速度慢

**优化建议**：
```bash
# 调整批处理大小
curl -X PUT http://localhost:8083/api/tasks/$TASK_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"batch_size": 5000}'

# 增加并行度
curl -X PUT http://localhost:8083/api/tasks/$TASK_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"max_parallelism": 8}'

# 增加 Worker 数量（修改 .env）
WORKER_COUNT=10
CONCURRENT_TASKS=20
```

### 5. 定时任务未触发

**问题**：设置了 `schedule` 但任务未自动执行

**解决方法**：
```bash
# 检查 Worker 日志
tail -f /Users/pampa/code/addp/logs/transfer-worker.log

# 验证 Cron 表达式
# 在线工具：https://crontab.guru/

# 检查任务状态（必须不是 paused）
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/tasks/$TASK_ID | jq .status

# 恢复暂停的任务
curl -X POST http://localhost:8083/api/tasks/$TASK_ID/resume \
  -H "Authorization: Bearer $TOKEN"
```

### 6. 内存溢出（OOM）

**问题**：处理大文件时 Worker 进程崩溃

**解决方法**：
```bash
# 减小批处理大小
curl -X PUT http://localhost:8083/api/tasks/$TASK_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"batch_size": 500, "max_parallelism": 1}'

# 使用流式模式（适合超大文件）
curl -X PUT http://localhost:8083/api/tasks/$TASK_ID \
  -H "Authorization: Bearer $TOKEN" \
  -H "Content-Type: application/json" \
  -d '{"mode": "stream"}'
```

---

## 📊 监控和指标

### 查看任务统计

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/tasks/statistics | jq .
```

**返回示例**：
```json
{
  "total_tasks": 10,
  "pending_tasks": 2,
  "running_tasks": 1,
  "success_tasks": 6,
  "failed_tasks": 1,
  "total_executions": 25,
  "total_records": 1500000,
  "total_bytes": 524288000
}
```

### 查看执行统计

```bash
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/executions/statistics | jq .
```

### 监控单个任务

```bash
# 获取任务最新执行记录
curl -H "Authorization: Bearer $TOKEN" \
  "http://localhost:8083/api/tasks/$TASK_ID/executions?page=1&page_size=1" | jq .

# 查看任务进度
curl -H "Authorization: Bearer $TOKEN" \
  http://localhost:8083/api/tasks/$TASK_ID | jq .progress
```

---

## 🎯 最佳实践

### 1. 任务命名规范

使用描述性名称，包含：
- 数据方向（导入/导出/同步）
- 数据源/目标
- 业务含义

**示例**：
- `导出-业务库-活跃用户-到CSV`
- `导入-MinIO-销售数据-到数据仓库`
- `同步-订单库-增量订单-到分析库`

### 2. 批处理大小选择

| 数据量 | 推荐 batch_size | 说明 |
|--------|----------------|------|
| < 10,000 行 | 500-1000 | 小数据集，快速完成 |
| 10,000-100,000 | 1000-5000 | 中等数据集，平衡性能 |
| > 100,000 | 5000-10000 | 大数据集，减少网络往返 |
| 超大文件 | 使用 stream 模式 | 避免内存溢出 |

### 3. 定时任务 Cron 表达式

```bash
# 每天凌晨 2 点
"0 2 * * *"

# 每小时整点
"0 * * * *"

# 每 15 分钟
"*/15 * * * *"

# 工作日上午 9 点
"0 9 * * 1-5"

# 每月 1 日凌晨
"0 0 1 * *"
```

### 4. 错误处理策略

```json
{
  "retry_policy": {
    "max_retries": 3,
    "backoff_policy": "exponential",
    "initial_delay": "30s",
    "max_delay": "5m"
  },
  "config": {
    "target": {
      "conflict_strategy": "update"  // 冲突时更新而非报错
    }
  }
}
```

### 5. 安全建议

- ✅ 使用 System 模块的 Resources 存储连接信息（自动加密）
- ✅ 不要在任务 `config` 中硬编码密码
- ✅ 定期轮换 `INTERNAL_API_KEY`
- ✅ 使用租户隔离（多租户场景）
- ✅ 审计日志记录所有任务创建和执行

---

## 🔗 相关文档

- [Transfer 模块完成总结](./Transfer模块完成总结.md)
- [Transfer README](./README.md)
- [ADDP 平台架构](../CLAUDE.md)
- [System 模块文档](../system/CLAUDE.md)
- [API 网关文档](../gateway/ARCHITECTURE.md)

---

## 💬 获取帮助

如有问题，请：
1. 查看日志：`tail -f logs/transfer-backend.log`
2. 检查服务状态：`make dev-health`
3. 查看 API 文档：访问 http://localhost:8083/api/swagger (如果已启用)
4. 提交 Issue：在项目 GitHub 仓库创建问题

---

**文档版本**: v1.0.0
**最后更新**: 2025-01-15
**适用版本**: Transfer 模块 v1.2.0+
