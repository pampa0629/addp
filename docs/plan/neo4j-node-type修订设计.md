# Neo4j Node Type 修订设计（历史废弃）

> 状态：历史方案，已废弃
> 创建日期：2026-04-24
> 当前准则：Neo4j catalog leaf 统一为 `graph`；label、relationship type 和 endpoint pattern 进入 `attributes.type_info.graph`，不作为独立 `meta_item`。

---

## 一、背景

在当前实现中，Neo4j 元数据落库存在两类语义偏差：

1. Node Label 被存为 `item_type = "collection"`（MongoDB 术语）
2. Relationship Type 被存为 `item_type = "relationship_type"`

该文档曾计划把 Neo4j label / relationship type 落为独立 `meta_item`。当前架构已经收敛：Neo4j 的 ADDP data item 是 graph 整体，label / relationship type 只是图结构事实。

此外，Neo4j 的 `RTREE_*` 关系属于内部空间索引实现细节，不应暴露给业务元数据层。

---

## 二、设计目标

1. Neo4j catalog model 使用 `database -> graph`。
2. Neo4j label、relationship type 和 endpoint pattern 写入 `type_info.graph`。
3. 过滤 Neo4j 内部关系类型（`RTREE_*`），避免进入 graph schema / sample 事实。
4. Meta、Manager、Graph、Service 等模块围绕 graph item 消费图结构事实。

---

## 三、术语映射规则（目标态）

### 3.1 MongoDB（不变）

- 集合：`item_type = "collection"`

### 3.2 Neo4j（当前）

- 图整体：`item_type = "graph"`
- 节点标签：`type_info.graph.node_shapes`
- 关系类型：`type_info.graph.relationship_shapes`

### 3.3 禁止值（Neo4j 场景）

- `collection`（用于 Neo4j 时）
- `label`（作为独立 `meta_item.item_type`）
- `relationship` / `relationship_type`（作为独立 `meta_item.item_type`）

---

## 四、代码改造范围

### 4.1 Meta 扫描服务

改造点：
1. Neo4j database 下只落 graph item。
2. label / relationship type 不进入 `meta_item.item_type`。
3. 图结构事实进入 `attributes.type_info.graph`。

### 4.2 Neo4j 插件

文件：
- `common/engine/plugins/neo4j/plugin.go`

改造点：
1. 在 `ListRelationshipTypes()` 过滤内部关系：
   - `RTREE_METADATA`
   - `RTREE_REFERENCE`
   - `RTREE_ROOT`
2. 过滤逻辑放在插件层，避免上层重复判断。

### 4.3 Manager 读取与展示

重点检查所有基于 `item_type` 的分支/过滤：
- Neo4j 只匹配 `graph`。
- label / relationship type 作为图结构筛选条件，不作为 item 类型。
- 避免与 MongoDB 的 `collection` 规则冲突。

### 4.4 文档规范同步

需同步修订：
- `docs/spec/addp存储引擎路径体系规范.md`
- Meta/Manager 相关表结构或接口文档（若包含旧术语）

---

## 五、数据迁移方案

> 由于 ADDP 当前开发策略不要求向后兼容，可直接迁移并清理旧值。

### 5.1 迁移目标

在 `metadata.meta_item` 中，针对 Neo4j 引擎历史记录执行清理或重扫：

- `collection`、`label`、`relationship`、`relationship_type` 等历史拆分值不再作为最终主路径。
- 推荐在开发阶段直接清理 Neo4j 历史扫描结果并重新扫描，生成 `item_type='graph'`。

### 5.2 迁移 SQL（示意）

```sql
DELETE FROM metadata.meta_item mi
USING system.engines e
WHERE mi.engine_id = e.id
  AND e.engine_type = 'neo4j'
  AND mi.item_type IN ('collection', 'label', 'relationship', 'relationship_type');
```

### 5.3 清理与校验

```sql
-- 不应再存在的旧值（Neo4j）
SELECT mi.id, mi.engine_id, mi.item_type, mi.name
FROM metadata.meta_item mi
JOIN system.engines e ON mi.engine_id = e.id
WHERE e.engine_type = 'neo4j'
  AND mi.item_type IN ('collection', 'label', 'relationship', 'relationship_type');
```

结果应为 0 行。

---

## 六、实施顺序（本阶段）

1. 确认 Neo4j catalog model 为 `database -> graph`
2. 确认 Neo4j 插件过滤 `RTREE_*`
3. 确认 Meta Neo4j 落库 `item_type=graph`
4. 确认 Manager / Graph 读取 `type_info.graph`
5. 清理或重扫历史 Neo4j 数据
6. 更新规范文档
7. 完成回归测试

---

## 七、回归测试清单

### 7.1 Neo4j 扫描

- 扫描后应出现 `item_type='graph'`
- label / relationship type 进入 `type_info.graph`
- 不应出现 `RTREE_*` 关系

### 7.2 MongoDB 扫描

- `item_type='collection'` 保持不变
- 预览与搜索行为不受影响

### 7.3 Manager 展示

- Neo4j graph item 可见
- Neo4j label / relationship type 可作为图结构视角展示
- 分类与筛选正确

### 7.4 文档一致性

- 规范文档术语与代码一致
- 不再出现“Neo4j label 以 collection 落库”的描述

---

## 八、风险与应对

### 风险 1：历史数据与新代码不一致

应对：上线前执行迁移 SQL，并在启动检查中增加告警（发现旧值即提示）。

### 风险 2：Manager 仍按旧值过滤导致数据“消失”

应对：在同一变更集内同步修复 Manager 过滤条件，禁止拆分发布。

### 风险 3：RTREE 过滤不全

应对：先做白名单/黑名单评估；短期先黑名单 `RTREE_*`，后续根据版本扩展。

---

## 九、验收标准

1. Neo4j 新扫描数据只出现：`graph`
2. Neo4j 数据中不再出现：`collection`、`label`、`relationship`、`relationship_type` 作为 item type
3. `RTREE_*` 不再进入元数据
4. MongoDB 行为不变
5. 文档、代码、数据库三者术语一致
