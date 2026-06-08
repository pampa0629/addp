# Plan: Graph 模块阶段2 — 图谱构建（LLM 驱动）详细规划

## Context

阶段1（本体建模）和阶段3（知识探索/G6可视化）均已完成，Graph 模块目前具备：
- 完整的本体 CRUD（ontologies, entity_types, relation_types, ontology_versions 表）
- 知识图谱实例管理（knowledge_graphs 表，绑定 ontology + Neo4j engine + database）
- Neo4j 连接层（GetSchema/Stats/Overview/Search/Expand/Path），含 `SyncConstraints()`
- G6 图谱浏览器（GraphBrowser.vue + GraphCanvas.vue）

阶段2目标：实现"从原始材料 → LLM 抽取 → 置信度分类 → 自动写入/审核 → Neo4j"的完整图谱构建流水线。

**关键约束（本次修订新增）**：
- 所有构建任务的执行追踪通过 Monitor 模块统一管理（`common.task_executions`）
- Neo4j 实体写入的唯一性约束由本体模型中 `PropertyDefinition.Unique` 驱动，不得硬编码
- 超长材料支持：**分块逻辑在 Graph 后端（Go）执行**，Copilot `/kg-build/extract` 只处理单个 chunk（无超时风险），并支持断点续传

---

## 一、数据库设计（PostgreSQL，仅新增3张表）

### 设计原则
- 执行状态、进度、时间等由 `common.task_executions` 统一记录（Monitor 可查询）
- `graph.build_tasks` 仅存储图谱构建特有的配置和统计（不重复 task_executions 的字段）

### 1. build_tasks — 构建任务配置

```sql
CREATE TABLE graph.build_tasks (
    id                   SERIAL PRIMARY KEY,
    tenant_id            INTEGER NOT NULL,
    graph_id             INTEGER NOT NULL REFERENCES graph.knowledge_graphs(id) ON DELETE CASCADE,
    execution_id         VARCHAR(64),      -- 关联 common.task_executions.execution_id
    name                 VARCHAR(255) NOT NULL,
    description          TEXT,
    status               VARCHAR(50)  NOT NULL DEFAULT 'pending',  -- pending/running/success/failed/cancelled
    confidence_threshold DECIMAL(3,2) NOT NULL DEFAULT 0.70,
    chunk_size           INTEGER NOT NULL DEFAULT 1000,  -- 每个 chunk 的字符数
    chunk_overlap        INTEGER NOT NULL DEFAULT 200,   -- 相邻 chunk 的重叠字符数
    stats                JSONB,   -- {total_materials,processed,auto_written,pending_review,approved,rejected}
    error_message        TEXT,
    created_at           TIMESTAMPTZ DEFAULT NOW(),
    updated_at           TIMESTAMPTZ DEFAULT NOW(),
    started_at           TIMESTAMPTZ,
    completed_at         TIMESTAMPTZ
);
```

### 2. build_materials — 构建材料

```sql
CREATE TABLE graph.build_materials (
    id               SERIAL PRIMARY KEY,
    task_id          INTEGER NOT NULL REFERENCES graph.build_tasks(id) ON DELETE CASCADE,
    tenant_id        INTEGER NOT NULL,
    graph_id         INTEGER NOT NULL,
    type             VARCHAR(50) NOT NULL,  -- document/url
    file_name        VARCHAR(255),
    file_path        TEXT,                  -- MinIO path (graph/build/ 前缀)
    file_size        BIGINT,
    -- 分块进度（支持断点续传）
    total_chunks     INTEGER DEFAULT 0,     -- 总 chunk 数（首次分块时写入）
    processed_chunks INTEGER DEFAULT 0,     -- 已处理 chunk 数
    -- 状态
    status           VARCHAR(50) NOT NULL DEFAULT 'pending',  -- pending/processing/completed/failed
    stats            JSONB,        -- {total_chunks,processed_chunks,auto_written,queued_review,entities_count,relations_count}
    error_message    TEXT,
    created_at       TIMESTAMPTZ DEFAULT NOW(),
    processed_at     TIMESTAMPTZ
);
```

