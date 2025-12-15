# ADDP 集成 Apache Doris 实施方案

## 项目目标
将 Apache Doris 作为业务数据库引擎集成到 ADDP 平台，提供高性能 OLAP 分析能力。

---

## 一、需求分析

### 1.1 核心功能需求

1. **引擎注册**: 将 Doris 注册为数据库引擎到 System 模块（ResourceType = "doris"）
2. **业务库部署**: 在 `business/` 目录独立部署 Doris 集群（遵循 ADDP 业务库分离原则）
3. **资源管理**: 通过 System 的 Resource 模型管理 Doris 连接信息（加密存储）
4. **元数据扫描**: Meta 模块支持扫描 Doris 的数据库/表/字段
5. **数据预览**: Manager 模块支持预览 Doris 表数据
6. **数据传输**: Transfer 模块支持从/到 Doris 的数据导入导出
7. **SQL 工作台**: Develop 模块支持执行 Doris SQL 查询

### 1.2 技术约束

- **协议兼容性**: Doris 兼容 MySQL 协议（端口 9030），可复用 MySQL 连接代码
- **部署隔离**: Doris 作为业务库，部署在 `business/docker-compose.yml`
- **端口分配**: 避免与现有服务冲突
  - FE Query Port: 9030 (MySQL 协议)
  - FE HTTP Port: 8030
  - BE Webserver: 8040
- **最小资源**: FE 至少 2GB RAM，BE 至少 4GB RAM

---

## 二、实施方案（分阶段）

### 阶段 1: 业务库部署（`business/` 目录）

**目标**: 在 `business/docker-compose.yml` 中添加 Doris 服务

#### 1.1 Docker Compose 配置

**文件**: `business/docker-compose.yml`

```yaml
services:
  doris-fe:
    image: apache/doris:2.0.14-fe-x86_64
    container_name: business-doris-fe
    environment:
      - FE_SERVERS=fe1:172.20.80.2:9010
      - FE_ID=1
    ports:
      - "9030:9030"  # MySQL 协议
      - "8030:8030"  # HTTP API
    volumes:
      - doris_fe_data:/opt/apache-doris/fe/doris-meta
      - doris_fe_log:/opt/apache-doris/fe/log
    networks:
      business-network:
        ipv4_address: 172.20.80.2

  doris-be:
    image: apache/doris:2.0.14-be-x86_64
    container_name: business-doris-be
    environment:
      - FE_SERVERS=fe1:172.20.80.2:9010
      - BE_ADDR=172.20.80.3:9050
    ports:
      - "8040:8040"  # BE Webserver
      - "9060:9060"  # BE Heartbeat
    volumes:
      - doris_be_data:/opt/apache-doris/be/storage
      - doris_be_log:/opt/apache-doris/be/log
    networks:
      business-network:
        ipv4_address: 172.20.80.3
    depends_on:
      - doris-fe

volumes:
  doris_fe_data:
  doris_fe_log:
  doris_be_data:
  doris_be_log:

networks:
  business-network:
    driver: bridge
    ipam:
      config:
        - subnet: 172.20.80.0/24
```

#### 1.2 初始化脚本

**文件**: `business/init-doris.sh`

```bash
#!/bin/bash
# 等待 FE 启动
sleep 30

# 添加 BE 节点
mysql -h127.0.0.1 -P9030 -uroot <<EOF
ALTER SYSTEM ADD BACKEND "172.20.80.3:9050";
SHOW BACKENDS\G
EOF

# 创建测试数据库和表
mysql -h127.0.0.1 -P9030 -uroot <<EOF
CREATE DATABASE IF NOT EXISTS test_db;
USE test_db;

-- Duplicate 模型示例
CREATE TABLE IF NOT EXISTS user_events (
    event_time DATETIME,
    user_id BIGINT,
    event_type VARCHAR(50),
    page_url VARCHAR(200)
)
DUPLICATE KEY(event_time, user_id)
DISTRIBUTED BY HASH(user_id) BUCKETS 10;

-- Aggregate 模型示例
CREATE TABLE IF NOT EXISTS sales_agg (
    date DATE,
    product_id INT,
    sales_amount DECIMAL(10,2) SUM,
    order_count INT SUM
)
AGGREGATE KEY(date, product_id)
DISTRIBUTED BY HASH(product_id) BUCKETS 10;
EOF
```

#### 1.3 环境变量配置

