# ADDP Graph 统一抽象设计

> 状态：讨论后推进  
> 创建日期：2026-05-25

## 背景

ADDP 已将 table 型事实收敛到 `datatype.TableInfo`，并明确 table item 的主事实进入 `plugin.ItemMetadata.Table`。graph 重构前存在过概念混用：

- Neo4j catalog 曾以 `label` / `relationship` 作为叶子 item。
- Meta 扫描曾把 `label` / `relationship` 写成独立 `meta_item`。
- `type_info.graph` 曾使用 `node_count`、`edge_count`、`from_labels`、`to_labels` 等扁平事实。
- Manager 曾按 label / relationship 展示图结构，这只是 UI 投影视角，不应反向定义 `common/datatype`。

graph 的统一抽象需要先回到本体：图的核心是 node 和 relationship；label 只是节点分类或展示分组方式，不能作为 graph data type 的一等本体。

## 核心结论

1. `graph` 的 data item 应表示一个可被查询、采样、预览和治理的图整体。
2. `label`、`relationship type`、属性结构和连接模式属于 graph schema / shape facts，不应作为 graph data type 的核心 item 本体。
3. Neo4j 的 `label` / `relationship type` 可以作为 Graph 模块和 Manager 展示中的筛选、分组或投影视角，但不能作为 Meta 主数据项。
4. `common/datatype.GraphInfo` 描述图结构摘要，不承载实际节点、边样本或前端图组件 DTO。
5. 图查询语言、路径探索、图算法和可视化交互属于 graph 模块或 engine provider 能力，不进入 `common/datatype`。

## Meta Item 建议

目标形态：

```text
meta_node: database = neo4j
  meta_item: item_type = graph
             attributes.item.data_type = graph
             attributes.type_info.graph = GraphInfo
```

不建议继续把每个 label / relationship type 作为独立 `meta_item`：

- 一个 Neo4j node 可以有多个 label，按 label 落 item 会造成归属和计数重复。
- relationship type 依赖 endpoint node shape，不是独立图数据集。
- label / relationship type 更适合作为 `GraphInfo` 中的 schema shape。

当前不保留旧 label / relationship item 兼容层；历史数据通过重新扫描生成 graph item 和 `type_info.graph`。

## Manager 预览建议

Manager 负责通用资源浏览和轻量预览，不沉淀图领域模型。graph 预览建议分为三个视角：

1. Schema 视角：展示 node shapes、relationship shapes 和 relationship patterns。
2. Sample 视角：展示小规模真实子图样本，节点和关系可点选查看属性。
3. Properties 视角：展示 node / relationship 的属性结构，明确这是 graph property，不是 table column。

Neo4j label 可以投影成 node shape：

```text
Person label -> NodeShape(kind=label, labels=["Person"])
```

Neo4j relationship type 可以投影成 relationship shape 和 pattern：

```text
(:Person)-[:WORKS_FOR]->(:Company)
```

不得仅以顶层 `from_labels[]` / `to_labels[]` 表示 relationship endpoint，因为它会丢失配对关系，也无法准确表达多 label 节点。

## Graph 模块调整建议

Graph 模块应承接真正的图领域能力：

- graph schema 浏览：node shapes、relationship shapes、patterns。
- graph query：Cypher / GQL / SPARQL 等图查询语言执行。
- graph sample：默认样本、按 node shape 采样、按 relationship pattern 采样。
- 图可视化交互：展开邻居、路径探索、属性面板。
- 图算法入口：最短路径、中心性、社区发现等后续能力。

Manager 可复用 graph 模块或 engine graph provider 的能力，但不应自行维护独立图领域模型。

Graph 模块的浏览、知识服务、推导和图算法必须使用同一套业务图视角：

- Neo4j 插件或扩展生成的内部关系不进入业务图 schema、统计、预览、路径和算法投影。
- 第一批过滤的 Neo4j spatial 内部关系为 `RTREE_METADATA`、`RTREE_REFERENCE`、`RTREE_ROOT`。
- 过滤规则归属于 Graph 模块服务层；`common/datatype.GraphInfo` 不携带具体引擎内部规则。