> **设计说明**：不存储 `content_text`，避免超大文本写入 PG。文本内容从 MinIO 读取后，由 Go 后端分块处理，每个 chunk 独立调用 Copilot。

### 3. review_items — 待审核项

```sql
CREATE TABLE graph.review_items (
    id            SERIAL PRIMARY KEY,
    task_id       INTEGER NOT NULL REFERENCES graph.build_tasks(id) ON DELETE CASCADE,
    material_id   INTEGER REFERENCES graph.build_materials(id),
    tenant_id     INTEGER NOT NULL,
    graph_id      INTEGER NOT NULL,
    item_type     VARCHAR(50) NOT NULL,   -- entity/relation
    content       JSONB NOT NULL,         -- entity: {type,name,unique_key_field,unique_key_value,properties}
                                          -- relation: {type,source_type,source_unique_field,source_unique_value,
                                          --            target_type,target_unique_field,target_unique_value,properties}
    confidence    DECIMAL(5,4) NOT NULL,
    source_text   TEXT,                   -- 原始文本片段（审核上下文）
    status        VARCHAR(50) NOT NULL DEFAULT 'pending',  -- pending/approved/rejected/modified
    final_content JSONB,                  -- 修改后内容（status=modified 时使用）
    neo4j_id      TEXT,                   -- 写入 Neo4j 后的节点/关系 ID
    reviewed_by   INTEGER,
    reviewed_at   TIMESTAMPTZ,
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);
```

**注意**：不再创建 `build_logs` 表，执行日志通过 `common.task_executions.metadata` JSONB 存阶段摘要，材料级详情存入 `build_materials.stats`。

---

## 二、Monitor 集成（common 模块扩展）

### 2.1 新增 graph 模块常量

修改文件：`common/execution/task_execution.go`

```go
// 新增常量
ModuleGraph = "graph"

// 新增 TaskType 常量
TaskTypeKGBuild = "kg_build"
```

### 2.2 Graph 后端集成方式

参考 Develop 模块 (`develop/backend/internal/service/dev_executor.go`) 的模式：

```go
// 构建任务启动时，创建 task_execution 记录
import commonExecution "common/execution"
import commonModels "common/models"

executionID := uuid.New().String()
execution := &commonExecution.TaskExecution{
    TenantID:       tenantID,
    ExecutionID:    executionID,
    Module:         commonExecution.ModuleGraph,
    TaskType:       commonExecution.TaskTypeKGBuild,
    SourceTaskID:   commonExecution.NewSourceTaskIDFromUint(buildTask.ID),
    SourceTaskName: &buildTask.Name,
    Status:         commonExecution.ExecutionStatusPending,
    Progress:       0,
    TriggerType:    commonExecution.TriggerTypeManual,
    TriggeredBy:    &userID,
    ExecutionConfig: commonModels.JSONMap{
        "graph_id":            buildTask.GraphID,
        "confidence_threshold": buildTask.ConfidenceThreshold,
        "material_count":      len(materials),
    },
    StartedAt: &now,
}
taskExecutionRepo.Create(ctx, execution)

// 保存 execution_id 到 build_tasks，方便前端链接到 Monitor
buildTask.ExecutionID = executionID
buildRepo.UpdateTask(buildTask)

// 执行过程中更新进度
taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
    "status":   "running",
    "progress": 50,
    "metadata": JSONMap{"processed_materials": 1, "total_materials": 3},
})

// 完成时
taskExecutionRepo.UpdateFields(ctx, executionID, tenantID, map[string]interface{}{
    "status":           "success",
    "progress":         100,
    "completed_at":     time.Now(),
    "records_written":  autoWrittenCount,
    "metadata": JSONMap{
        "auto_written":   autoWrittenCount,
        "pending_review": reviewCount,
    },
})
```

### 2.3 初始化要求