**文件**: `business/.env`

```bash
# Doris Configuration
DORIS_FE_HOST=business-doris-fe
DORIS_FE_PORT=9030
DORIS_FE_HTTP_PORT=8030
DORIS_BE_HOST=business-doris-be
DORIS_BE_PORT=8040
DORIS_ROOT_PASSWORD=  # 默认为空
```

#### 1.4 文档更新

**文件**: `business/README.md`

添加以下内容：

```markdown
## Doris 服务

Apache Doris 是一个基于 MPP 架构的高性能实时分析数据库。

### 访问方式
- **MySQL 协议**: `mysql -h127.0.0.1 -P9030 -uroot`
- **FE Web UI**: http://localhost:8030
- **BE Web UI**: http://localhost:8040

### 默认账户
- 用户名: root
- 密码: (空)

### 端口说明
- 9030: MySQL 协议端口
- 8030: FE HTTP API
- 8040: BE Webserver
- 9060: BE Heartbeat

### 常用命令
```bash
# 查看 BE 节点
mysql -h127.0.0.1 -P9030 -uroot -e "SHOW BACKENDS\G"

# 查看数据库
mysql -h127.0.0.1 -P9030 -uroot -e "SHOW DATABASES;"
```
```

---

### 阶段 2: System 模块支持 Doris 资源类型

**目标**: 扩展 Resource 模型支持 Doris 连接信息

#### 2.1 后端 - ConnectionInfo 结构定义

**文件**: `common/models/resource.go`

在 `BuildConnectionString` 函数中添加 Doris 支持：

```go
// 在 BuildConnectionString 函数的 switch 语句中添加 case
case "doris":
    // Doris 使用 MySQL 协议，连接字符串格式与 MySQL 相同
    return fmt.Sprintf("%s:%s@tcp(%s:%s)/%s",
        username, password, host, port, database), nil
```

**修改位置**: 在现有的 `case "mysql":` 之后添加

#### 2.2 前端 - 表单支持

**文件**: `common-frontend/basic/src/components/StorageEngineForm.vue`

添加 Doris 选项：

```javascript
// 在 resourceTypes 数组中添加
{ value: 'doris', label: 'Apache Doris' }

// 在 formFields 对象中添加 Doris 配置
doris: [
  { key: 'host', label: '主机', required: true, placeholder: 'business-doris-fe 或 127.0.0.1' },
  { key: 'port', label: '端口', required: true, default: '9030' },
  { key: 'user', label: '用户名', required: true, default: 'root' },
  { key: 'password', label: '密码', type: 'password', required: false },
  { key: 'database', label: '数据库', required: true, placeholder: 'test_db' }
]
```

#### 2.3 资源创建示例

用户在 System 前端创建 Doris 资源时的配置示例：

```json
{
  "name": "doris_business",
  "display_name": "业务 Doris 数据库",
  "resource_type": "doris",
  "connection_info": {
    "host": "business-doris-fe",
    "port": "9030",
    "user": "root",
    "password": "",
    "database": "test_db"
  },
  "scan_config": {
    "schedule_type": "manual",
    "enabled": true
  }
}
```

---

### 阶段 3: Meta 模块支持 Doris 扫描

**目标**: 扫描 Doris 的数据库、表、字段元数据

#### 3.1 扫描器实现

**文件**: `meta/backend/internal/service/scan_service.go`

在 `scanDatabaseMetadata` 函数的 switch 语句中添加：

```go
// Doris 使用 MySQL 协议，扫描逻辑与 MySQL 相同
case "doris":
    return s.scanMySQLMetadata(ctx, resource, scanType)
```

**关键点**:
- Doris 完全兼容 MySQL 协议
- 复用现有 MySQL 扫描逻辑：
  - `SELECT SCHEMA_NAME FROM information_schema.SCHEMATA`
  - `SELECT TABLE_NAME FROM information_schema.TABLES WHERE TABLE_SCHEMA = ?`
  - `SELECT COLUMN_NAME, DATA_TYPE FROM information_schema.COLUMNS WHERE TABLE_SCHEMA = ? AND TABLE_NAME = ?`

#### 3.2 Doris 特有元数据扩展（可选 - P2优先级）

如需支持 Doris 特有信息（表模型、分区、副本），可添加额外查询：

