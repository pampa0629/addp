# Meta 模块实现状态

## 当前进度

### ✅ 已完成

1. **设计文档** (`DESIGN.md`)
   - 完整的数据库表设计（5张表）
   - API 设计（Meta + Manager 集成）
   - 前端页面设计
   - 实现步骤规划

2. **项目结构**
   - 创建了完整的目录结构
   - `go.mod` 文件
   - 配置管理 (`internal/config/config.go`)
   - 第一个模型文件 (`internal/models/datasource.go`)

### 🔄 进行中

**Meta 模块后端** - 需要创建的核心文件：

```
meta/backend/
├── internal/
│   ├── models/          ← 当前位置
│   │   ├── datasource.go     ✅ 已创建
│   │   ├── database.go       ⏳ 待创建
│   │   ├── table.go          ⏳ 待创建
│   │   ├── field.go          ⏳ 待创建
│   │   └── sync_log.go       ⏳ 待创建
│   ├── repository/
│   │   ├── database.go       ⏳ 数据库连接和迁移
│   │   ├── datasource_repo.go ⏳ 数据源仓库
│   │   ├── database_repo.go   ⏳ 数据库仓库
│   │   └── ...
│   ├── scanner/        ← 核心扫描逻辑
│   │   ├── mysql_scanner.go   ⏳ MySQL 扫描器
│   │   ├── postgres_scanner.go ⏳ PostgreSQL 扫描器
│   │   ├── scanner.go         ⏳ 扫描器接口
│   │   └── factory.go         ⏳ 扫描器工厂
│   ├── service/
│   │   ├── sync_service.go    ⏳ 同步服务
│   │   ├── scan_service.go    ⏳ 扫描服务
│   │   └── metadata_service.go ⏳ 元数据查询服务
│   ├── api/
│   │   ├── router.go          ⏳ 路由配置
│   │   ├── sync_handler.go    ⏳ 同步 API
│   │   ├── scan_handler.go    ⏳ 扫描 API
│   │   └── metadata_handler.go ⏳ 查询 API
│   └── middleware/
│       └── auth.go            ⏳ 认证中间件
└── cmd/server/
    └── main.go                ⏳ 应用入口
```

### ⏳ 待完成

#### Phase 1: Meta 后端核心功能（预计 4-6 小时）

1. **完成数据库模型**（30分钟）
   - database.go
   - table.go
   - field.go
   - sync_log.go

2. **实现数据库连接和迁移**（30分钟）
   - repository/database.go
   - 连接 PostgreSQL
   - AutoMigrate 所有表

3. **实现扫描器**（2小时）
   - MySQL 扫描器（支持 Level 1 + Level 2）
   - PostgreSQL 扫描器
   - 扫描器工厂

4. **实现服务层**（1.5小时）
   - 轻量级同步服务（Level 1）
   - 深度扫描服务（Level 2）
   - 元数据查询服务

5. **实现 API 层**（1小时）
   - 路由配置
   - 同步/扫描 Handler
   - 查询 Handler

6. **创建应用入口**（30分钟）
   - main.go
   - 启动服务
   - 测试基本功能

#### Phase 2: Manager 模块集成（预计 2-3 小时）

1. **添加纳管关系表**
   ```sql
   CREATE TABLE manager.managed_objects (
       id SERIAL PRIMARY KEY,
       resource_id INTEGER NOT NULL,
       tenant_id INTEGER NOT NULL,
       database VARCHAR(255),
       "table" VARCHAR(255),
       status VARCHAR(50) DEFAULT 'pending',
       permission VARCHAR(20) DEFAULT 'read',
       scan_id INTEGER,
       managed_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP,
       UNIQUE(resource_id, database, "table")
   );
   ```

2. **实现纳管 API**
   - GET /api/manager/datasources/:id/available-databases
   - POST /api/manager/manage
   - GET /api/manager/managed-objects

3. **集成 Meta 服务**
   - 创建 Meta 客户端
   - 调用深度扫描 API

#### Phase 3: Manager 前端（预计 3-4 小时）

1. **创建数据源纳管页面**
   - 数据源列表组件
   - 数据库/表选择组件
   - 纳管状态显示
   - 扫描进度展示

2. **实现交互逻辑**
   - 获取可纳管列表
   - 提交纳管请求
   - 轮询扫描状态

#### Phase 4: Meta 前端（预计 2-3 小时）

