# SQL 编辑器模块实现方案

## 一、需求分析

基于用户需求和现有 ADDP 架构,SQL 编辑器模块需要实现:

1. **SQL 开发与编辑**: 代码高亮、自动补全、语法检查
2. **数据库执行**: 推送 SQL 到目标数据库并返回结果
3. **版本管理**: SQL 脚本保存、历史版本、上下线管理
4. **多数据库支持**: PostgreSQL、MySQL、ClickHouse 等,可扩展
5. **依赖管理**: SQL 节点间的依赖关系、执行顺序

## 二、推荐开源库

### 前端核心库

#### 1. SQL 编辑器 - Monaco Editor (强烈推荐)

**理由**:
- ✅ 与 VS Code 相同的编辑引擎,专业度最高
- ✅ 内置 SQL 语言支持,语法高亮和基础补全
- ✅ 支持多种主题(VS Dark、Light)
- ✅ 丰富的 API(格式化、查找替换、多光标)
- ✅ 支持多种 SQL 方言配置

**安装**:
```bash
npm install monaco-editor
npm install @monaco-editor/react  # 如果用 React 封装
```

**Vue 3 集成**:
```javascript
import * as monaco from 'monaco-editor'
import EditorWorker from 'monaco-editor/esm/vs/editor/editor.worker?worker'
import SqlWorker from 'monaco-editor/esm/vs/language/sql/sql.worker?worker'

// Vite 配置 worker 加载
self.MonacoEnvironment = {
  getWorker(_, label) {
    if (label === 'sql') return new SqlWorker()
    return new EditorWorker()
  }
}

// 创建编辑器
const editor = monaco.editor.create(container, {
  value: 'SELECT * FROM users WHERE id = 1;',
  language: 'sql',
  theme: 'vs-dark',
  automaticLayout: true,
  minimap: { enabled: false },
  fontSize: 14
})
```

**替代方案**: CodeMirror 6 (更轻量,约 600KB)
```bash
npm install codemirror @codemirror/lang-sql @codemirror/theme-one-dark
```

---

#### 2. SQL 自动补全增强 - sql-autocomplete

**理由**:
- ✅ 基于语法树的智能补全
- ✅ 支持表名、列名、关键字补全
- ✅ 可集成元数据(从 Meta 模块获取)
- ✅ 支持多种 SQL 方言

**安装**:
```bash
npm install sql-autocomplete
```

**集成示例**:
```javascript
import { AutocompleteProvider } from 'sql-autocomplete'

const provider = new AutocompleteProvider({
  // 从 Meta 模块获取的元数据
  tables: ['users', 'orders', 'products'],
  columns: {
    users: ['id', 'name', 'email'],
    orders: ['id', 'user_id', 'total']
  }
})

// 获取当前位置的补全建议
const suggestions = provider.getCompletions(sqlText, cursorPosition)
```

---

#### 3. SQL 格式化 - sql-formatter

**理由**:
- ✅ 支持多种 SQL 方言(PostgreSQL、MySQL、SQL Server 等)
- ✅ 可配置缩进、大小写风格
- ✅ 轻量级,无依赖

**安装**:
```bash
npm install sql-formatter
```

**使用**:
```javascript
import { format } from 'sql-formatter'

const formattedSQL = format('SELECT * FROM users WHERE id=1', {
  language: 'postgresql',
  indent: '  ',
  uppercase: true,
  linesBetweenQueries: 2
})
```

---

#### 4. SQL 解析与验证 - node-sql-parser

**理由**:
- ✅ 将 SQL 解析为 AST(抽象语法树)
- ✅ 支持语法验证
- ✅ 可提取表名、列名(用于依赖分析)
- ✅ 支持 PostgreSQL、MySQL、BigQuery 等多种方言

**安装**:
```bash
npm install node-sql-parser
```

**使用示例**:
```javascript
import { Parser } from 'node-sql-parser'

const parser = new Parser()

// 解析 SQL
try {
  const ast = parser.astify('SELECT * FROM users WHERE id = 1', { database: 'PostgreSQL' })
  console.log('语法正确', ast)

  // 提取表依赖
  const tables = parser.tableList('SELECT u.name FROM users u JOIN orders o ON u.id = o.user_id')
  // => ['users', 'orders']
} catch (error) {
  console.error('语法错误:', error.message)
}
```

---

#### 5. 结果表格显示 - Element Plus Table

**理由**:
- ✅ 项目已集成 Element Plus
- ✅ 支持虚拟滚动(大数据集)
- ✅ 列排序、筛选、导出