```sql
-- 获取表模型类型
SELECT TABLE_NAME, ENGINE FROM information_schema.TABLES
WHERE TABLE_SCHEMA = 'test_db';

-- 获取分区信息
SHOW PARTITIONS FROM test_db.user_events;

-- 获取副本状态
SHOW TABLET FROM test_db.user_events;
```

在 `meta_item.attributes` JSONB 字段中存储：

```json
{
  "table_name": "user_events",
  "table_model": "DUPLICATE",
  "bucket_count": 10,
  "replica_count": 1,
  "partition_type": "UNPARTITIONED"
}
```

---

### 阶段 4: Manager 模块支持 Doris 数据预览

**目标**: 在 Manager 前端预览 Doris 表数据

#### 4.1 后端 API 扩展

**文件**: `manager/backend/internal/service/preview_service.go`

添加 Doris 预览函数：

```go
func (s *PreviewService) PreviewDorisTable(resourceID uint, tableName string, limit int) ([]map[string]interface{}, error) {
    // 1. 从 System 获取资源
    resource, err := s.systemClient.GetResource(resourceID)
    if err != nil {
        return nil, err
    }

    // 2. 构建连接字符串
    connStr, err := commonModels.BuildConnectionString(resource)
    if err != nil {
        return nil, err
    }

    // 3. 连接数据库
    db, err := sql.Open("mysql", connStr)
    if err != nil {
        return nil, err
    }
    defer db.Close()

    // 4. 查询数据
    query := fmt.Sprintf("SELECT * FROM %s LIMIT %d", tableName, limit)
    rows, err := db.Query(query)
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    // 5. 解析结果为 JSON
    return parseRowsToJSON(rows)
}
```

#### 4.2 前端组件复用

Doris 表预览复用现有的 `TablePreview` 组件（`common-frontend/map/src/components/TablePreview.vue`），无需修改。

---

### 阶段 5: Develop 模块支持 Doris SQL 查询

**目标**: 在 SQL 工作台执行 Doris 查询

#### 5.1 后端 - SQL 执行器

**文件**: `develop/backend/internal/service/query_service.go`

在 `ExecuteSQL` 函数中添加 Doris 支持：

```go
func (s *QueryService) ExecuteSQL(resourceID uint, sqlText string) (*QueryResult, error) {
    resource, err := s.systemClient.GetResource(resourceID)
    if err != nil {
        return nil, err
    }

    switch resource.ResourceType {
    case "postgresql":
        return s.executePostgreSQL(resource, sqlText)
    case "mysql", "doris":
        return s.executeMySQL(resource, sqlText)  // Doris 复用 MySQL 执行器
    default:
        return nil, fmt.Errorf("unsupported resource type: %s", resource.ResourceType)
    }
}
```

#### 5.2 前端 - 数据源选择器

**文件**: `develop/frontend/src/views/SQLWorkbench.vue`

在数据源过滤中添加 'doris'：

```javascript
// 支持选择 Doris 类型的资源
const datasources = computed(() => {
  return resources.value.filter(r =>
    ['postgresql', 'mysql', 'doris'].includes(r.resource_type)
  )
})
```

---

### 阶段 6: Transfer 模块支持 Doris 数据导入导出（P1优先级）

**目标**: 从 Doris 导出数据，或向 Doris 导入数据

#### 6.1 导出任务

**文件**: `transfer/backend/internal/worker/extractor.go`

```go
func (w *Worker) ExtractFromDoris(task *models.Task) error {
    // 1. 连接 Doris
    db, err := sql.Open("mysql", connStr)
    if err != nil {
        return err
    }
    defer db.Close()

    // 2. 执行查询
    rows, err := db.Query(task.SourceQuery)
    if err != nil {
        return err
    }
    defer rows.Close()

    // 3. 写入目标（CSV/JSON/Parquet）
    return w.writeToTarget(rows, task.TargetConfig)
}
```

#### 6.2 导入任务（推荐使用 Stream Load）

**文件**: `transfer/backend/internal/worker/loader.go`

```go
func (w *Worker) LoadToDoris(task *models.Task) error {
    // 使用 Doris Stream Load API
    url := fmt.Sprintf("http://%s:%s/api/%s/%s/_stream_load",
        feHost, feHTTPPort, database, table)

    req, err := http.NewRequest("PUT", url, dataReader)
    if err != nil {
        return err
    }

    req.SetBasicAuth(user, password)
    req.Header.Set("format", "json")  // 或 csv
    req.Header.Set("column_separator", ",")

    resp, err := w.client.Do(req)
    if err != nil {
        return err
    }
    defer resp.Body.Close()

    return parseStreamLoadResult(resp)
}
```