1. **创建元数据浏览页面**
   - 树形结构组件
   - 详情面板
   - 同步历史查看

## 下一步行动

### 建议的实施顺序：

**Option 1: 完整后端优先**（推荐）
1. 完成 Meta 后端所有代码
2. 调整 Manager 后端
3. 实现 Manager 前端
4. 实现 Meta 前端

**优点**:
- 可以先用 API 测试完整功能
- 前端开发时后端稳定
- 问题集中解决

**Option 2: 垂直切片**
1. 实现 Level 1 轻量级同步（Meta 后端 + API）
2. 实现纳管流程（Manager 后端 + 前端）
3. 实现 Level 2 深度扫描（Meta 后端 + API）
4. 完善前端展示（Manager + Meta）

**优点**:
- 可以尽快看到可用的功能
- 用户可以早期反馈
- 迭代式开发

### 推荐方案

**采用 Option 1（完整后端优先）**，因为：
1. Meta 和 Manager 耦合较紧，一次性实现更高效
2. 避免频繁切换上下文
3. 可以用 Postman/curl 测试完整的数据流
4. 前端开发时 API 已经稳定

### 预计总耗时

- **Meta 后端**: 4-6 小时
- **Manager 后端调整**: 2-3 小时
- **Manager 前端**: 3-4 小时
- **Meta 前端**: 2-3 小时

**总计**: 11-16 小时

建议分 3-4 次完成：
1. Session 1 (4小时): Meta 后端核心功能
2. Session 2 (3小时): Manager 后端调整 + 部分前端
3. Session 3 (4小时): 完成 Manager 前端
4. Session 4 (3小时): Meta 前端 + 整体测试

## 技术债务和优化

### 当前未实现的功能（可后续增强）

1. **增量同步**
   - 检测数据库变更
   - 只同步有变化的部分
   - 基于时间戳/版本号

2. **统计信息采样**
   - 字段唯一值统计
   - NULL 比例
   - 数据分布

3. **定时任务**
   - 自动同步调度（Cron）
   - 任务队列管理

4. **性能优化**
   - Redis 缓存元数据
   - 并发扫描多个表
   - 分页加载大表字段

5. **错误恢复**
   - 断点续传
   - 自动重试失败的扫描

## 当前可演示的功能

完成 Phase 1 后，可以演示：

1. ✅ 创建数据源后，Meta 自动获取数据库列表
2. ✅ 手动触发深度扫描，获取表和字段信息
3. ✅ 通过 API 查询元数据
4. ✅ 查看同步日志和状态

完成 Phase 2 后，可以演示：

5. ✅ 在 Manager 中浏览可纳管的数据库
6. ✅ 选择数据库/表进行纳管
7. ✅ 纳管时自动触发深度扫描
8. ✅ 查看已纳管对象列表

完成 Phase 3-4 后，可以演示完整的用户流程。

## 风险和注意事项

1. **数据库连接池管理**
   - 扫描时会创建多个数据库连接
   - 需要合理设置连接池大小
   - 避免连接泄漏

2. **大规模数据源性能**
   - 数据库很多（>100个）时，Level 1 同步可能较慢
   - 表很多（>1000张）时，Level 2 扫描需要分批
   - 需要设置合理的超时时间

3. **租户隔离**
   - 确保所有查询都包含 tenant_id 过滤
   - 防止跨租户数据泄露

4. **并发扫描控制**
   - 同一数据源不能同时有多个扫描任务
   - 需要使用锁机制（数据库锁或分布式锁）

## 需要确认的问题

在继续实施前，请确认：

1. ✅ 数据库表设计是否符合预期？
2. ✅ API 设计是否满足需求？
3. ✅ 前端页面结构是否清晰？
4. ❓ 是否需要先实现 Level 1，再实现 Level 2？还是一次性实现？
5. ❓ 是否需要定时任务功能？还是先做手动触发？
6. ❓ 是否需要支持 MongoDB 等 NoSQL 数据库？

## 继续实施

确认无误后，我将按照以下顺序继续实施：

1. 完成所有数据库模型
2. 实现数据库连接和迁移
3. 实现 PostgreSQL 扫描器（优先）
4. 实现 MySQL 扫描器
5. 实现服务层
6. 实现 API 层
7. 创建 main.go 并测试

请告知是否可以继续，或者需要调整方案！
