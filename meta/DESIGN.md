# Meta 模块设计文档 - 混合方案

## 架构概述

Meta 模块采用 **分层元数据管理** 策略，平衡用户体验和资源消耗：

- **Level 1**: 轻量级自动同步（数据库/Schema 级别）
- **Level 2**: 按需深度扫描（表/字段级别）

## 数据库表设计

### 1. metadata_datasources (数据源元数据)

```sql
CREATE TABLE metadata.datasources (
    id SERIAL PRIMARY KEY,
    resource_id INTEGER NOT NULL,           -- 关联 system.resources
    tenant_id INTEGER NOT NULL,             -- 租户隔离
    datasource_name VARCHAR(255),           -- 数据源名称
    datasource_type VARCHAR(50),            -- mysql, postgresql, mongodb, etc.
    sync_status VARCHAR(50) DEFAULT 'pending', -- pending, syncing, success, failed
    last_sync_at TIMESTAMP,                 -- 最后同步时间
    sync_level VARCHAR(20) DEFAULT 'database', -- database, table, field
    error_message TEXT,                     -- 同步错误信息
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_metadata_datasources_resource ON metadata.datasources(resource_id);
CREATE INDEX idx_metadata_datasources_tenant ON metadata.datasources(tenant_id);
```

### 2. metadata_databases (数据库级元数据 - Level 1)

```sql
CREATE TABLE metadata.databases (
    id SERIAL PRIMARY KEY,
    datasource_id INTEGER REFERENCES metadata.datasources(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    database_name VARCHAR(255) NOT NULL,    -- 数据库/Schema 名称
    charset VARCHAR(50),                    -- 字符集
    collation VARCHAR(50),                  -- 排序规则
    table_count INTEGER DEFAULT 0,          -- 表数量（预估）
    total_size_bytes BIGINT DEFAULT 0,      -- 总大小（字节）
    is_scanned BOOLEAN DEFAULT FALSE,       -- 是否已深度扫描
    last_scan_at TIMESTAMP,                 -- 最后扫描时间
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(datasource_id, database_name)
);

CREATE INDEX idx_metadata_databases_datasource ON metadata.databases(datasource_id);
CREATE INDEX idx_metadata_databases_tenant ON metadata.databases(tenant_id);
```

### 3. metadata_tables (表级元数据 - Level 2)

```sql
CREATE TABLE metadata.tables (
    id SERIAL PRIMARY KEY,
    database_id INTEGER REFERENCES metadata.databases(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    table_name VARCHAR(255) NOT NULL,       -- 表名
    table_type VARCHAR(50),                 -- TABLE, VIEW, MATERIALIZED VIEW
    table_schema VARCHAR(255),              -- Schema 名称（PostgreSQL）
    engine VARCHAR(50),                     -- 存储引擎（MySQL）
    row_count BIGINT DEFAULT 0,             -- 行数（预估）
    data_size_bytes BIGINT DEFAULT 0,       -- 数据大小
    index_size_bytes BIGINT DEFAULT 0,      -- 索引大小
    table_comment TEXT,                     -- 表注释
    is_scanned BOOLEAN DEFAULT FALSE,       -- 是否已扫描字段
    last_scan_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(database_id, table_schema, table_name)
);

CREATE INDEX idx_metadata_tables_database ON metadata.tables(database_id);
CREATE INDEX idx_metadata_tables_tenant ON metadata.tables(tenant_id);
CREATE INDEX idx_metadata_tables_name ON metadata.tables(table_name);
```

### 4. metadata_fields (字段级元数据 - Level 2)

```sql
CREATE TABLE metadata.fields (
    id SERIAL PRIMARY KEY,
    table_id INTEGER REFERENCES metadata.tables(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    field_name VARCHAR(255) NOT NULL,       -- 字段名
    field_position INTEGER,                 -- 字段顺序
    data_type VARCHAR(100),                 -- 数据类型
    column_type VARCHAR(255),               -- 完整类型定义（如 varchar(100)）
    is_nullable BOOLEAN DEFAULT TRUE,       -- 是否可空
    column_key VARCHAR(20),                 -- PRI, UNI, MUL
    column_default TEXT,                    -- 默认值
    extra VARCHAR(100),                     -- auto_increment 等
    field_comment TEXT,                     -- 字段注释
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(table_id, field_name)
);

CREATE INDEX idx_metadata_fields_table ON metadata.fields(table_id);
CREATE INDEX idx_metadata_fields_tenant ON metadata.fields(tenant_id);
```

### 5. metadata_sync_logs (同步日志)