在 `graph/backend/cmd/server/main.go` 中增加 `TaskExecutionRepository` 的初始化（参考 `develop/backend/cmd/server/main.go`）：

```go
taskExecutionRepo := commonExecution.NewTaskExecutionRepository(db)
buildService := service.NewBuildService(db, taskExecutionRepo, neo4jSvc, copilotURL)
```

---

## 三、Copilot 模块扩展

参考现有 `workflow_pipeline.py` 的模式，新增 KG 抽取能力。

### 新增文件结构

```
copilot/backend/
├── api/
│   └── kg_extract_api.py           # 新增
├── chains/
│   ├── entity_extraction_chain.py  # 新增
│   └── relation_extraction_chain.py # 新增
├── models/
│   └── kg_models.py                # 新增
├── pipelines/
│   └── kg_build_pipeline.py        # 新增
└── prompts/
    ├── entity_extraction.txt        # 新增
    └── relation_extraction.txt      # 新增
```

### 3.1 数据模型（`models/kg_models.py`）

```python
class PropertyInfo(BaseModel):
    name: str
    label: str
    data_type: str
    unique: bool = False    # 驱动 Neo4j MERGE 的唯一键选择
    required: bool = False
    description: str = ""

class EntityTypeInfo(BaseModel):
    name: str
    label: str
    description: str = ""
    properties: List[PropertyInfo] = []

class RelationTypeInfo(BaseModel):
    name: str
    label: str
    source_type: str
    target_type: str
    description: str = ""
    properties: List[PropertyInfo] = []

class OntologySchema(BaseModel):
    entity_types: List[EntityTypeInfo]
    relation_types: List[RelationTypeInfo]

class KGExtractRequest(BaseModel):
    text: str               # 单个 chunk 的文本（由 Graph 后端切分，不含整篇文档）
    doc_context: str = ""   # 文档头部上下文（前 N 字），帮助 LLM 理解主题和简称
    ontology: OntologySchema
    graph_id: int
    confidence_threshold: float = 0.7
    # 不再包含 chunk_size/chunk_overlap（分块已在 Graph 后端完成）

class ExtractedEntity(BaseModel):
    temp_id: str            # 局部ID
    type: str
    properties: Dict[str, Any]  # 包含 unique 属性值
    confidence: float
    source_text: str

class ExtractedRelation(BaseModel):
    type: str
    source_temp_id: str
    target_temp_id: str
    properties: Dict[str, Any] = {}
    confidence: float
    source_text: str

class KGExtractResponse(BaseModel):
    entities: List[ExtractedEntity]
    relations: List[ExtractedRelation]
    processing_time: float
```

### 3.2 Prompt 设计要点（同前，略）

### 3.3 KG 抽取 Pipeline（`kg_build_pipeline.py`，简化版）

```python
class KGBuildPipeline:
    async def run(self, request: KGExtractRequest) -> KGExtractResponse:
        # 输入已是单个 chunk，直接执行抽取
        entities = await entity_chain.run(request.text, request.ontology.entity_types)
        relations = await relation_chain.run(request.text, request.ontology.relation_types, entities)
        return KGExtractResponse(entities=entities, relations=relations, processing_time=elapsed)
```

Copilot 变为**无状态的单 chunk 抽取服务**，无超时风险，易于水平扩展。

### 3.4 API 端点（`api/kg_extract_api.py`）

```python
router = APIRouter()

@router.post("/kg-build/extract", response_model=KGExtractResponse)
async def extract_from_text(request: KGExtractRequest):
    pipeline = KGBuildPipeline()
    return await pipeline.run(request)
```

在 `main.py` 注册：
```python
from api.kg_extract_api import router as kg_extract_router
app.include_router(kg_extract_router, prefix="/api/v1/copilot", tags=["KG Build"])
```

---

## 四、Graph 后端扩展（Go）

### 4.1 新增/修改文件

