# Meta 元数据模块

## 概述

Meta 是全域数据平台的元数据服务，负责数据的元数据解析、存储、查询和血缘追踪。支持可扩展的数据类型插件和基于元数据的数据检索。

## 核心功能

- **元数据解析**: 自动解析各类数据源的元数据（表结构、字段类型、统计信息等）
- **元数据存储**: 统一存储和管理元数据信息
- **元数据查询**: 提供灵活的元数据查询接口
- **数据血缘**: 追踪数据在处理流程中的来源和去向
- **类型扩展**: 支持插件化扩展新的数据类型解析器
- **元数据检索**: 基于元数据的全文搜索和过滤

## 技术栈

- **语言**: Go 1.21+
- **框架**: Gin
- **ORM**: GORM
- **数据库**: PostgreSQL (元数据存储) / Elasticsearch (全文检索)
- **图数据库**: Neo4j / ArangoDB (血缘关系，可选)
- **前端**: Vue 3 + Element Plus + G6 (血缘图可视化)

## 项目结构

```
meta/
├── backend/
│   ├── cmd/
│   │   └── server/
│   │       └── main.go      # 应用入口
│   ├── internal/
│   │   ├── api/             # HTTP 处理层
│   │   ├── service/         # 业务逻辑层
│   │   ├── repository/      # 数据访问层
│   │   ├── models/          # 数据模型
│   │   ├── parser/          # 元数据解析器
│   │   │   ├── base.go      # 解析器接口
│   │   │   ├── csv.go       # CSV 解析器
│   │   │   ├── json.go      # JSON 解析器
│   │   │   ├── parquet.go   # Parquet 解析器
│   │   │   └── database.go  # 数据库解析器
│   │   ├── lineage/         # 血缘追踪
│   │   └── search/          # 元数据检索
│   ├── pkg/
│   │   └── plugin/          # 插件加载机制
│   ├── plugins/             # 扩展插件目录
│   ├── Dockerfile
│   └── go.mod
├── frontend/
│   ├── src/
│   │   ├── views/
│   │   │   ├── Metadata.vue       # 元数据浏览
│   │   │   ├── Lineage.vue        # 血缘图谱
│   │   │   └── Search.vue         # 元数据搜索
│   │   └── api/
│   ├── package.json
│   └── Dockerfile
└── README.md
```

## 数据模型设计

### 数据集元数据 (Dataset)
```go
type Dataset struct {
    ID          uint
    Name        string
    Type        string      // table, file, api
    SourceID    uint        // 关联数据源
    Path        string      // 数据路径
    Description string
    Schema      JSON        // 字段结构
    Statistics  JSON        // 统计信息
    Tags        []string    // 标签
    CreatedAt   time.Time
    UpdatedAt   time.Time
}
```

### 字段元数据 (Field)
```go
type Field struct {
    ID          uint
    DatasetID   uint
    Name        string
    Type        string      // string, int, float, timestamp, etc.
    Nullable    bool
    Description string
    Statistics  JSON        // min, max, distinct_count, etc.
    Position    int         // 字段顺序
}
```

### 血缘关系 (Lineage)
```go
type Lineage struct {
    ID          uint
    SourceID    uint        // 源数据集 ID
    TargetID    uint        // 目标数据集 ID
    Type        string      // transform, copy, aggregate, join
    Transform   JSON        // 转换逻辑描述
    CreatedAt   time.Time
}
```

## API 端点

### 元数据管理
- `POST /api/metadata/parse` - 解析数据源元数据
- `GET /api/metadata/datasets` - 获取数据集列表
- `GET /api/metadata/datasets/:id` - 获取数据集元数据
- `PUT /api/metadata/datasets/:id` - 更新元数据
- `DELETE /api/metadata/datasets/:id` - 删除元数据