```sql
CREATE TABLE metadata.sync_logs (
    id SERIAL PRIMARY KEY,
    datasource_id INTEGER REFERENCES metadata.datasources(id) ON DELETE CASCADE,
    tenant_id INTEGER NOT NULL,
    sync_type VARCHAR(20),                  -- auto, manual, deep
    sync_level VARCHAR(20),                 -- database, table, field
    target_database VARCHAR(255),           -- 目标数据库（深度扫描时）
    status VARCHAR(50),                     -- running, success, failed
    started_at TIMESTAMP,
    completed_at TIMESTAMP,
    duration_seconds INTEGER,
    databases_scanned INTEGER DEFAULT 0,
    tables_scanned INTEGER DEFAULT 0,
    fields_scanned INTEGER DEFAULT 0,
    error_message TEXT,
    created_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_metadata_sync_logs_datasource ON metadata.sync_logs(datasource_id);
CREATE INDEX idx_metadata_sync_logs_tenant ON metadata.sync_logs(tenant_id);
CREATE INDEX idx_metadata_sync_logs_created ON metadata.sync_logs(created_at DESC);
```

## API 设计

### Meta 模块 API

#### 1. 轻量级自动同步（Level 1）

```
POST /api/metadata/sync/auto
Request:
{
  "resource_id": 1,           // 数据源 ID（system.resources）
  "force": false              // 是否强制重新同步
}

Response:
{
  "sync_id": 123,
  "status": "running",
  "message": "开始同步数据库列表"
}
```

#### 2. 深度扫描（Level 2）

```
POST /api/metadata/scan/deep
Request:
{
  "resource_id": 1,
  "database": "business",     // 指定数据库
  "include_tables": true,     // 是否扫描表
  "include_fields": true,     // 是否扫描字段
  "include_statistics": false // 是否采样统计（未来功能）
}

Response:
{
  "scan_id": 124,
  "status": "running",
  "estimated_duration": "30s"
}
```

#### 3. 查询元数据

```
# 获取数据源列表
GET /api/metadata/datasources?tenant_id=1

# 获取数据库列表（Level 1 数据）
GET /api/metadata/databases?resource_id=1

# 获取表列表（Level 2 数据）
GET /api/metadata/tables?database_id=5

# 获取字段列表（Level 2 数据）
GET /api/metadata/fields?table_id=100

# 获取同步日志
GET /api/metadata/sync-logs?resource_id=1&limit=10
```

#### 4. 检查扫描状态

```
GET /api/metadata/scan/status/:scan_id

Response:
{
  "scan_id": 124,
  "status": "success",
  "started_at": "2025-10-04T12:00:00Z",
  "completed_at": "2025-10-04T12:00:25Z",
  "duration_seconds": 25,
  "tables_scanned": 50,
  "fields_scanned": 500
}
```

### Manager 模块 API（调整）

#### 1. 获取可纳管数据库列表

```
GET /api/manager/datasources/:id/available-databases

Response:
{
  "resource_id": 1,
  "resource_name": "业务数据库",
  "databases": [
    {
      "database_id": 5,
      "database_name": "business",
      "table_count": 50,
      "total_size_mb": 1024,
      "is_scanned": false,
      "last_sync_at": "2025-10-04T00:00:00Z",
      "is_managed": false
    },
    {
      "database_id": 6,
      "database_name": "analytics",
      "table_count": 20,
      "is_scanned": true,
      "is_managed": true
    }
  ]
}
```

#### 2. 纳管数据库/表

```
POST /api/manager/manage
Request:
{
  "resource_id": 1,
  "database": "business",
  "tables": ["users", "orders"],  // 可选，不填则纳管整个数据库
  "trigger_deep_scan": true,      // 是否触发深度扫描
  "permission": "read"            // read, write, admin
}

Response:
{
  "status": "success",
  "managed_objects": [
    {
      "database": "business",
      "table": "users",
      "status": "scanning"
    },
    {
      "database": "business",
      "table": "orders",
      "status": "scanning"
    }
  ],
  "scan_id": 125
}
```

#### 3. 获取已纳管列表

```
GET /api/manager/managed-objects

Response:
{
  "objects": [
    {
      "id": 1,
      "resource_id": 1,
      "resource_name": "业务数据库",
      "database": "business",
      "table": "users",
      "status": "ready",
      "permission": "read",
      "row_count": 10000,
      "managed_at": "2025-10-04T12:00:00Z"
    }
  ]
}
```

## 前端页面设计

### Manager 模块前端

#### 数据源纳管页面（DataSourceManage.vue）

**功能**：
1. 显示所有数据源列表（来自 System）
2. 点击数据源，展开可纳管的数据库列表（来自 Meta Level 1）
3. 选择数据库/表，触发纳管操作
4. 显示纳管状态和深度扫描进度

**页面结构**：
```
┌──────────────────────────────────────────────────┐
│  数据源纳管                                       │
├──────────────────────────────────────────────────┤
│  数据源列表                                       │
│  ├─ 业务数据库 (PostgreSQL) [已连接]             │
│  │   ├─ business  [未纳管] [纳管] [扫描中]       │
│  │   │   ├─ users (1000行) ☐                    │
│  │   │   ├─ orders (5000行) ☐                   │
│  │   │   └─ products (200行) ☐                  │
│  │   └─ analytics [已纳管✓] [查看详情]           │
│  ├─ 测试数据库 (MySQL) [已连接]                  │
│  └─ 对象存储 (MinIO) [已连接]                    │
└──────────────────────────────────────────────────┘
```