### 本体类型与引擎执行映射

Graph 模块里的本体 `EntityType` 是概念层定义，`EntityType.Name` 是本体概念标识，不应继续被硬性等同为 Neo4j label。Neo4j label 或 label set 是该概念在具体图引擎中的执行映射。

因此 Graph 模块本体层增加实体类型执行映射：

```go
type EntityType struct {
    Name       string
    Label      string
    NodeLabels []string
}
```

规则：

1. `Name`：本体概念标识，用于本体关系、模型导入、LLM schema、前端展示和 API 语义。
2. `NodeLabels`：Neo4j 执行映射，允许单 label 或 label set，例如 `["Person"]`、`["Employee","Person"]`。
3. 未显式设置 `NodeLabels` 时，Graph 模块可按实体类型继承链生成默认 Neo4j labels，例如 `City -> POI` 生成 `["City","POI"]`。
4. 从已有 Neo4j 数据推导本体时，推导结果写入 `EntityType.NodeLabels`，`Name` 可使用 node shape 名作为初始概念名，但后续用户可重命名概念而不改变执行映射。
5. `RelationType.SourceTypeID` / `TargetTypeID` 继续指向本体 `EntityType`；运行时查询、构建、空间图层、算法过滤需要 Neo4j label 时必须通过 `NodeLabels` 或默认映射解析，不直接把 `EntityType.Name` 当作唯一 label。
6. Neo4j 约束同步受 Cypher DDL 限制，只能作用于单个 label。当前策略是使用 `NodeLabels[0]`，未显式配置时使用默认映射中的第一个 label。复合 label set 的唯一性约束后续如需更严格，需要在 Graph 模块单独定义约束策略。

### 空间图层与节点映射

空间图层属于 Graph 模块的本体配置和 Neo4j spatial 运行时能力，不进入 `common/datatype.GraphInfo`。它需要同时区分三类概念：

1. `EntityType.Name`：本体实体类型概念标识。
2. `EntityType.NodeLabels`：该实体类型落到 Neo4j 节点时使用的 label set。
3. `SpatialLayerConfig.LayerName`：Neo4j spatial layer 标识，用于 `spatial.addPointLayerXY`、`spatial.addWKTLayer`、`spatial.addNode`、`spatial.withinDistance`、`spatial.intersects` 等过程调用。

规则：

1. 空间图层选择、同步和算法执行传递的是 `LayerName`，不能把 `LayerName` 当作 Neo4j label。
2. 节点注册到空间图层时，节点匹配必须使用 `NodeLabels` 或继承链默认映射。
3. 继承空间配置时，子类型可以复用父类型的几何字段配置，但默认生成独立的 layer name，避免多个实体类型写入同一个 spatial layer 后失去本体边界。
4. Manager 只消费 graph metadata 的通用结构摘要；空间图层能力、Neo4j spatial 内部关系过滤和运行时注册逻辑留在 Graph 模块。

## Common Datatype 目标结构

第一版 `GraphInfo` 只表达结构摘要：

```go
type GraphInfo struct {
    Model              string
    Directed           *bool
    NodeCount          *int64
    RelationshipCount  *int64
    NodeShapes         []GraphNodeShapeInfo
    RelationshipShapes []GraphRelationshipShapeInfo
}

type GraphNodeShapeInfo struct {
    Name       string
    Kind       string
    Labels     []string
    Properties []FieldInfo
    Count      *int64
}

type GraphRelationshipShapeInfo struct {
    Type       string
    Properties []FieldInfo
    Patterns   []GraphRelationshipPatternInfo
    Count      *int64
}

type GraphRelationshipPatternInfo struct {
    From  GraphEndpointInfo
    To    GraphEndpointInfo
    Count *int64
}

type GraphEndpointInfo struct {
    ShapeName string
    Labels    []string
}
```

推荐常量：

- graph model：`property_graph`、`rdf`、`generic`。
- node shape kind：`label`、`label_set`、`class`、`inferred`。

