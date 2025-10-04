# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## 项目概述

**Manager 模块** 是全域数据平台的数据管理服务，负责管理数据源的接入、上传数据的组织和数据预览功能。

技术栈：
- **后端**: Go + Gin + GORM + PostgreSQL
- **前端**: Vue 3 + Vite + Pinia + Element Plus
- **存储**: MinIO (S3-compatible)
- **数据库**: PostgreSQL (manager schema)

## 常用命令

### 后端开发

```bash
# 进入后端目录
cd backend

# 下载依赖
go mod download

# 开发模式运行
go run cmd/server/main.go

# 编译
go build -o ../bin/manager cmd/server/main.go

# 运行测试
go test ./...
```

### 前端开发

```bash
# 进入前端目录
cd frontend

# 安装依赖
npm install

# 开发模式运行（默认端口 5174）
npm run dev

# 构建生产版本
npm run build

# 预览生产版本
npm run preview
```

## 项目结构

### 后端架构（Go）

```
backend/
├── cmd/server/          # 应用入口
│   └── main.go
├── internal/            # 内部包（不对外暴露）
│   ├── api/            # HTTP 处理层
│   │   ├── router.go   # 路由配置
│   │   └── *_handler.go # 各模块的 HTTP 处理器
│   ├── config/         # 配置管理
│   ├── middleware/     # 中间件（认证、日志等）
│   ├── models/         # 数据模型和请求/响应结构
│   ├── repository/     # 数据访问层
│   ├── service/        # 业务逻辑层
│   ├── connector/      # 数据源连接器
│   └── preview/        # 数据预览引擎
└── pkg/                # 可对外暴露的工具包
    ├── storage/        # 存储抽象层
    └── utils/          # 工具函数（加密等）
```

**分层设计**:
- **API Layer**: 处理 HTTP 请求、参数验证、响应格式化
- **Service Layer**: 实现业务逻辑、事务处理
- **Repository Layer**: 数据库操作、CRUD 接口
- **Model Layer**: 定义数据结构、数据库表映射
- **Connector Layer**: 实现各类数据源连接器
- **Preview Layer**: 实现数据预览解析器

### 前端架构（Vue 3）

```
frontend/src/
├── api/              # API 请求封装
│   ├── client.js    # Axios 实例配置（拦截器、认证）
│   └── *.js         # 各模块的 API 调用
├── components/       # 可复用组件
├── store/           # Pinia 状态管理
│   └── auth.js      # 认证状态
├── views/           # 页面组件
│   ├── DataSources.vue  # 数据源管理
│   ├── Directory.vue    # 目录浏览
│   └── Preview.vue      # 数据预览
└── router/          # 路由配置
```

## 核心功能实现

### 认证流程

Manager 模块通过 System 模块实现认证：

1. 前端通过 `/api/auth/login` 登录（调用 System 模块）
2. 获取 JWT Token 并存储到 localStorage
3. 后续请求通过 `Authorization: Bearer <token>` 头部携带 Token
4. 后端中间件 `AuthMiddleware` 验证 Token（调用 System 模块验证）

### 数据库设计

**manager.data_sources 表**:
- 数据源基本信息、连接配置、状态
- 字段: `id`, `name`, `resource_type`, `connection_info`, `tenant_id`, `created_by`, `is_active`
- `connection_info` 为 JSONB 类型，灵活存储不同类型的连接配置
- 敏感字段使用 **AES-256-GCM** 加密（通过 System 资源表实现）

**manager.directories 表**:
- 目录和文件组织结构
- 字段: `id`, `name`, `parent_id`, `path`, `type`, `size`, `tenant_id`, `created_by`
- 支持树形结构（parent_id 自关联）

**manager.permissions 表** (计划中):
- 权限控制
- 字段: `id`, `resource_type`, `resource_id`, `user_id`, `group_id`, `permission`, `tenant_id`

### 数据源连接器架构

**Connector 接口**:
```go
type Connector interface {
    Connect(config ConnectionInfo) error
    TestConnection() error
    Close() error
    GetSchema() (*Schema, error)
}
```

**已支持的数据源**:
- MySQL / MariaDB
- PostgreSQL
- ClickHouse
- MongoDB
- MinIO / S3
- HDFS
- FTP/SFTP

**连接器实现位置**: `internal/connector/`
- `mysql.go` - MySQL 连接器
- `postgres.go` - PostgreSQL 连接器
- `mongodb.go` - MongoDB 连接器
- `s3.go` - MinIO/S3 连接器
- `hdfs.go` - HDFS 连接器
- `ftp.go` - FTP/SFTP 连接器

### 数据预览引擎

**Parser 接口**:
```go
type Parser interface {
    SupportedFormats() []string
    Parse(reader io.Reader) (*PreviewData, error)
    GetSchema(reader io.Reader) ([]Field, error)
}
```

**已支持的格式**:
- CSV
- JSON / JSONL
- Parquet
- Excel (xlsx, xls)
- TXT
- SQL

**预览实现位置**: `internal/preview/`
- `csv.go` - CSV 解析器
- `json.go` - JSON 解析器
- `parquet.go` - Parquet 解析器
- `excel.go` - Excel 解析器

