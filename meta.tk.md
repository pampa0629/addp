

⏺ 好的，我已经全面分析了 Meta 模块的架构。让我为您详细梳理当前的交互方式并提出优化建议：

  📊 Meta 模块当前架构分析

  1️⃣ 三层数据库交互架构

  Meta 模块运行时的数据库交互:

  ┌─────────────────────────────────────────────────────────────┐
  │                     Meta 模块 (8082)                         │
  │                                                              │
  │  ┌────────────────────────────────────────────────────┐    │
  │  │  1. 配置加载阶段 (config.LoadConfig)                │    │
  │  │     - 从 System 服务获取 DB 连接配置               │    │
  │  │     - SystemServiceURL + /internal/config          │    │
  │  └──────────────────┬─────────────────────────────────┘    │
  │                     │ HTTP                                  │
  │                     ▼                                       │
  │  ┌────────────────────────────────────────────────────┐    │
  │  │  2. 系统库连接 (repository.InitDatabase)            │    │
  │  │     PostgreSQL: addp 数据库 / metadata schema      │    │
  │  │     - datasources (关联 system.resources)          │    │
  │  │     - databases, tables, fields                    │    │
  │  │     - sync_logs                                    │    │
  │  └──────────────────┬─────────────────────────────────┘    │
  │                     │ GORM                                  │
  │                     ▼                                       │
  │  ┌────────────────────────────────────────────────────┐    │
  │  │  3. 业务库元数据提取 (scanner.Scanner)              │    │
  │  │     - 通过 SystemClient 获取 Resource 连接信息     │    │
  │  │     - 解密连接密码                                 │    │
  │  │     - 建立到业务库的临时连接                        │    │
  │  │     - 扫描 INFORMATION_SCHEMA                      │    │
  │  │     - 提取元数据后关闭连接                          │    │
  │  └────────────────────────────────────────────────────┘    │
  └─────────────────────────────────────────────────────────────┘

  2️⃣ 关键交互流程

  A. 与 System 模块的交互

  // router.go:27 - 创建 SystemClient
  systemClient := client.NewSystemClient(cfg.SystemServiceURL, "")

  // sync_service.go:39 - 获取资源列表
  resources, err := s.systemClient.ListResources("")

  // sync_service.go:179 - 获取单个资源
  resource, err := s.systemClient.GetResource(resourceID)

  用途：
  1. 配置获取：启动时获取 DB 连接配置（JWT_SECRET, DB 连接信息）
  2. 资源查询：查询 System 的 resources 表获取业务数据库连接信息
  3. 认证验证：通过 AuthMiddleware 验证 JWT Token

  B. 与系统库（PostgreSQL metadata schema）的交互

  // database.go:19 - 建立系统库连接
  dsn := fmt.Sprintf("host=%s port=%s user=%s password=%s dbname=%s sslmode=disable search_path=%s",
      cfg.DBHost, cfg.DBPort, cfg.DBUser, cfg.DBPassword, cfg.DBName, cfg.DBSchema)

  用途：
  1. 存储元数据：将扫描得到的元数据持久化
  2. 租户隔离：所有表都有 tenant_id 字段
  3. 关联关系：datasources.resource_id 关联 system.resources.id

  C. 与业务库的交互（临时连接）

  // sync_service.go:107 - 构建业务库连接
  connStr, err := commonModels.BuildConnectionString(resource)

  // sync_service.go:114 - 创建扫描器并连接业务库
  scan, err := scanner.NewScanner(resource.ResourceType, connStr)

  // sync_service.go:122 - 扫描元数据
  databases, err := scan.ScanDatabases()

  // sync_service.go:119 - 用完即关闭
  defer scan.Close()

  用途：
  1. 元数据提取：连接业务库的 INFORMATION_SCHEMA
  2. 按需扫描：Level 1 (databases) → Level 2 (tables) → Level 3 (fields)
  3. 临时连接：扫描完成立即关闭，不保持长连接

  ---
  ✅ 合理性分析

  优点：

  1. ✅ 职责清晰
    - System 模块：配置中心 + 资源管理 + 认证
    - Meta 模块：元数据提取 + 存储 + 查询
  2. ✅ 安全性好
    - 业务库密码在 System 中加密存储
    - Meta 通过 API 获取，自动解密
    - 不在 Meta 模块中存储明文密码
  3. ✅ 租户隔离
    - 所有元数据表都带 tenant_id
    - 查询自动过滤租户数据
  4. ✅ 临时连接策略
    - 业务库连接用完即关闭
    - 避免连接池泄漏
    - 减少对业务库的影响
  5. ✅ 异步扫描
  go func(r commonModels.Resource) {
      s.syncResourceInternal(&r, r.TenantID)
  }(resource)
    - 不阻塞主流程
    - 支持批量扫描

  ---
  ⚠️ 存在的问题

  问题 1：SystemClient 无认证 Token

  // router.go:27 - 创建时传了空字符串
  systemClient := client.NewSystemClient(cfg.SystemServiceURL, "")

  问题：
  - SystemClient 调用 System API 时没有 JWT Token
  - 如果 System 的 /internal/config 需要认证会失败
  - 服务间调用应该有专门的认证机制

  建议：
  // 方案1: 使用内部 API Key
  systemClient := client.NewSystemClient(cfg.SystemServiceURL, cfg.InternalAPIKey)

  // 方案2: 在配置加载时获取服务间通信的 JWT
  // common/config/loader.go 中可以改为使用 INTERNAL_API_KEY 认证

  问题 2：每次扫描都调用 GetResource

  // sync_service.go:190 - getOrCreateDatasource 中
  resource, err := s.systemClient.GetResource(resourceID)

  // sync_service.go:179 - SyncResource 中
  resource, err := s.systemClient.GetResource(resourceID)

  问题：
  - 在 AutoSyncAll 中已经获取了所有 resources
  - 异步 goroutine 中又重新调用 GetResource
  - 重复的网络调用，增加延迟

  已优化（代码中已经做对了）：
  // sync_service.go:64 - 直接传递 resource 对象
  go func(r commonModels.Resource) {
      s.syncResourceInternal(&r, r.TenantID)
  }(resource)
  ✅ 这部分已经优雅了

  问题 3：资源信息冗余存储

  // datasource.go:12 - MetadataDatasource 存储了冗余信息
  DatasourceName string  // 从 system.resources.name 复制
  DatasourceType string  // 从 system.resources.resource_type 复制

  问题：
  - datasources 表通过 resource_id 关联 system.resources
  - 但又存储了 name 和 type，造成冗余
  - 如果 System 中修改了 resource name，这里不会自动更新

  建议两个方案：

  方案 A（推荐）: 保留冗余，定期同步
  // 优点: 查询性能好，不需要跨表 JOIN
  // 缺点: 需要同步机制

  // 在 syncResourceInternal 中更新名称
  datasource.DatasourceName = resource.ResourceName
  datasource.DatasourceType = resource.ResourceType
  s.db.Save(datasource)

  方案 B: 移除冗余，通过 JOIN 查询
  -- 查询时 JOIN system.resources
  SELECT d.*, r.name, r.resource_type
  FROM metadata.datasources d
  JOIN system.resources r ON d.resource_id = r.id
  缺点：跨 schema JOIN 性能较差