`edge_count` 不再作为标准字段名，统一使用 `relationship_count`。如需兼容旧 Meta 数据，应通过重新扫描生成新 attributes，不在运行期保留双字段。

## Common Engine Provider 建议

provider 分层建议：

```go
type GraphMetadataProvider interface {
    EnginePlugin
    DescribeGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*datatype.GraphInfo, error)
}

type GraphSampleProvider interface {
    EnginePlugin
    SampleGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts GraphSampleOptions) (*GraphData, error)
}

type GraphQueryProvider interface {
    EnginePlugin
    ExecuteGraphQuery(ctx context.Context, connInfo ConnectionInfo, query string, opts QueryOptions) (*GraphQueryResult, error)
}
```

`ItemMetadataProvider.DescribeItem()` 可包装 `DescribeGraph()`：

```go
ItemMetadata{
    Kind:  "graph",
    Graph: graphInfo,
}
```

Neo4j catalog 已从：

```text
database
  label item
  relationship item
```

收敛为：

```text
database
  graph item
```

label 和 relationship type 列表由 `GraphInfo.NodeShapes` 与 `GraphInfo.RelationshipShapes` 提供，Manager 再按展示需要投影。

## 推进顺序

1. 已更新 `common/datatype.GraphInfo` 为 node / relationship shape 模型。
2. 已补充 `GraphInfo.Clone()`、`GraphInfoAttributes()`、`GraphInfoFromAttributes()` 等 helper。
3. 已同步 `docs/next/common-datatype统一抽象设计.md` 和 attributes 规范中的 graph 字段命名。
4. 已在 `common/engine/plugin` 增加 `ItemMetadata.Graph` 与 graph metadata provider。
5. 已改 Neo4j provider：以 database 下 graph item 作为主 item，GraphInfo 承载 label-set node shape、relationship shape 和 endpoint pattern 事实。
6. 已改 Meta namespace scan：graph item 落库，label / relationship type 进入 `type_info.graph`。
7. 已改 Manager graph preview：从 GraphInfo 投影通用概览。
8. 已改 Graph 模块浏览 schema 为 `node_shapes` / `relationship_shapes` / `patterns`，Browse schema 和 schema inference 均按完整 label set 生成 node shape，知识服务、schema 推导和图算法入口统一过滤 Neo4j 内部关系。
9. 已改 Service 图查询服务：入门向导从旧 `label` 模式调整为 `node shape` 模式，前端选择项来自 graph item 的 `type_info.graph.node_shapes`；服务保存 `node_shape` 与执行所需的 `node_labels` 映射，不再从 Meta 树读取 label item。
10. 已改 Graph 本体层：`EntityType.Name` 回到本体概念标识，新增 `EntityType.NodeLabels` 作为 Neo4j 执行映射，构建、知识服务、空间和算法查询统一通过映射解析 label set。
11. 已改 Graph Analysis：算法筛选请求使用 `node_shapes`，后端按完整 label set 过滤节点；GDS 在选择 node shape 时使用 Cypher projection，避免把多 label node shape 退化为宽松 label 匹配。
12. 已改空间图层链路：Graph 后端统一 `SpatialLayerMapping`，能力探测返回 `entity_type`、`entity_type_label` 与 `node_labels`；前端区域节点筛选使用 `node_labels`，不再把 spatial layer name 当作 label。
13. 已改图谱构建写入链路：实体和关系写入 Neo4j 都使用 `NodeLabels` / label set，审核内容中的执行映射统一命名为 `node_labels`、`source_node_labels`、`target_node_labels`，不再用 `ancestor_labels` 暗示概念继承就是 Neo4j label。
14. 已改 Graph Browser 节点形状筛选：多 label node shape 必须完整匹配该 label set，不再按任一 label 命中。
15. 已改 Graph 前端展示边界：节点详情在没有本体类型时显示完整 label set；审核修改弹窗把本体类型和 Neo4j 执行映射分开展示和编辑。