**优势**: Stream Load 比 INSERT 性能高 10-100 倍

---

## 三、关键技术点

### 3.1 Doris vs MySQL 差异

| 特性 | MySQL | Doris |
|------|-------|-------|
| **架构** | 单机关系型数据库 | MPP 分布式分析数据库 |
| **适用场景** | OLTP（事务处理） | OLAP（分析查询） |
| **表模型** | 仅支持行存储 | 4种模型（Duplicate/Aggregate/Unique/Primary Key） |
| **数据分布** | 无分区分桶概念 | 分区（Partition）+ 分桶（Bucket） |
| **索引** | B+树、全文索引 | 前缀索引、Bitmap、Bloom Filter、倒排索引 |
| **聚合性能** | 一般 | 极高（列存储 + 预聚合） |
| **数据导入** | INSERT | Stream Load（高性能）、Broker Load、Routine Load |
| **物化视图** | 手动维护 | 自动路由和更新 |

### 3.2 为什么 Doris 适合 ADDP

1. **兼容 MySQL 协议**：零成本复用现有代码（扫描、预览、查询）
2. **高性能分析**：亿级数据秒级响应，适合 OLAP 场景
3. **实时数据导入**：Stream Load 支持高吞吐实时写入
4. **简化运维**：单一系统替代 Hive + Spark，降低复杂度
5. **外部表支持**：可联邦查询 S3/HDFS 上的数据湖
6. **多租户隔离**：通过数据库级别权限控制

### 3.3 ADDP 中的 Doris 使用场景

| 场景 | ADDP 模块 | Doris 能力 |
|------|-----------|-----------|
| **元数据扫描** | Meta | 扫描 Doris 数据库/表/字段元数据 |
| **数据预览** | Manager | 高性能查询预览（百万行数据秒级响应） |
| **SQL 分析** | Develop | 交互式 SQL 查询，支持复杂 OLAP |
| **数据导入** | Transfer | Stream Load 高性能批量导入 |
| **实时报表** | Orchestrator | 编排 Kafka → Doris 实时管道 |
| **数据湖加速** | Develop | 通过外部表查询 S3/MinIO 数据 |

---

## 四、实施建议

### 4.1 最小可行方案（MVP）

**优先级 P0**（核心功能，必须实现）:
1. ✅ 业务库部署（business/docker-compose.yml）
2. ✅ System 支持 Doris 资源类型（common/models/resource.go）
3. ✅ Meta 支持 Doris 扫描（复用 MySQL 扫描器）
4. ✅ Develop 支持 Doris SQL 查询（复用 MySQL 执行器）

**优先级 P1**（增强功能）:
5. Manager 支持 Doris 表数据预览
6. Transfer 支持 Doris 数据导入导出（Stream Load）

**优先级 P2**（可选功能）:
7. Doris 特有元数据扩展（表模型、分区信息）
8. Orchestrator 编排 Doris 数据管道
9. 性能监控和告警（Prometheus + Grafana）

### 4.2 代码修改清单

| 模块 | 文件路径 | 修改内容 | 工作量 |
|------|---------|---------|--------|
| **Business** | business/docker-compose.yml | 添加 doris-fe、doris-be 服务 | 1小时 |
| **Business** | business/init-doris.sh | 初始化 BE 节点和测试数据 | 30分钟 |
| **Business** | business/README.md | 添加 Doris 文档 | 30分钟 |
| **Common** | common/models/resource.go | BuildConnectionString 添加 doris case | 10分钟 |
| **Common Frontend** | common-frontend/basic/src/components/StorageEngineForm.vue | 添加 Doris 表单字段 | 20分钟 |
| **Meta** | meta/backend/internal/service/scan_service.go | scanDatabaseMetadata 添加 doris case | 5分钟 |
| **Develop** | develop/backend/internal/service/query_service.go | ExecuteSQL 添加 doris case | 5分钟 |
| **Develop** | develop/frontend/src/views/SQLWorkbench.vue | 数据源过滤添加 'doris' | 5分钟 |
| **Manager** | manager/backend/internal/service/preview_service.go | 添加 PreviewDorisTable 函数 | 30分钟 |
| **Transfer** | transfer/backend/internal/worker/loader.go | 添加 LoadToDoris (Stream Load) | 2小时 |