=======


  问题 4：数据库迁移被禁用

  // database.go:48 - AutoMigrate 被注释掉
  // TODO: 修复后重新启用
  // if err := autoMigrate(db); err != nil {
  //     return nil, fmt.Errorf("failed to auto migrate: %w", err)
  // }

  问题：
  - 表结构变更需要手动执行 SQL
  - 容易遗漏，造成生产环境问题

  建议：
  # 使用 golang-migrate 或类似工具
  # 1. 创建 migrations 目录
  mkdir -p meta/backend/migrations

  # 2. 生成迁移文件
  migrate create -ext sql -dir migrations -seq create_datasources_table

  # 3. 在代码中执行迁移
  import "github.com/golang-migrate/migrate/v4"

  问题 5：错误处理不够优雅

  // sync_service.go:66 - goroutine 中的错误只是 log，没有上报
  go func(r commonModels.Resource) {
      if err := s.syncResourceInternal(&r, r.TenantID); err != nil {
          log.Printf("Failed to sync resource %d: %v", r.ID, err)  // 只有 log
      }
  }(resource)

  问题：
  - 异步扫描失败后，用户无法感知
  - 只有 sync_logs 表记录了状态
  - API 返回成功，但实际可能失败

  建议：
  // 方案1: 返回 channel 让调用者可以等待
  func (s *SyncService) AutoSyncAll(tenantID uint) (<-chan error, error) {
      errChan := make(chan error, len(resources))
      // ...
      go func(r commonModels.Resource) {
          err := s.syncResourceInternal(&r, r.TenantID)
          errChan <- err
      }(resource)
      return errChan, nil
  }

  // 方案2: 使用 context 支持取消
  func (s *SyncService) AutoSyncAll(ctx context.Context, tenantID uint) error

  ---
  🎯 优化建议总结

  立即优化（高优先级）

  1. 修复 SystemClient 认证
  // config/config.go 添加字段
  InternalAPIKey string

  // router.go 使用配置的 API Key
  systemClient := client.NewSystemClient(cfg.SystemServiceURL, cfg.InternalAPIKey)
  2. 启用数据库迁移
    - 使用 golang-migrate 或手动执行初始化 SQL
    - 确保表结构正确创建
  3. 完善错误处理
    - 异步扫描错误通过 WebSocket 或轮询通知前端
    - 或者改为同步扫描，返回明确的成功/失败状态

  中期优化（可维护性）

  4. 统一配置管理
    - ✅ 已完成：使用 common/config 统一加载配置
  5. 添加重试机制
  // 扫描失败自动重试 3 次
  for i := 0; i < 3; i++ {
      if err := scan.ScanDatabases(); err == nil {
          break
      }
      time.Sleep(time.Second * time.Duration(i+1))
  }
  6. 连接池优化
  // 为业务库扫描也建立连接池（如果需要频繁扫描）
  // 但要注意：过多连接池会占用资源

  长期优化（性能）

  7. 缓存热点数据
  // 使用 Redis 缓存常用元数据查询结果
  cache.Set("datasources:list:tenant:1", datasources, 5*time.Minute)
  8. 增量同步
  // 只同步有变化的表（通过对比 last_modified_time）
  // MySQL: SELECT UPDATE_TIME FROM INFORMATION_SCHEMA.TABLES

  ---
  总结

  当前架构评分：7.5/10

  ✅ 做得好的地方：
  - 三层数据库架构清晰
  - 临时连接策略安全
  - 异步扫描不阻塞
  - 租户隔离完善
  - 已使用 common 模块消除配置重复

  ⚠️ 需要改进的地方：
  - SystemClient 缺少认证
  - 数据库迁移被禁用
  - 异步错误处理不完善
  - 缺少重试和缓存机制

  总体评价： 架构设计合理且优雅，主要问题在于实现细节和错误处理，修复上述问题后可达到 9/10 的水平。