### 元数据检索
- `GET /api/metadata/search?q=keyword` - 全文搜索
- `POST /api/metadata/search/advanced` - 高级搜索（多条件）
- `GET /api/metadata/tags` - 获取标签列表
- `GET /api/metadata/datasets/by-tag/:tag` - 按标签查询

### 血缘追踪
- `POST /api/lineage` - 创建血缘关系
- `GET /api/lineage/upstream/:id` - 获取上游血缘
- `GET /api/lineage/downstream/:id` - 获取下游血缘
- `GET /api/lineage/graph/:id` - 获取完整血缘图
- `GET /api/lineage/impact/:id` - 影响分析

### 插件管理
- `GET /api/plugins` - 获取已安装插件
- `POST /api/plugins/install` - 安装插件
- `DELETE /api/plugins/:name` - 卸载插件

## 元数据解析器接口

```go
type MetadataParser interface {
    // 支持的数据类型
    SupportedTypes() []string

    // 解析元数据
    Parse(ctx context.Context, source DataSource) (*Dataset, error)

    // 解析字段信息
    ParseFields(ctx context.Context, source DataSource) ([]*Field, error)

    // 获取统计信息
    GetStatistics(ctx context.Context, source DataSource) (map[string]interface{}, error)
}
```

## 支持的数据类型

### 结构化数据
- CSV / TSV
- JSON / JSONL
- Parquet
- Avro
- ORC

### 数据库
- MySQL / PostgreSQL (表结构)
- MongoDB (Collection Schema)
- ClickHouse
- Elasticsearch

### 半结构化数据
- XML
- YAML
- Protobuf

## 血缘追踪场景

### 数据流转
- 数据导入：Manager → Meta 记录来源
- 数据转换：Transfer → Meta 记录转换关系
- 数据导出：Transfer → Meta 记录目标

### 影响分析
- 当上游数据变更时，分析影响哪些下游数据
- 字段级别血缘追踪
- 转换逻辑记录

## 开发计划

### 阶段 1: 基础元数据
- [ ] 元数据数据模型设计
- [ ] CSV/JSON 解析器实现
- [ ] 基础元数据 CRUD API
- [ ] 元数据浏览前端

### 阶段 2: 元数据检索
- [ ] PostgreSQL 全文搜索
- [ ] 标签系统
- [ ] 高级过滤查询
- [ ] 搜索前端界面

### 阶段 3: 数据库元数据
- [ ] MySQL 表结构解析
- [ ] PostgreSQL 表结构解析
- [ ] 数据库统计信息采集
- [ ] 增量元数据更新

### 阶段 4: 血缘追踪
- [ ] 血缘关系数据模型
- [ ] 血缘 API 实现
- [ ] 血缘图可视化（G6）
- [ ] 影响分析功能

### 阶段 5: 高级特性
- [ ] 插件系统框架
- [ ] Parquet/Avro 解析器
- [ ] Elasticsearch 集成（可选）
- [ ] 字段级血缘追踪

## 配置说明

### 环境变量

```bash
# 服务端口
META_PORT=8082

# 数据库配置
DB_HOST=postgres
DB_PORT=5432
DB_NAME=metadata
DB_USER=meta
DB_PASSWORD=password

# Elasticsearch 配置（可选）
ELASTICSEARCH_URL=http://elasticsearch:9200
ELASTICSEARCH_INDEX=metadata

# 血缘图数据库（可选）
GRAPH_DB_TYPE=neo4j  # neo4j, arangodb, postgres
NEO4J_URL=bolt://neo4j:7687
NEO4J_USER=neo4j
NEO4J_PASSWORD=password

# 解析器配置
PARSER_SAMPLE_SIZE=10000     # 统计信息采样行数
PARSER_TIMEOUT=300s          # 解析超时时间
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

## 待补充内容

- 各解析器的详细实现规范
- 统计信息的具体字段定义
- 血缘关系的详细分类
- 插件开发规范和示例
- 与 Manager 模块的集成流程
- 与 Transfer 模块的血缘记录方式
- 元数据版本管理机制