| 文件 | 操作 | 说明 |
|------|------|------|
| `models/build.go` | 新增 | BuildTask, BuildMaterial, ReviewItem 模型 |
| `repository/build_repository.go` | 新增 | 3 张新表的 CRUD |
| `service/build_service.go` | 新增 | 构建业务逻辑 + Monitor 集成 + Copilot 调用 |
| `api/build_handler.go` | 新增 | HTTP handlers |
| `api/router.go` | 修改 | 注册新路由 |
| `repository/database.go` | 修改 | AutoMigrate 新增 3 个模型 |
| `cmd/server/main.go` | 修改 | 初始化 TaskExecutionRepository |

### 4.2 API 路由设计

```
// 构建任务
GET    /api/v1/graph/graphs/:id/build/tasks           # 任务列表
POST   /api/v1/graph/graphs/:id/build/tasks           # 创建任务
GET    /api/v1/graph/graphs/:id/build/tasks/:tid      # 任务详情（含材料列表）
DELETE /api/v1/graph/graphs/:id/build/tasks/:tid      # 删除任务
POST   /api/v1/graph/graphs/:id/build/tasks/:tid/run  # 触发执行（异步 goroutine）
POST   /api/v1/graph/graphs/:id/build/tasks/:tid/cancel  # 取消

// 构建材料
POST   /api/v1/graph/graphs/:id/build/tasks/:tid/materials     # 上传材料（multipart）
GET    /api/v1/graph/graphs/:id/build/tasks/:tid/materials     # 材料列表
DELETE /api/v1/graph/graphs/:id/build/tasks/:tid/materials/:mid # 删除材料

// 审核队列
GET    /api/v1/graph/graphs/:id/review                  # 审核列表（?status=pending&item_type=entity&task_id=1）
POST   /api/v1/graph/graphs/:id/review/:iid/approve     # 确认写入 Neo4j
POST   /api/v1/graph/graphs/:id/review/:iid/reject      # 拒绝
PUT    /api/v1/graph/graphs/:id/review/:iid             # 修改内容后确认（body 含 final_content）
POST   /api/v1/graph/graphs/:id/review/batch            # 批量操作（{ids:[...], action:"approve/reject"}）
```

### 4.3 本体驱动的 Neo4j 写入逻辑（核心）

唯一性约束从本体的 `PropertyDefinition.Unique` 中读取，不硬编码字段名。

```go
// build_service.go

// getUniqueKey：从本体实体类型定义中提取 unique 属性
func (s *BuildService) getUniqueKey(entityTypeName string, ontology *OntologySnapshot) (field string, value interface{}) {
    for _, et := range ontology.EntityTypes {
        if et.Name == entityTypeName {
            for _, prop := range et.ParsedProperties() {
                if prop.Unique {
                    return prop.Name, nil  // 返回 unique 字段名
                }
            }
        }
    }
    return "name", nil  // fallback（无 unique 约束时按名称）
}

// writeEntityToNeo4j：使用本体定义的 unique 字段做 MERGE
func (s *BuildService) writeEntityToNeo4j(graphID uint, entity ExtractedEntity, ontology *OntologySnapshot) (string, error) {
    uniqueField, _ := s.getUniqueKey(entity.Type, ontology)
    uniqueValue := entity.Properties[uniqueField]
    
    cypher := fmt.Sprintf(`
        MERGE (n:%s {%s: $unique_value})
        ON CREATE SET n += $props, n.created_at = timestamp()
        ON MATCH  SET n += $props, n.updated_at = timestamp()
        RETURN elementId(n)
    `, entity.Type, uniqueField)
    
    return s.neo4jSvc.RunCypher(graphID, cypher, map[string]any{
        "unique_value": uniqueValue,
        "props":        entity.Properties,
    })
}

// writeRelationToNeo4j：先查源/目标节点（也通过 unique 字段定位），再 MERGE 关系
func (s *BuildService) writeRelationToNeo4j(graphID uint, rel ExtractedRelation, ontology *OntologySnapshot) error {
    rt := s.findRelationType(rel.Type, ontology)
    srcField, _ := s.getUniqueKey(rt.SourceType, ontology)
    tgtField, _ := s.getUniqueKey(rt.TargetType, ontology)
    
    cypher := fmt.Sprintf(`
        MATCH (a:%s {%s: $src_val}), (b:%s {%s: $tgt_val})
        MERGE (a)-[r:%s]->(b)
        ON CREATE SET r += $props
        RETURN elementId(r)
    `, rt.SourceType, srcField, rt.TargetType, tgtField, rel.Type)
    
    return s.neo4jSvc.RunCypher(graphID, cypher, map[string]any{
        "src_val": rel.SourceUniqueValue,
        "tgt_val": rel.TargetUniqueValue,
        "props":   rel.Properties,
    })
}
```