**总工作量估算**: 约 5 小时（MVP 版本约 2小时）

### 4.3 测试验证清单

**部署验证**:
- [ ] Doris FE 启动成功（`SHOW FRONTENDS`）
- [ ] Doris BE 添加成功（`SHOW BACKENDS`）
- [ ] MySQL 客户端连接成功（`mysql -h127.0.0.1 -P9030 -uroot`）

**功能验证**:
- [ ] System 前端创建 Doris 资源成功
- [ ] Meta 模块扫描 Doris 元数据成功
- [ ] Develop 工作台执行 Doris SQL 成功
- [ ] Manager 预览 Doris 表数据成功
- [ ] Transfer 向 Doris 导入数据成功

**性能验证**:
- [ ] 百万行数据聚合查询 < 1秒
- [ ] Stream Load 导入 10万行/秒
- [ ] 并发查询不阻塞

---

## 五、风险和注意事项

### 5.1 技术风险

1. **资源消耗**：Doris BE 默认占用较多内存（4-8GB），需确保宿主机资源充足
2. **数据一致性**：Doris 最终一致性模型，不适合强事务场景
3. **版本兼容性**：Doris 2.0+ 与 1.x 有 breaking changes，建议使用 2.0.14+
4. **BE 节点管理**：手动添加 BE 节点（ALTER SYSTEM ADD BACKEND），需自动化

### 5.2 解决方案

1. **资源优化**：
   - 开发环境使用最小配置（FE 2GB, BE 4GB）
   - 通过 `mem_limit` 限制内存使用
   - 使用 Docker resource limits

2. **自动化 BE 注册**：
   - 在 `business/init-doris.sh` 中自动添加 BE
   - 使用 health check 等待 FE 完全启动

3. **版本管理**：
   - 固定 Docker 镜像版本（`apache/doris:2.0.14-fe-x86_64`）
   - 在文档中标注兼容版本

### 5.3 最佳实践

1. **分区策略**：根据查询模式选择时间分区或范围分区
2. **分桶数量**：通常设置为 BE 节点数的 2-4 倍
3. **副本数量**：单节点设置为 1，生产环境至少 3
4. **索引使用**：高基数字段使用 Bitmap 索引，低基数使用 Bloom Filter
5. **数据导入**：优先使用 Stream Load，避免使用 INSERT（性能差）

---

## 六、下一步行动

### 6.1 立即开始（第一天）

1. **部署 Doris 业务库**：
   ```bash
   cd business
   # 编辑 docker-compose.yml 添加 doris-fe 和 doris-be
   docker-compose up -d doris-fe doris-be
   bash init-doris.sh
   ```

2. **验证连接**：
   ```bash
   mysql -h127.0.0.1 -P9030 -uroot
   SHOW DATABASES;
   USE test_db;
   SHOW TABLES;
   ```

3. **创建第一个表**：
   ```sql
   CREATE TABLE user_events (
       event_time DATETIME,
       user_id BIGINT,
       event_type VARCHAR(50)
   )
   DUPLICATE KEY(event_time, user_id)
   DISTRIBUTED BY HASH(user_id) BUCKETS 10;
   ```

### 6.2 第一周任务

- [ ] 完成业务库 Doris 部署
- [ ] System 模块支持 Doris 资源类型
- [ ] Meta 模块扫描 Doris 元数据
- [ ] Develop 模块执行 Doris SQL

### 6.3 后续规划

- **第二周**：完善 Manager 预览和 Transfer 导入导出
- **第三周**：Doris 特有元数据扩展（可选）
- **第四周**：性能优化和监控

---

## 七、总结

### 7.1 核心优势

1. **零成本集成**：Doris 兼容 MySQL 协议，ADDP 现有代码几乎无需修改
2. **高性能分析**：亿级数据秒级响应，显著提升 OLAP 场景体验
3. **统一架构**：单一系统替代复杂的 Hadoop 生态（Hive + Spark）
4. **实时能力**：Stream Load 支持实时数据导入，满足实时报表需求

### 7.2 关键原则

- **保持简洁**：MVP 优先，避免过度设计
- **复用代码**：充分利用 MySQL 协议兼容性
- **性能优先**：OLAP 场景重点关注查询性能

---

**Let's start coding! 🚀**