**操作流程**：
1. 用户点击"纳管"按钮
2. 弹出对话框，询问是否触发深度扫描
3. 提交后，调用 Manager API 创建纳管关系
4. Manager 调用 Meta API 触发深度扫描
5. 显示扫描进度，完成后状态变为"已纳管✓"

### Meta 模块前端

#### 元数据浏览页面（MetadataBrowser.vue）

**功能**：
1. 树形结构展示所有元数据（数据源 → 数据库 → 表 → 字段）
2. 显示元数据详情（类型、大小、注释等）
3. 查看同步历史和状态

**页面结构**：
```
┌──────────────────────────────────────────────────┐
│  元数据浏览                                       │
├──────────────────────────────────────────────────┤
│  ┌─ 树形视图 ──┐  ┌─ 详情面板 ─────────────────┐│
│  │ 📦 业务数据库  │  │  数据库: business         ││
│  │  ├─ 📁 business│  │  类型: PostgreSQL         ││
│  │  │  ├─ 📊 users│  │  表数: 50                ││
│  │  │  │  ├─ id  │  │  大小: 1.2GB             ││
│  │  │  │  ├─ name│  │  最后同步: 2h ago        ││
│  │  │  │  └─ ...  │  │                          ││
│  │  │  ├─ orders │  │  表: users                ││
│  │  │  └─ ...    │  │  字段数: 15               ││
│  │  └─ analytics │  │  行数: ~10,000           ││
│  └──────────────┘  │  引擎: InnoDB            ││
│                    └──────────────────────────────┘│
└──────────────────────────────────────────────────┘
```

## 实现步骤

### Phase 1: Meta 模块后端（当前）

1. ✅ 设计数据库表结构
2. ⏳ 创建 Meta 模块项目结构
3. ⏳ 实现 Level 1 自动同步逻辑
4. ⏳ 实现 Level 2 深度扫描逻辑
5. ⏳ 实现查询 API

### Phase 2: Manager 模块后端调整

1. ⏳ 添加纳管关系表（manager.managed_objects）
2. ⏳ 实现纳管 API
3. ⏳ 集成 Meta 模块（调用深度扫描）
4. ⏳ 调整预览功能（基于纳管关系）

### Phase 3: Manager 前端

1. ⏳ 创建数据源纳管页面
2. ⏳ 实现纳管流程和状态显示
3. ⏳ 集成扫描进度展示

### Phase 4: Meta 前端

1. ⏳ 创建元数据浏览页面
2. ⏳ 实现树形结构展示
3. ⏳ 添加详情面板

## 配置项

### Meta 模块 (.env)

```bash
# Meta 模块端口
PORT=8082

# 数据库配置
DB_HOST=localhost
DB_PORT=5432
DB_NAME=addp
DB_USER=addp
DB_PASSWORD=addp_password
DB_SCHEMA=metadata

# System 模块地址（获取资源连接信息）
SYSTEM_SERVICE_URL=http://localhost:8080

# 自动同步配置
META_AUTO_SYNC_ENABLED=true
META_AUTO_SYNC_SCHEDULE="0 0 * * *"  # 每晚12点
META_AUTO_SYNC_LEVEL=database         # database | table | field
META_DEEP_SCAN_TIMEOUT=300s
META_DEEP_SCAN_BATCH_SIZE=100         # 批量扫描表数量

# 服务集成
ENABLE_SERVICE_INTEGRATION=true
```

### Manager 模块调整

```bash
# Manager 模块地址
META_SERVICE_URL=http://localhost:8082

# 纳管配置
MANAGER_AUTO_TRIGGER_DEEP_SCAN=true   # 纳管时自动触发深度扫描
MANAGER_SCAN_TIMEOUT=300s
```

## 关键技术点

### 1. 并发控制

- 同一数据源同时只能有一个扫描任务运行
- 使用数据库锁或 Redis 分布式锁

### 2. 性能优化

- Level 1 同步：批量查询，只获取数据库列表
- Level 2 扫描：分批扫描表，避免一次性加载所有元数据
- 使用连接池复用数据库连接

### 3. 错误处理

- 扫描失败时记录详细错误信息
- 支持部分成功（某些表扫描失败不影响其他表）
- 提供重试机制

### 4. 增量同步（未来）

- 检测数据库变更（新增/删除表）
- 只同步有变化的部分
- 基于时间戳或版本号

## 下一步行动

请确认此设计方案是否符合预期。确认后我将开始实施：

1. 创建 Meta 模块的 Go 项目结构
2. 实现数据库表和模型
3. 实现 Level 1 自动同步 API
4. 实现 Level 2 深度扫描 API
5. 调整 Manager 模块纳管流程
6. 实现前端页面

**预估工作量**：
- Meta 后端：4-6小时
- Manager 后端调整：2-3小时
- Manager 前端：3-4小时
- Meta 前端：2-3小时

**总计**：约 11-16小时（分多次迭代完成）