**使用**:
```vue
<el-table :data="queryResults" v-loading="loading" max-height="600">
  <el-table-column
    v-for="col in columns"
    :key="col.name"
    :prop="col.name"
    :label="col.name"
    :width="col.width"
  />
</el-table>
```

**增强方案**: ag-Grid (企业级,支持百万行数据)
```bash
npm install ag-grid-vue3 ag-grid-community
```

---

### 后端核心库

#### 6. 数据库驱动 - 使用 GORM + 原生 SQL

**理由**:
- ✅ 项目已使用 GORM
- ✅ 支持 Raw SQL 执行
- ✅ 多数据库连接池管理
- ✅ 支持 PostgreSQL、MySQL、SQLite、SQL Server 等

**执行 SQL 示例**:
```go
import (
    "gorm.io/driver/postgres"
    "gorm.io/gorm"
)

// 执行查询
func ExecuteQuery(db *gorm.DB, sqlText string) ([]map[string]interface{}, error) {
    rows, err := db.Raw(sqlText).Rows()
    if err != nil {
        return nil, err
    }
    defer rows.Close()

    // 获取列名
    columns, _ := rows.Columns()

    // 扫描结果
    var results []map[string]interface{}
    for rows.Next() {
        values := make([]interface{}, len(columns))
        valuePtrs := make([]interface{}, len(columns))
        for i := range values {
            valuePtrs[i] = &values[i]
        }

        rows.Scan(valuePtrs...)

        row := make(map[string]interface{})
        for i, col := range columns {
            row[col] = values[i]
        }
        results = append(results, row)
    }

    return results, nil
}

// 执行 DML(INSERT/UPDATE/DELETE)
func ExecuteDML(db *gorm.DB, sqlText string) (int64, error) {
    result := db.Exec(sqlText)
    return result.RowsAffected, result.Error
}
```

---

#### 7. SQL 解析与分析 - go-sql-parser (可选)

**理由**:
- ✅ Go 原生实现
- ✅ 提取 SQL 依赖关系
- ✅ 验证语法

**安装**:
```bash
go get github.com/xwb1989/sqlparser
```

**使用**:
```go
import "github.com/xwb1989/sqlparser"

// 解析 SQL
stmt, err := sqlparser.Parse("SELECT * FROM users WHERE id = 1")
if err != nil {
    return fmt.Errorf("SQL 语法错误: %v", err)
}

// 提取表名
var tables []string
_ = sqlparser.Walk(func(node sqlparser.SQLNode) (bool, error) {
    if tableName, ok := node.(sqlparser.TableName); ok {
        tables = append(tables, tableName.Name.String())
    }
    return true, nil
}, stmt)
```

---

#### 8. 任务调度 - Asynq (项目已使用)

**理由**:
- ✅ Transfer 模块已使用 Asynq
- ✅ 支持延迟任务、重试、优先级
- ✅ 基于 Redis,与现有基础设施一致

**SQL 任务执行**:
```go
import "github.com/hibiken/asynq"

// 定义 SQL 执行任务
type SQLExecutionPayload struct {
    QueryID  uint   `json:"query_id"`
    SQL      string `json:"sql"`
    ResourceID uint `json:"resource_id"`
}

// 任务处理器
func HandleSQLExecution(ctx context.Context, task *asynq.Task) error {
    var payload SQLExecutionPayload
    json.Unmarshal(task.Payload(), &payload)

    // 执行 SQL
    results, err := executionService.Execute(payload.ResourceID, payload.SQL)

    // 保存结果到数据库
    return queryRepository.SaveResults(payload.QueryID, results)
}
```

---

#### 9. DAG 依赖管理 - 使用 Orchestrator 模块现有能力

**理由**:
- ✅ Orchestrator 前端已实现 DAG 编辑器(@antv/g6)
- ✅ 支持节点依赖、环检测
- ✅ 可扩展为 SQL 节点类型

**集成方案**:
1. 在 Orchestrator 中添加 `SQLNode` 类型
2. SQL 编辑器作为节点配置界面
3. 执行时按拓扑排序顺序执行 SQL

---

## 三、架构设计

### 模块位置与命名

**推荐**: 新建 `/develop` 模块(独立服务)

**命名理由**:
- ✅ 体现"数据开发"的核心定位
- ✅ 与现有模块命名风格一致(system/manager/meta/transfer)
- ✅ 涵盖更广的功能范围(不仅是查询,还有开发、测试、部署)
- ✅ 与 `labs/develop` 实验室目录呼应(从实验到正式模块)

**其他备选名称**:
- `/studio` - 数据开发工作室
- `/workbench` - 数据开发工作台
- `/ide` - 集成开发环境

