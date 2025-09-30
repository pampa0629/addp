# Manager 数据管理模块

## 概述

Manager 是全域数据平台的数据管理服务，负责管理数据源的接入、上传数据的组织和数据预览功能。

## 核心功能

- **数据源管理**: 配置和管理各类数据源连接（数据库、文件系统、API 等）
- **目录组织**: 为上传的数据建立目录树结构，支持分层管理
- **数据预览**: 支持多种数据格式的在线预览（CSV、JSON、Parquet 等）
- **权限控制**: 管理用户对不同数据源和目录的访问权限
- **数据统计**: 提供数据量、存储空间等统计信息

## 技术栈

- **语言**: Go 1.21+
- **框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL (元数据) / MinIO (文件存储)
- **前端**: Vue 3 + Element Plus

## 项目结构

```
manager/
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go      # 应用入口
│   ├── internal/
│   │   ├── api/             # HTTP 处理层
│   │   ├── service/         # 业务逻辑层
│   │   ├── repository/      # 数据访问层
│   │   ├── models/          # 数据模型
│   │   ├── connector/       # 数据源连接器
│   │   └── preview/         # 数据预览引擎
│   ├── pkg/
│   │   └── storage/         # 存储抽象层
│   ├── Dockerfile
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── views/
│   │   │   ├── DataSources.vue    # 数据源管理
│   │   │   ├── Directory.vue      # 目录浏览
│   │   │   └── Preview.vue        # 数据预览
│   │   └── api/
│   ├── package.json
│   └── Dockerfile
└── README.md
```

## 数据模型设计

### 数据源 (DataSource)
```go
type DataSource struct {
    ID             uint
    Name           string
    Type           string  // mysql, postgresql, s3, hdfs, etc.
    ConnectionInfo JSON    // 连接配置
    Status         string  // active, inactive, error
    CreatedBy      uint
}
```

### 目录结构 (Directory)
```go
type Directory struct {
    ID        uint
    Name      string
    ParentID  *uint      // 父目录 ID，null 为根目录
    Path      string     // 完整路径
    Type      string     // folder, file
    Size      int64      // 文件大小（字节）
    CreatedBy uint
}
```

## API 端点

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

## 支持的数据源类型

### 数据库
- MySQL / MariaDB
- PostgreSQL
- ClickHouse
- MongoDB

### 文件系统
- 本地文件系统
- MinIO / S3
- HDFS
- NFS

### 其他
- HTTP/HTTPS API
- FTP/SFTP

## 支持的预览格式

- CSV
- JSON / JSONL
- Parquet
- Excel (xlsx, xls)
- TXT
- SQL

## 开发计划

### 阶段 1: 基础功能
- [ ] 数据源 CRUD API
- [ ] 数据源连接测试
- [ ] 基础目录树实现
- [ ] 简单文件预览（CSV, JSON）

### 阶段 2: 存储集成
- [ ] MinIO 集成用于文件存储
- [ ] 文件上传功能
- [ ] 大文件分片上传
- [ ] 目录浏览前端

### 阶段 3: 高级预览
- [ ] Parquet 格式支持
- [ ] Excel 文件预览
- [ ] 数据采样和分页
- [ ] 数据格式转换

### 阶段 4: 权限与安全
- [ ] 基于角色的访问控制
- [ ] 数据源级别权限
- [ ] 目录级别权限
- [ ] 操作审计日志

## 配置说明

### 环境变量

```bash
# 服务端口
MANAGER_PORT=8081

# 数据库配置
DB_HOST=postgres
DB_PORT=5432
DB_NAME=manager
DB_USER=manager
DB_PASSWORD=password

# 存储配置
STORAGE_TYPE=minio  # local, minio, s3, hdfs
MINIO_ENDPOINT=minio:9000
MINIO_ACCESS_KEY=minioadmin
MINIO_SECRET_KEY=minioadmin
MINIO_BUCKET=addp-data

# 预览配置
PREVIEW_MAX_ROWS=1000
PREVIEW_MAX_SIZE_MB=100
```

## 运行方式