### 文件上传分片策略

**分片配置**:
```go
const (
    ChunkSize      = 5 * 1024 * 1024  // 5MB per chunk
    MaxFileSize    = 10 * 1024 * 1024 * 1024  // 10GB max
    UploadTimeout  = 3600 * time.Second  // 1 hour
)
```

**上传流程**:
1. **客户端**：
   - 计算文件 MD5 哈希
   - 将文件分割为固定大小的分片（5MB）
   - 为每个分片生成唯一标识

2. **服务端**：
   - 接收分片上传请求
   - 验证分片完整性
   - 存储分片到临时目录
   - 记录上传进度

3. **合并分片**：
   - 所有分片上传完成后触发合并
   - 按序合并分片到最终文件
   - 验证最终文件 MD5
   - 清理临时分片文件

### 预览数据缓存策略

**缓存架构**:
- **L1 Cache**: 内存缓存（LRU，最大 100 个文件预览）
- **L2 Cache**: Redis 缓存（TTL 30 分钟）
- **缓存 Key**: `preview:{file_id}:{offset}:{limit}`

**缓存策略**:
```go
type PreviewCache struct {
    FileID     string
    Offset     int
    Limit      int
    Data       []map[string]interface{}
    Schema     []Field
    TotalRows  int64
    CachedAt   time.Time
    TTL        time.Duration  // 30 minutes
}
```

**缓存失效条件**:
- 文件被修改或删除
- 超过 TTL 时间
- 内存不足时 LRU 淘汰

**预览采样策略**:
- 小文件（< 10MB）：加载全部数据
- 中等文件（10MB - 100MB）：加载前 10000 行
- 大文件（> 100MB）：按页加载，每页 1000 行

## 开发注意事项

1. **添加新的 API 端点**:
   - 在 `internal/models/` 定义请求/响应结构
   - 在 `internal/repository/` 添加数据访问方法
   - 在 `internal/service/` 实现业务逻辑
   - 在 `internal/api/` 创建 HTTP 处理器
   - 在 `internal/api/router.go` 注册路由

2. **添加新的数据源类型**:
   - 实现 `connector.Connector` 接口
   - 在 `internal/connector/` 创建新的连接器文件
   - 在 `connector/factory.go` 注册新类型
   - 更新数据源连接参数格式文档

3. **添加新的预览格式**:
   - 实现 `preview.Parser` 接口
   - 在 `internal/preview/` 创建新的解析器文件
   - 在 `preview/factory.go` 注册新格式
   - 添加相关的格式检测逻辑

4. **数据库迁移**:
   - 修改 `internal/models/` 中的模型结构
   - 在 `repository/database.go` 的 `AutoMigrate` 中添加新模型
   - 重启应用自动执行迁移

5. **前端添加新页面**:
   - 在 `src/views/` 创建 Vue 组件
   - 在 `src/api/` 添加 API 调用函数
   - 在 `src/router/index.js` 注册路由
   - 在侧边栏添加导航链接

6. **环境配置**:
   - 后端配置通过环境变量或 `.env` 文件
   - 前端配置通过 `vite.config.js` 和 API baseURL

7. **端口配置**:
   - 后端默认: 8081
   - 前端开发: 5174
   - 前端生产（Nginx）: 8091

## 安全机制

### 数据源密码加密

1. **资源连接密码加密** (通过 System 资源表)
   - 算法: **AES-256-GCM** (对称加密 + 认证)
   - 密钥管理:
     - 开发环境: 默认32字节密钥 `dev-encryption-key-32-bytes!`
     - 生产环境: 环境变量 `ENCRYPTION_KEY` (Base64编码)
   - 加密字段: `password`, `access_key`, `secret_key`, `token`, `api_key`
   - 自动解密: Manager 模块查询 System 资源时自动解密

2. **本地解密工具**
   - 位置: `pkg/utils/encryption.go`
   - 功能: 解密 System 模块加密的敏感字段
   - 使用: `Decrypt(ciphertext, key)`

### 权限模型 (计划中)

**权限级别**:
```go
type Permission string

const (
    PermissionNone   Permission = "none"      // 无权限
    PermissionRead   Permission = "read"      // 只读
    PermissionWrite  Permission = "write"     // 读写
    PermissionAdmin  Permission = "admin"     // 管理员
)
```

**权限检查优先级**:
1. 用户级别权限（最高优先级）
2. 用户组权限
3. 继承的父目录权限
4. 默认权限（无权限）

## API 端点

### 数据源管理
- `POST /api/datasources` - 创建数据源
- `GET /api/datasources` - 获取数据源列表（自动过滤租户）
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

### 文件上传
- `POST /api/upload/init` - 初始化上传，返回 upload_id
- `POST /api/upload/chunk` - 上传分片
- `POST /api/upload/complete` - 完成上传，触发合并
- `GET /api/upload/progress` - 查询上传进度
- `POST /api/upload/cancel` - 取消上传

### 权限管理 (计划中)
- `GET /api/permissions/check` - 检查权限
- `POST /api/permissions` - 授予权限
- `DELETE /api/permissions/:id` - 撤销权限