**最终选择**: **`/develop`** (简洁、准确、符合中台定位)

**目录结构**:
```
/develop/
├── backend/
│   ├── cmd/server/main.go
│   ├── internal/
│   │   ├── api/
│   │   │   ├── sql_handler.go          # SQL 开发 API
│   │   │   ├── script_handler.go       # 脚本管理 API
│   │   │   ├── execution_handler.go    # 执行历史 API
│   │   │   └── router.go
│   │   ├── service/
│   │   │   ├── sql_service.go          # SQL 执行服务
│   │   │   ├── script_service.go       # 脚本版本管理(上下线)
│   │   │   ├── parser_service.go       # SQL 解析(依赖提取)
│   │   │   └── connection_service.go   # 多数据库连接池
│   │   ├── repository/
│   │   │   ├── script_repository.go    # 脚本存储
│   │   │   ├── execution_repository.go # 执行历史
│   │   │   └── database.go
│   │   ├── models/
│   │   │   ├── script.go               # SQL 脚本模型
│   │   │   ├── execution.go            # 执行记录
│   │   │   └── dependency.go           # 依赖关系
│   │   └── middleware/
│   │       └── auth.go
│   └── go.mod
│
└── frontend/
    ├── src/
    │   ├── views/
    │   │   ├── SQLEditor.vue           # SQL 编辑器主页(数据开发主界面)
    │   │   ├── ScriptList.vue          # 脚本列表(版本管理)
    │   │   ├── ExecutionHistory.vue    # 执行历史
    │   │   └── DependencyGraph.vue     # 依赖关系图(节点编排)
    │   ├── components/
    │   │   ├── MonacoEditor.vue        # Monaco 编辑器封装
    │   │   ├── SQLResult.vue           # SQL 执行结果表格
    │   │   ├── ScriptVersions.vue      # 版本列表(上下线管理)
    │   │   └── SQLFormatter.vue        # 格式化工具栏
    │   ├── api/
    │   │   ├── sql.js                  # SQL 执行 API
    │   │   └── script.js               # 脚本管理 API
    │   ├── stores/
    │   │   └── developStore.js         # Pinia 状态管理
    │   └── utils/
    │       ├── sqlParser.js            # 前端 SQL 解析
    │       └── autocomplete.js         # 自动补全提供者
    ├── package.json
    └── vite.config.js
```

---

### 数据库设计

```sql
-- 创建 develop schema (数据开发模块)
CREATE SCHEMA IF NOT EXISTS develop;

-- SQL 脚本表
CREATE TABLE develop.scripts (
    id SERIAL PRIMARY KEY,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    sql_content TEXT NOT NULL,
    resource_id INTEGER NOT NULL REFERENCES system.resources(id), -- 目标数据库
    version INTEGER DEFAULT 1,
    status VARCHAR(20) DEFAULT 'draft',  -- draft, published, archived
    created_by INTEGER NOT NULL REFERENCES system.users(id),
    tenant_id INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP
);

-- 脚本版本历史表
CREATE TABLE develop.script_versions (
    id SERIAL PRIMARY KEY,
    script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    version INTEGER NOT NULL,
    sql_content TEXT NOT NULL,
    change_description TEXT,
    created_by INTEGER NOT NULL,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(script_id, version)
);

-- 执行记录表
CREATE TABLE develop.executions (
    id SERIAL PRIMARY KEY,
    script_id INTEGER REFERENCES develop.scripts(id),
    resource_id INTEGER NOT NULL,
    sql_content TEXT NOT NULL,
    status VARCHAR(20) NOT NULL,  -- running, success, failed
    rows_affected INTEGER,
    execution_time_ms INTEGER,
    error_message TEXT,
    executed_by INTEGER NOT NULL,
    tenant_id INTEGER NOT NULL,
    started_at TIMESTAMP DEFAULT NOW(),
    completed_at TIMESTAMP
);

-- 脚本依赖关系表
CREATE TABLE develop.script_dependencies (
    id SERIAL PRIMARY KEY,
    script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    depends_on_script_id INTEGER NOT NULL REFERENCES develop.scripts(id) ON DELETE CASCADE,
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(script_id, depends_on_script_id)
);

-- 索引
CREATE INDEX idx_scripts_tenant ON develop.scripts(tenant_id);
CREATE INDEX idx_scripts_status ON develop.scripts(status);
CREATE INDEX idx_executions_script ON develop.executions(script_id);
CREATE INDEX idx_executions_tenant ON develop.executions(tenant_id);
CREATE INDEX idx_executions_started ON develop.executions(started_at DESC);
```