```bash
# 后端开发
cd backend
go run cmd/server/main.go

# 前端开发
cd frontend
npm install
npm run dev

# Docker 部署
docker-compose up -d
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

## 文件上传分片策略

### 分片配置
```go
const (
    ChunkSize      = 5 * 1024 * 1024  // 5MB per chunk
    MaxFileSize    = 10 * 1024 * 1024 * 1024  // 10GB max
    UploadTimeout  = 3600 * time.Second  // 1 hour
)
```

### 上传流程
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

### API 端点
```bash
POST /api/upload/init        # 初始化上传，返回 upload_id
POST /api/upload/chunk       # 上传分片
POST /api/upload/complete    # 完成上传，触发合并
GET  /api/upload/progress    # 查询上传进度
POST /api/upload/cancel      # 取消上传
```

## 预览数据缓存策略

### 缓存架构
- **L1 Cache**: 内存缓存（LRU，最大 100 个文件预览）
- **L2 Cache**: Redis 缓存（TTL 30 分钟）
- **缓存 Key**: `preview:{file_id}:{offset}:{limit}`

### 缓存策略
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

### 缓存失效条件
- 文件被修改或删除
- 超过 TTL 时间
- 内存不足时 LRU 淘汰

### 预览采样策略
- 小文件（< 10MB）：加载全部数据
- 中等文件（10MB - 100MB）：加载前 10000 行
- 大文件（> 100MB）：按页加载，每页 1000 行

## 权限模型设计

### 权限级别
```go
type Permission string

const (
    PermissionNone   Permission = "none"      // 无权限
    PermissionRead   Permission = "read"      // 只读
    PermissionWrite  Permission = "write"     // 读写
    PermissionAdmin  Permission = "admin"     // 管理员
)
```

### 数据源权限
```go
type DataSourcePermission struct {
    ID           uint
    DataSourceID uint
    UserID       *uint   // 用户 ID，null 表示组权限
    GroupID      *uint   // 用户组 ID
    Permission   Permission
}
```

### 目录权限
```go
type DirectoryPermission struct {
    ID          uint
    DirectoryID uint
    UserID      *uint
    GroupID     *uint
    Permission  Permission
    Inherited   bool    // 是否继承父目录权限
}
```

### 权限检查优先级
1. 用户级别权限（最高优先级）
2. 用户组权限
3. 继承的父目录权限
4. 默认权限（无权限）

### 权限检查 API
```bash
GET /api/permissions/check?resource_type=datasource&resource_id=1&action=read
```

## 与其他模块的集成

### 与 System 模块集成
```go
// 从 System 模块获取用户认证信息
type SystemClient struct {
    BaseURL string
}

func (c *SystemClient) ValidateToken(token string) (*User, error)
func (c *SystemClient) GetUserPermissions(userID uint) ([]Permission, error)
```

### 与 Meta 模块集成
```go
// 当数据源或文件发生变化时，通知 Meta 模块更新元数据
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
```go
// Manager 提供数据源信息给 Transfer 使用
type ManagerAPI interface {
    GetDataSourceConnection(dsID uint) (*ConnectionInfo, error)
    GetFileLocation(fileID uint) (string, error)
}
```

### 集成配置
```bash
# System 模块地址
SYSTEM_SERVICE_URL=http://system:8080

# Meta 模块地址
META_SERVICE_URL=http://meta:8082

# Transfer 模块地址
TRANSFER_SERVICE_URL=http://transfer:8083

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

## Docker Compose 配置

### 独立运行
```yaml
version: '3.8'

services:
  manager:
    build: ./backend
    ports:
      - "8081:8081"
    environment:
      - DB_HOST=postgres
      - MINIO_ENDPOINT=minio:9000
    depends_on:
      - postgres
      - minio

  postgres:
    image: postgres:15
    environment:
      POSTGRES_DB: manager
      POSTGRES_USER: manager
      POSTGRES_PASSWORD: password

  minio:
    image: minio/minio
    command: server /data
    ports:
      - "9000:9000"
```

### 完整平台运行
在根目录的 `docker-compose.yml` 中配置所有服务的协同工作。

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

## 常见问题

### 1. 如何支持新的数据源类型？
实现 `connector.Connector` 接口：
```go
type Connector interface {
    Connect(config ConnectionInfo) error
    TestConnection() error
    Close() error
    GetSchema() (*Schema, error)
}
```

### 2. 大文件上传超时怎么办？
调整配置：
```bash
UPLOAD_TIMEOUT=7200s  # 增加超时时间
CHUNK_SIZE=10485760   # 增加分片大小到 10MB
```

### 3. 如何扩展预览支持的文件格式？
实现 `preview.Parser` 接口：
```go
type Parser interface {
    SupportedFormats() []string
    Parse(reader io.Reader) (*PreviewData, error)
    GetSchema(reader io.Reader) ([]Field, error)
}
```