**本体快照传递流程**：
1. Graph 后端 `RunTask` 时，先从 PG 读取本体（`entity_types` + `relation_types` + 属性定义）
2. 序列化为 `OntologySchema`（含 unique 标记），传给 Copilot 作为抽取上下文
3. Copilot 返回的 `content` 中包含 unique 字段的值
4. `review_items.content` 记录：`{type, unique_key_field, unique_key_value, properties, ...}`
5. 审核确认时，用同样逻辑写入 Neo4j

### 4.4 超长材料处理（分块 + 断点续传）

```go
// processMaterial：分块处理一份材料
func (s *BuildService) processMaterial(task *models.BuildTask, mat *models.BuildMaterial, ontology *OntologySnapshot) {
    // 1. 从 MinIO 读取文件内容（不存 PG，避免大对象）
    text := s.minioClient.GetText(mat.FilePath)
    
    // 2. 分块（Go 实现简单的 overlap 字符分割）
    chunks := splitText(text, task.ChunkSize, task.ChunkOverlap)
    
    // 更新材料总 chunk 数（首次计算时写入）
    if mat.TotalChunks == 0 {
        mat.TotalChunks = len(chunks)
        s.repo.UpdateMaterial(mat)
    }
    
    // 3. 断点续传：跳过已处理的 chunks
    startFrom := mat.ProcessedChunks  // 上次失败时保存的进度
    
    // 4. 逐 chunk 调用 Copilot（顺序处理，每个 chunk 完成后立即更新进度）
    for i := startFrom; i < len(chunks); i++ {
        chunk := chunks[i]
        
        result, err := s.callCopilotExtract(chunk, ontology, task.ConfidenceThreshold)
        if err != nil {
            // 保存当前进度，允许下次从这里续传
            mat.ProcessedChunks = i
            mat.Status = "failed"
            mat.ErrorMessage = err.Error()
            s.repo.UpdateMaterial(mat)
            return
        }
        
        // 5. 高置信度 → 立即写 Neo4j；低置信度 → 写 review_items
        //    实体去重：以 (type, unique_key_value) 为维度，本材料内跨 chunk 累计已见集合
        s.processChunkResult(task, mat, result, ontology)
        
        // 6. 更新 chunk 进度（持久化，支持续传）
        mat.ProcessedChunks = i + 1
        s.repo.UpdateMaterial(mat)
    }
    
    mat.Status = "completed"
    mat.ProcessedAt = time.Now()
    s.repo.UpdateMaterial(mat)
}

// processChunkResult：处理单个 chunk 的抽取结果
// seenEntities 是本材料级别的内存去重集合（type+unique_key → neo4j_id）
func (s *BuildService) processChunkResult(task, mat, result, ontology) {
    for _, entity := range result.Entities {
        uniqueField := getUniqueField(entity.Type, ontology)
        uniqueVal := entity.Properties[uniqueField]
        key := entity.Type + "::" + fmt.Sprint(uniqueVal)
        
        if entity.Confidence >= task.ConfidenceThreshold {
            if _, seen := s.seenEntities[key]; !seen {
                // 未见过 → 写 Neo4j（MERGE 本身幂等，跨材料也安全）
                neo4jID := s.writeEntityToNeo4j(task.GraphID, entity, ontology)
                s.seenEntities[key] = neo4jID
            }
            // 已见过 → 跳过（同一材料内同实体只写一次）
        } else {
            if _, seen := s.seenEntities[key]; !seen {
                // 低置信度且首次见到 → 进审核队列
                s.queueEntityForReview(task, mat, entity)
                s.seenEntities[key] = "queued"
            }
        }
    }
    // 关系类似，但需要 source/target 均已写入（或跳过未知引用的关系）
}
```