---

### API 设计

#### SQL 开发执行 API

```
POST /api/develop/execute
Content-Type: application/json
Authorization: Bearer <token>

Request:
{
  "resource_id": 123,
  "sql": "SELECT * FROM users LIMIT 10",
  "timeout": 30000  // 毫秒
}

Response:
{
  "columns": ["id", "name", "email"],
  "rows": [
    {"id": 1, "name": "Alice", "email": "alice@example.com"},
    {"id": 2, "name": "Bob", "email": "bob@example.com"}
  ],
  "execution_time_ms": 45,
  "rows_count": 2
}
```

#### 脚本管理 API

```
# 创建脚本
POST /api/develop/scripts
{
  "name": "用户统计查询",
  "description": "按月统计活跃用户数",
  "sql_content": "SELECT DATE_TRUNC('month', created_at) as month, COUNT(*) FROM users GROUP BY month",
  "resource_id": 123
}

# 列表脚本
GET /api/develop/scripts?status=published&page=1&limit=20

# 获取脚本详情
GET /api/develop/scripts/:id

# 更新脚本(创建新版本)
PUT /api/develop/scripts/:id
{
  "sql_content": "...",
  "change_description": "添加 WHERE 条件"
}

# 发布脚本(上线)
POST /api/develop/scripts/:id/publish

# 归档脚本(下线)
POST /api/develop/scripts/:id/archive

# 获取版本历史
GET /api/develop/scripts/:id/versions

# 回滚到指定版本
POST /api/develop/scripts/:id/rollback
{
  "version": 3
}
```

#### 依赖管理 API

```
# 设置脚本依赖
POST /api/develop/scripts/:id/dependencies
{
  "depends_on": [456, 789]  // 脚本 ID 列表
}

# 获取依赖图
GET /api/develop/scripts/:id/dependency-graph

Response:
{
  "nodes": [
    {"id": 123, "name": "用户统计", "status": "published"},
    {"id": 456, "name": "订单汇总", "status": "published"}
  ],
  "edges": [
    {"from": 123, "to": 456}
  ]
}

# 批量执行(按依赖顺序)
POST /api/develop/scripts/batch-execute
{
  "script_ids": [123, 456, 789]
}
```

---

## 四、实施步骤

### Phase 1: 基础 SQL 编辑器 (1-2 周)

**目标**: 实现基本的 SQL 编辑和执行

1. **前端**:
   - [ ] 创建 `/develop/frontend` 目录,配置 Vite + Vue 3
   - [ ] 集成 Monaco Editor,实现 SQL 语法高亮
   - [ ] 实现 `SQLEditor.vue` 主视图
   - [ ] 集成 `sql-formatter` 实现格式化功能
   - [ ] 实现 `SQLResult.vue` 结果表格组件

2. **后端**:
   - [ ] 创建 `/develop/backend` 目录,初始化 Go 模块
   - [ ] 实现 `SQLService` - 多数据源连接和 SQL 执行
   - [ ] 实现 `SQLHandler` - HTTP API 接口
   - [ ] 集成 System 模块获取数据源配置
   - [ ] 添加认证中间件

