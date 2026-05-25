# ADDP Graph 统一抽象设计

> 状态：讨论后推进  
> 创建日期：2026-05-25

## 背景

ADDP 已将 table 型事实收敛到 `datatype.TableInfo`，并明确 table item 的主事实进入 `plugin.ItemMetadata.Table`。graph 当前仍存在概念混用：

- Neo4j catalog 以 `label` / `relationship` 作为叶子 item。
- Meta 扫描把 `label` / `relationship` 写成独立 `meta_item`。
- `type_info.graph` 仍使用 `node_count`、`edge_count`、`from_labels`、`to_labels` 等扁平事实。
- Manager 当前按 label / relationship 展示图结构，这只是 UI 投影视角，不应反向定义 `common/datatype`。

graph 的统一抽象需要先回到本体：图的核心是 node 和 relationship；label 只是节点分类或展示分组方式，不能作为 graph data type 的一等本体。

## 核心结论

1. `graph` 的 data item 应表示一个可被查询、采样、预览和治理的图整体。
2. `label`、`relationship type`、属性结构和连接模式属于 graph schema / shape facts，不应作为 graph data type 的核心 item 本体。
3. Neo4j 的 `label` / `relationship` 可以作为 Manager 展示中的虚拟节点或筛选入口，但不应长期作为 Meta 主数据项。
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

过渡期可以保留现有 catalog 展示能力，但新的事实模型应朝 graph item 收敛，不新增兼容别名或第二套 graph facts。

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

Neo4j catalog 的目标层级应从：

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

1. 更新 `common/datatype.GraphInfo` 为 node / relationship shape 模型。
2. 补充 `GraphInfo.Clone()`、`GraphInfoAttributes()`、`GraphInfoFromAttributes()` 等 helper。
3. 同步 `docs/next/common-datatype统一抽象设计.md` 和 attributes 规范中的 graph 字段命名。
4. 在 `common/engine/plugin` 增加 `ItemMetadata.Graph` 与 graph metadata provider。
5. 改 Neo4j provider：以 database 下 graph item 作为主 item，GraphInfo 承载 label/type/pattern 事实。
6. 改 Meta namespace scan：graph item 落库，label / relationship type 进入 `type_info.graph`。
7. 改 Manager graph preview：从 GraphInfo 投影 schema/sample/properties 视图。