**关键设计：**
- `mat.processed_chunks` 持久化到 PG，失败重试时直接从断点继续
- `seenEntities` 仅存在材料处理的内存生命周期，用于**材料内跨 chunk 去重**（跨材料由 Neo4j MERGE 保证幂等）
- 每个 Copilot 调用处理约 1000 字符，单次耗时几秒，总不超时

---

## 四·五、文本分块策略（专项分析）

### 问题 1：在哪里截断？

**朴素字符截断的危害**：
```
原文："Alice 于 2020 年加入 Google，担任 CEO"
↓ 截断位置不当
Chunk N:  "Alice 于 2020 年加入 Go"   ← "Google" 被切断
Chunk N+1:"ogle，担任 CEO"            ← LLM 无法识别 "ogle" 是公司名
```

**解决方案：语义感知分块（Recursive Character Split）**

按优先级尝试分割符，从粗粒度到细粒度：
```
优先级1：\n\n（段落边界，最佳）
优先级2：\n（换行）
优先级3：。！？.!? （句子边界）
优先级4：，,；; （子句边界，fallback）
优先级5：空格（词边界，最后手段）
优先级6：字符边界（极端 fallback，实践中几乎不触发）
```

Go 实现（简单版）：
```go
func splitTextSentenceAware(text string, chunkSize int, overlapSize int) []string {
    separators := []string{"\n\n", "\n", "。", "！", "？", ". ", "! ", "? ", "，", ", ", " ", ""}
    return recursiveSplit(text, separators, chunkSize)
}
```

这样**始终在语义边界截断**，实体名称和句子结构完整保留。

---

### 问题 2：截断会导致信息丢失吗？

即使在语义边界截断，**跨句子/段落的关系仍可能被分割**：

```
段落A（Chunk N 末尾）：
  "阿里巴巴集团（以下简称'阿里'）于本季度发布财报..."

段落B（Chunk N+1 开头）：
  "营收同比增长 35%，其中云业务贡献最大..."
```

问题：Chunk N+1 开头的 "营收" 没有主语上下文，LLM 无法识别归属。

**解决方案：文档头部注入（Document Header Injection）**

对每个 chunk，在实际文本前面注入一段"全局上下文"：
```
[文档上下文]
文件名：2024年Q3财报.txt
来源片段（原文开头200字）：阿里巴巴集团于本季度...
---
[待分析文本]
营收同比增长 35%，其中云业务...
```

这个上下文注入：
- 告知 LLM 文档的主题和主要实体
- 帮助解析代词和简称（"阿里" = "阿里巴巴集团"）
- 不计入 chunk_size，不会触发分块逻辑

**实现**：
```go
type ChunkWithContext struct {
    Index       int
    Text        string   // 实际 chunk 文本
    DocContext  string   // 文档头部上下文（前 200 字）
}

// KGExtractRequest 扩展
class KGExtractRequest(BaseModel):
    text: str            # 单个 chunk 文本
    doc_context: str     # 文档级上下文（头部摘要）
    ontology: OntologySchema
    ...
```

---

### 问题 3：重叠（Overlap）的作用与尺寸

**Overlap 的作用**：确保跨 chunk 边界的实体/关系至少在一个完整 chunk 中出现：

```
Chunk N（1000字）：
  ...Alice 于 2020 年加入 Google [← 句子在这里完整]

  [← 200字 overlap 区 →]

Chunk N+1（1000字）：
  [← 包含上面200字 →] Alice 于 2020 年加入 Google...
```

即使 "Google" 恰好落在 N 的末尾，也会在 N+1 的开头被再次完整看到。