3. **集成**:
   - [ ] 添加到 docker-compose.yml(端口 8084)
   - [ ] Gateway 配置路由规则(/api/develop/*)
   - [ ] Portal 添加数据开发入口

**关键文件**:
- `develop/frontend/src/views/SQLEditor.vue`
- `develop/frontend/src/components/MonacoEditor.vue`
- `develop/backend/internal/service/sql_service.go`
- `develop/backend/internal/api/sql_handler.go`

---

### Phase 2: 脚本管理与版本控制 (1-2 周)

**目标**: 实现 SQL 脚本的保存、版本管理、上下线功能

1. **数据库**:
   - [ ] 创建 `develop.scripts`、`develop.script_versions` 表
   - [ ] 添加到 `scripts/init-db.sql` 脚本

2. **后端**:
   - [ ] 实现 `ScriptService` - 脚本 CRUD 和版本管理
   - [ ] 实现 `ScriptRepository` - 数据访问层
   - [ ] 实现发布/归档/回滚接口

3. **前端**:
   - [ ] 实现 `ScriptList.vue` - 脚本列表页
   - [ ] 实现 `ScriptVersions.vue` - 版本历史组件
   - [ ] 实现保存脚本对话框
   - [ ] 实现状态管理(draft/published/archived)

**关键文件**:
- `develop/backend/internal/models/script.go`
- `develop/backend/internal/service/script_service.go`
- `develop/frontend/src/views/ScriptList.vue`

---

### Phase 3: 多数据库支持扩展 (1 周)

**目标**: 支持 PostgreSQL、MySQL、ClickHouse 等多种数据库

1. **后端**:
   - [ ] 抽象 `DatabaseDriver` 接口
   - [ ] 实现 `PostgreSQLDriver`、`MySQLDriver`、`ClickHouseDriver`
   - [ ] 连接池管理和超时控制
   - [ ] 数据库特定的元数据获取(表名、列名)

2. **前端**:
   - [ ] 根据数据源类型切换 SQL 方言
   - [ ] 针对不同数据库的语法提示

**关键文件**:
- `develop/backend/internal/service/driver/interface.go`
- `develop/backend/internal/service/driver/postgresql.go`
- `develop/backend/internal/service/driver/mysql.go`

---

### Phase 4: SQL 依赖管理 (1-2 周)

**目标**: 实现 SQL 节点间的依赖关系、执行顺序管理

1. **数据库**:
   - [ ] 创建 `develop.script_dependencies` 表

2. **后端**:
   - [ ] 实现 `ParserService` - SQL 解析,提取表依赖
   - [ ] 实现 `DependencyService` - 依赖关系管理
   - [ ] 实现拓扑排序算法(DAG 执行顺序)
   - [ ] 实现批量执行接口(按依赖顺序)
   - [ ] 环检测(防止循环依赖)

3. **前端**:
   - [ ] 实现 `DependencyGraph.vue` - 使用 @antv/g6 显示依赖图
   - [ ] 实现依赖关系编辑界面
   - [ ] 集成 Orchestrator 的 DAG 编辑器(可选)

**关键文件**:
- `develop/backend/internal/service/parser_service.go`
- `develop/backend/internal/service/dependency_service.go`
- `develop/frontend/src/views/DependencyGraph.vue`

---

### Phase 5: 高级功能 (可选,1-2 周)

1. **SQL 自动补全增强**:
   - [ ] 集成 `sql-autocomplete`
   - [ ] 从 Meta 模块获取元数据(表名、列名)
   - [ ] 实现智能补全提供者

2. **执行历史与监控**:
   - [ ] 实现 `ExecutionHistory.vue` - 历史记录查询
   - [ ] 执行统计(成功率、平均耗时)
   - [ ] 慢查询告警

3. **权限控制**:
   - [ ] 脚本级别的访问控制(owner/viewer/editor)
   - [ ] 数据源级别的执行权限

4. **导出功能**:
   - [ ] 查询结果导出为 CSV/Excel
   - [ ] 脚本导出为 SQL 文件

---

## 五、推荐技术栈总结

| 层次 | 组件 | 推荐库 | 理由 |
|------|------|--------|------|
| **前端编辑器** | SQL 编辑器 | **Monaco Editor** | 专业、功能强大 |
| **前端工具** | 格式化 | **sql-formatter** | 轻量、多方言 |
| **前端工具** | 解析验证 | **node-sql-parser** | 提取依赖、语法验证 |
| **前端工具** | 自动补全 | **sql-autocomplete** | 智能补全 |
| **前端图表** | 依赖图 | **@antv/g6** (已有) | 项目已使用 |
| **后端执行** | SQL 执行 | **GORM + Raw SQL** | 项目已使用 |
| **后端解析** | SQL 分析 | **go-sql-parser** (可选) | 依赖提取 |
| **后端任务** | 异步执行 | **Asynq** (已有) | 项目已使用 |

---

## 六、核心优势

1. ✅ **与现有架构完全契合** - 遵循微服务模式、分层架构
2. ✅ **复用现有能力** - System 资源管理、Meta 元数据、Orchestrator DAG
3. ✅ **技术栈一致** - Go + GORM + Vue 3 + Element Plus
4. ✅ **可扩展设计** - 支持多数据库、插件式驱动
5. ✅ **企业级功能** - 版本管理、权限控制、审计日志

---

## 七、预估工作量

- **Phase 1** (基础编辑器): 1-2 周
- **Phase 2** (脚本管理): 1-2 周
- **Phase 3** (多数据库): 1 周
- **Phase 4** (依赖管理): 1-2 周
- **Phase 5** (高级功能): 1-2 周

**总计**: 5-9 周(取决于团队规模和优先级)

---

## 八、下一步建议

1. **验证技术选型** - 创建 Monaco Editor 和 sql-formatter 的 POC
2. **确认需求优先级** - 与产品讨论 Phase 1-5 的优先级
3. **设计数据库 Schema** - Review `develop` schema 设计
4. **准备开发环境** - 创建 `/develop` 目录,配置 Go 和 Vue 项目
