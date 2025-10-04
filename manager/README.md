# Manager 数据管理模块

> 全域数据平台的数据源接入和文件管理服务

## 🎯 核心功能

- **数据源管理**: 连接和管理各类数据源（数据库、文件系统、对象存储等）
- **目录组织**: 为上传的数据建立目录树结构，支持分层管理
- **数据预览**: 支持多种格式的在线预览（CSV、JSON、Parquet、Excel 等）
- **元数据扫描**: 自动扫描数据库表结构和字段信息
- **权限控制**: 管理用户对数据源和目录的访问权限

## 🚀 快速开始

### 前置要求

- Go 1.21+
- PostgreSQL 15+ (元数据存储)
- MinIO (文件存储，可选)
- System 模块 (认证服务)

### 运行后端

```bash
cd backend
go mod download
go run cmd/server/main.go
```

访问: http://localhost:8081

### 运行前端

```bash
cd frontend
npm install
npm run dev
```

访问: http://localhost:5174

### Docker 部署

```bash
cd manager
docker-compose up -d
```

## 📊 支持的数据源类型

### 数据库
- MySQL / MariaDB
- PostgreSQL
- ClickHouse
- MongoDB

### 文件系统
- 本地文件系统
- MinIO / S3
- HDFS
- FTP/SFTP

### 其他
- HTTP/HTTPS API

## 📁 支持的预览格式

- CSV
- JSON / JSONL
- Parquet
- Excel (xlsx, xls)
- TXT
- SQL

## 📡 主要 API 端点

### 数据源管理
- `POST /api/datasources` - 创建数据源
- `GET /api/datasources` - 获取数据源列表
- `GET /api/datasources/:id` - 获取数据源详情
- `PUT /api/datasources/:id` - 更新数据源
- `DELETE /api/datasources/:id` - 删除数据源
- `POST /api/datasources/:id/test` - 测试数据源连接

### 目录管理
- `GET /api/directories` - 获取目录树
- `POST /api/directories` - 创建目录/上传文件
- `GET /api/directories/:id` - 获取目录内容
- `PUT /api/directories/:id` - 重命名/移动
- `DELETE /api/directories/:id` - 删除目录/文件

### 数据预览
- `GET /api/preview/:id` - 预览文件数据
- `GET /api/preview/:id/schema` - 获取数据结构

### 元数据管理
- `POST /api/metadata/scan` - 扫描数据源元数据
- `GET /api/metadata/databases` - 获取数据库列表
- `GET /api/metadata/tables` - 获取表列表
- `GET /api/metadata/fields` - 获取字段列表

### 文件上传
- `POST /api/upload/init` - 初始化上传
- `POST /api/upload/chunk` - 上传分片
- `POST /api/upload/complete` - 完成上传
- `GET /api/upload/progress` - 查询上传进度
- `POST /api/upload/cancel` - 取消上传

## ⚙️ 环境配置

### 后端配置 (.env)

```bash
# 服务端口
PORT=8081

# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_NAME=addp
DB_USER=addp
DB_PASSWORD=addp_password
DB_SCHEMA=manager

# System 模块地址 (用于认证)
SYSTEM_SERVICE_URL=http://localhost:8080

# 存储配置
STORAGE_TYPE=minio  # local, minio, s3, hdfs
MINIO_ENDPOINT=localhost:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
MINIO_BUCKET=addp-data

# 预览配置
PREVIEW_MAX_ROWS=1000
PREVIEW_MAX_SIZE_MB=100

# 服务集成
ENABLE_SERVICE_INTEGRATION=true
SERVICE_CALL_TIMEOUT=30s
```

### 端口说明

- **后端**: 8081 (开发) / 8081 (Docker)
- **前端**: 5174 (开发) / 8091 (Docker)

## 🔗 与其他模块集成

Manager 模块与其他模块的协作关系：

```
Manager:8081 ──┐
               ├→ System:8080 (用户认证、资源加密)
               ├→ Meta:8082 (元数据解析和存储)
               └→ Transfer:8083 (数据导入导出)
```

### 认证机制
- 通过 System 模块进行用户认证
- 使用 JWT Token 验证请求

### 资源加密
- 数据源连接密码通过 System 模块加密存储
- 使用 AES-256-GCM 算法确保安全

### 元数据同步
- 数据源创建后自动通知 Meta 模块解析元数据
- 文件上传后自动提取元数据信息

## 🔐 安全特性

### 密码加密
- 数据源连接密码使用 AES-256-GCM 加密
- 加密密钥通过环境变量 `ENCRYPTION_KEY` 配置
- 开发环境使用默认密钥，生产环境必须设置

### 访问控制
- 支持基于角色的权限控制
- 数据源级别权限管理
- 目录级别权限管理
- 操作审计日志记录

## 📋 数据源连接示例

### MySQL
```json
{
  "host": "localhost",
  "port": 3306,
  "database": "mydb",
  "username": "user",
  "password": "password"
}
```

### PostgreSQL
```json
{
  "host": "localhost",
  "port": 5432,
  "database": "mydb",
  "username": "user",
  "password": "password",
  "sslmode": "disable"
}
```

### MinIO / S3
```json
{
  "endpoint": "localhost:9000",
  "accessKey": "minioadmin",
  "secretKey": "minioadmin",
  "bucket": "my-bucket",
  "useSSL": false
}
```

## 🐛 常见问题

### 1. 数据源连接测试失败？

检查以下几点：
- 网络连接是否正常
- 连接参数是否正确（主机、端口、用户名、密码）
- 数据库是否允许远程连接
- 防火墙是否开放相应端口

### 2. 文件上传失败？

可能的原因：
- 文件大小超过限制（默认 10GB）
- MinIO 服务未启动或配置错误
- 存储空间不足

### 3. 预览大文件卡顿？

优化建议：
- 使用分页预览，每次加载 1000 行
- 调整预览行数限制 `PREVIEW_MAX_ROWS`
- 启用 Redis 缓存加速

### 4. 如何支持新的数据源类型？

请参考技术文档 [CLAUDE.md](./CLAUDE.md) 中的"添加新的数据源类型"章节。

### 5. 如何独立运行 Manager 模块？

```bash
# 禁用服务间集成
export ENABLE_SERVICE_INTEGRATION=false

# 使用本地认证模式
export AUTH_MODE=local

# 启动服务
go run cmd/server/main.go
```

## 📚 更多文档

- 详细技术文档: [CLAUDE.md](./CLAUDE.md)
- API 详细说明: [CLAUDE.md#API端点](./CLAUDE.md)
- 开发规范: [CLAUDE.md#开发规范](./CLAUDE.md)
- 数据源连接格式: [CLAUDE.md#数据源连接参数格式](./CLAUDE.md)

## 📄 License

Copyright © 2025 ADDP Team