**推荐尺寸**：chunk_size 的 15~25%（默认 1000/200 = 20%），对应 1~2 个句子的长度。

---

### 问题 4：Overlap 导致的重复抽取如何去重？

重叠区域内同一实体/关系可能被两个 chunk 各抽取一次。

#### 实体去重策略

```
去重键：(entity_type, unique_property_value)
合并规则：
  - 取两次抽取中 confidence 较高的那条
  - properties 取并集（后来者不覆盖已有值，防止 LLM 幻觉用低质量值覆盖高质量值）
```

代码层面（在 `processChunkResult` 的 `seenEntities` map 基础上扩展）：

```go
type SeenEntity struct {
    Neo4jID    string
    Confidence float64
    Properties map[string]any
}

// 已见到该实体
if existing, seen := s.seenEntities[key]; seen {
    // 新 chunk 置信度更高 → 用新属性补充/更新 Neo4j
    if entity.Confidence > existing.Confidence && entity.Confidence >= threshold {
        s.mergeEntityProperties(existing.Neo4jID, entity.Properties)
        existing.Confidence = entity.Confidence
    }
    // 否则忽略
}
```

#### 关系去重策略

```
去重键：(source_unique_val + "::" + relation_type + "::" + target_unique_val)
合并规则：
  - 保留置信度更高的那条
  - 如果 source 或 target 尚未写入 Neo4j（关系发现时实体还未出现），
    则暂存到 "pending relations" 列表，在本材料处理完毕后再尝试写入
```

#### 跨材料去重

无需特殊处理：Neo4j 的 `MERGE` 语句本身幂等。同一实体来自不同材料时，`MERGE` 找到已有节点后执行 `ON MATCH SET`，只更新属性，不创建重复节点。

---

### 问题 5：中文简称/代词消歧

中文文档常见缩写链："阿里巴巴集团" → "阿里" → "该公司" → "它"。

跨 chunk 时 LLM 可能识别错误（"阿里"被认为是独立实体）。

**阶段2的务实处理**：
- 文档头部注入（问题2的方案）可缓解大部分情况
- 极端情况（"它"这类代词）放弃识别，宁可漏掉也不产生幻觉
- Prompt 明确指示：**如果无法确定实体指向，不要猜测，直接跳过**

---

### 分块参数的前端配置

用户在创建任务时可配置（BuildTaskDetail.vue 的高级选项折叠面板）：

| 参数 | 默认值 | 说明 |
|------|--------|------|
| `chunk_size` | 1000 | 每个 chunk 字符数（不含 overlap） |
| `chunk_overlap` | 200 | 相邻 chunk 重叠字符数 |
| `doc_context_size` | 200 | 从文档头部提取的上下文字符数 |

---

### 新增文件

| 文件 | 说明 |
|------|------|
| `src/api/graphBuild.js` | Build 相关 API 调用 |
| `src/views/BuildManager.vue` | 构建任务列表页 |
| `src/views/BuildTaskDetail.vue` | 任务详情 + 材料上传 + 状态轮询 |
| `src/views/ReviewQueue.vue` | 人工审核页面 |

### 路由新增

```javascript
// router/index.js
{ path: '/graphs/:id/build', component: BuildManager },
{ path: '/graphs/:id/build/tasks/:tid', component: BuildTaskDetail },
{ path: '/graphs/:id/review', component: ReviewQueue },
```

### 页面设计要点

**BuildManager.vue**：
- 任务卡片列表，显示 status tag（pending/running/success/failed）+ 进度条
- 统计：已写入 / 待审核 / 已通过 / 已拒绝
- 创建任务 Dialog（name, confidence_threshold 滑块 0.0-1.0）
- 点击卡片跳转 BuildTaskDetail；有 `execution_id` 时可链接跳 Monitor 页面