### 元数据管理
- `POST /api/metadata/scan` - 扫描数据源元数据
- `GET /api/metadata/databases` - 获取数据库列表
- `GET /api/metadata/tables` - 获取表列表
- `GET /api/metadata/fields` - 获取字段列表

## 与其他模块集成

### 与 System 模块集成

**认证**:
```go
// 从 System 模块验证 JWT Token
type SystemClient struct {
    BaseURL string
}

func (c *SystemClient) ValidateToken(token string) (*User, error)
func (c *SystemClient) GetUserPermissions(userID uint) ([]Permission, error)
```

**资源管理**:
- Manager 创建数据源时，在 System 模块创建对应的 Resource 记录
- System 模块自动加密敏感字段（password, access_key 等）
- Manager 查询资源时，System 模块自动解密返回

### 与 Meta 模块集成

**元数据通知**:
```go
type MetaClient struct {
    BaseURL string
}

// 数据源创建后，通知 Meta 解析元数据
func (c *MetaClient) NotifyDataSourceCreated(dsID uint) error

// 文件上传后，通知 Meta 解析文件元数据
func (c *MetaClient) NotifyFileUploaded(fileID uint, filePath string) error

// 数据源删除后，通知 Meta 清理元数据
func (c *MetaClient) NotifyDataSourceDeleted(dsID uint) error
```

### 与 Transfer 模块集成

**数据源信息提供**:
```go
type ManagerAPI interface {
    GetDataSourceConnection(dsID uint) (*ConnectionInfo, error)
    GetFileLocation(fileID uint) (string, error)
}
```

### 集成配置

```bash
# System 模块地址
SYSTEM_SERVICE_URL=http://localhost:8080

# Meta 模块地址
META_SERVICE_URL=http://localhost:8082

# Transfer 模块地址
TRANSFER_SERVICE_URL=http://localhost:8083

# 是否启用服务间调用
ENABLE_SERVICE_INTEGRATION=true

# 服务调用超时
SERVICE_CALL_TIMEOUT=30s
```

## 独立运行说明

Manager 模块可以独立运行和部署，不依赖其他微服务：

### 独立运行模式

```bash
# 禁用服务间集成
export ENABLE_SERVICE_INTEGRATION=false

# 使用本地认证（不调用 System 模块）
export AUTH_MODE=local

# 启动服务
go run cmd/server/main.go
```

### 完整平台模式

与其他模块一起部署时，通过 Gateway 统一对外提供服务：

```
客户端 → Gateway:8000 → Manager:8081
                      ↓
                   System:8080 (认证)
                      ↓
                   Meta:8082 (元数据)
```

## 数据源连接参数格式

### MySQL / MariaDB
```json
{
  "host": "localhost",
  "port": 3306,
  "database": "mydb",
  "username": "user",
  "password": "password",
  "charset": "utf8mb4",
  "parseTime": true,
  "timeout": "10s"
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
  "sslmode": "disable",
  "timezone": "Asia/Shanghai"
}
```

### MongoDB
```json
{
  "uri": "mongodb://user:password@localhost:27017",
  "database": "mydb",
  "authSource": "admin",
  "replicaSet": ""
}
```

### MinIO / S3
```json
{
  "endpoint": "s3.amazonaws.com",
  "accessKey": "AKIAIOSFODNN7EXAMPLE",
  "secretKey": "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
  "bucket": "my-bucket",
  "region": "us-east-1",
  "useSSL": true
}
```

### HDFS
```json
{
  "namenode": "hdfs://namenode:9000",
  "user": "hadoop",
  "kerberos": false
}
```

### FTP / SFTP
```json
{
  "host": "ftp.example.com",
  "port": 21,
  "username": "ftpuser",
  "password": "password",
  "protocol": "sftp",
  "passiveMode": true
}
```

## 测试

```bash
# 单元测试
go test ./internal/...

# 集成测试
go test -tags=integration ./test/integration/...

# 数据源连接测试
go test ./internal/connector/...

# 预览功能测试
go test ./internal/preview/...

# 覆盖率测试
go test -cover ./...
```

## 监控指标

### 业务指标
- 数据源数量
- 文件总数和总大小
- 预览请求次数
- 上传成功/失败率

### 技术指标
- API 响应时间
- 数据库连接池状态
- MinIO 存储使用率
- 缓存命中率

### Prometheus 指标
```go
manager_datasources_total         // 数据源总数
manager_files_total               // 文件总数
manager_storage_bytes             // 存储使用量
manager_preview_requests_total    // 预览请求数
manager_upload_duration_seconds   // 上传耗时
```

## 开发规范

1. **代码风格**: 遵循 Go 官方代码风格和命名规范
2. **错误处理**: 所有错误必须妥善处理，不能忽略
3. **日志记录**: 使用结构化日志，包含足够的上下文信息
4. **并发安全**: 注意并发访问的数据结构，使用适当的锁机制
5. **资源释放**: 及时关闭文件句柄、数据库连接等资源
6. **单元测试**: 核心业务逻辑必须有单元测试覆盖
7. **文档注释**: 公开的函数和类型必须有文档注释