**BuildTaskDetail.vue**：
- 上半：任务信息 + Run/Cancel 按钮
- 中间：材料上传区（el-upload drag-drop，TXT 格式）+ 材料状态列表（pending/processing/completed/failed）
- 下半：执行状态（通过轮询 build/tasks/:tid 获取 status/stats）
- 右上角：审核队列入口按钮（显示 pending 数量 badge）

**ReviewQueue.vue**：
- Tabs：待审核实体 / 待审核关系
- 列表每行：置信度进度条 + 内容摘要 + 来源文本预览（折叠展开）+ 三按钮（确认/拒绝/修改）
- 修改 Dialog：展示字段表单（根据本体属性动态生成），保存后写 Neo4j
- 批量选择 + 批量确认/拒绝
- 顶部统计 tag：`待审核 N | 已通过 M | 已拒绝 K`

### 导航更新

在 `KnowledgeGraphList.vue` 的图谱卡片中增加"构建"和"审核"按钮（仿现有"浏览"按钮风格），审核按钮显示 pending 数量 badge。

---

## 六、关键文件路径汇总

### 新增文件

```
common/execution/task_execution.go       # 修改：新增 ModuleGraph 和 TaskTypeKGBuild 常量

copilot/backend/api/kg_extract_api.py
copilot/backend/chains/entity_extraction_chain.py
copilot/backend/chains/relation_extraction_chain.py
copilot/backend/models/kg_models.py
copilot/backend/pipelines/kg_build_pipeline.py
copilot/backend/prompts/entity_extraction.txt
copilot/backend/prompts/relation_extraction.txt

graph/backend/internal/models/build.go
graph/backend/internal/repository/build_repository.go
graph/backend/internal/service/build_service.go
graph/backend/internal/api/build_handler.go

graph/frontend/src/api/graphBuild.js
graph/frontend/src/views/BuildManager.vue
graph/frontend/src/views/BuildTaskDetail.vue
graph/frontend/src/views/ReviewQueue.vue
```

### 修改文件

```
copilot/backend/main.py                          # 注册 kg_extract_router
graph/backend/internal/repository/database.go   # AutoMigrate 新增3个模型
graph/backend/internal/api/router.go            # 注册 build + review 路由
graph/backend/cmd/server/main.go                # 初始化 TaskExecutionRepository
graph/frontend/src/router/index.js              # 新增3个路由
graph/frontend/src/views/KnowledgeGraphList.vue # 增加构建/审核入口
```

---

## 七、实施顺序

1. **common 扩展**：`task_execution.go` 新增 `ModuleGraph` 和 `TaskTypeKGBuild` 常量
2. **数据库层**：`build.go` 模型 → `database.go` AutoMigrate → 重启 graph 后端验建表
3. **Copilot 扩展**：kg_models → prompts → chains → pipeline → api 注册 → 重启 copilot 验端点
4. **Graph 后端**：build_repository → build_service（含 Monitor + Copilot 调用 + 本体驱动写入）→ build_handler → 更新 router → 重启验证
5. **前端**：graphBuild.js → BuildManager → BuildTaskDetail → ReviewQueue → 更新路由和导航

---

## 八、验证方式

1. **Monitor 可见**：执行构建任务后，在 Monitor 模块能查询到 `module=graph, task_type=kg_build` 的执行记录，状态、进度正确更新
2. **Copilot 端点**：`POST /api/v1/copilot/kg-build/extract` 传入文本和本体（含 `unique:true` 属性），返回 entities/relations，unique 字段值正确填充
3. **端到端验证**：
   - 创建本体（Person 类型，email 字段设为 unique=true）→ 创建知识图谱
   - 创建构建任务（confidence_threshold=0.7）→ 上传含人物信息的 TXT 文件 → 触发 Run
   - 高置信度实体自动写入 Neo4j：在 GraphBrowser 中可浏览，MERGE 以 email 字段为唯一键
   - 低置信度实体出现在审核队列
   - 审核队列中确认一条 → 写入 Neo4j；拒绝一条 → 状态 rejected
   - Monitor 模块中可查到本次执行记录，records_written 和 metadata 字段正确
