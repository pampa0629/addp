# Neo4j Node Type 修订设计（后实施）

> 状态：待评审  
> 创建日期：2026-04-24  
> 前置条件：已完成《nosql插件接口分拆设计》并合入主干。

---

## 一、背景

在当前实现中，Neo4j 元数据落库存在两类语义偏差：

1. Node Label 被存为 `item_type = "collection"`（MongoDB 术语）
2. Relationship Type 被存为 `item_type = "relationship_type"`

你已明确目标术语：
- Node type 用 `label`
- Relationship 用 `relationship`（不要 `relationship_type`）

此外，Neo4j 的 `RTREE_*` 关系属于内部空间索引实现细节，不应暴露给业务元数据层。

---

## 二、设计目标

1. Neo4j 元数据术语与业界主流保持一致：
   - `label`
   - `relationship`
2. 过滤 Neo4j 内部关系类型（`RTREE_*`）
3. 保证 Meta、Manager、System 等支持 Neo4j 引擎的模块行为一致
4. 完成历史数据迁移，避免新老 `item_type` 混杂

---

## 三、术语映射规则（目标态）

### 3.1 MongoDB（不变）

- 集合：`item_type = "collection"`

### 3.2 Neo4j（变更）

- 节点标签：`item_type = "label"`
- 关系类型：`item_type = "relationship"`

### 3.3 禁止值（Neo4j 场景）

- `collection`（用于 Neo4j 时）
- `relationship_type`

---

## 四、代码改造范围

### 4.1 Meta 扫描服务

文件：
- `meta/backend/internal/service/scan_nosql_service.go`

改造点：
1. Neo4j label 持久化时，`UpsertItem(..., "label", ...)`
2. Neo4j relationship 持久化时，`UpsertItem(..., "relationship", ...)`
3. 软删除查询条件同步改为新值：
   - `item_type = 'label'`
   - `item_type = 'relationship'`

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
- 读取 Neo4j label 的逻辑改为匹配 `label`
- 读取 Neo4j relationship 的逻辑改为匹配 `relationship`
- 避免与 MongoDB 的 `collection` 规则冲突

### 4.4 文档规范同步

需同步修订：
- `docs/spec/addp存储引擎路径体系规范.md`
- Meta/Manager 相关表结构或接口文档（若包含旧术语）

---

## 五、数据迁移方案

> 由于 ADDP 当前开发策略不要求向后兼容，可直接迁移并清理旧值。

### 5.1 迁移目标

在 `metadata.meta_item` 中，针对 Neo4j 引擎记录执行：

- `collection` → `label`
- `relationship_type` → `relationship`

### 5.2 迁移 SQL（示意）

```sql
-- 1) Neo4j label: collection -> label
UPDATE metadata.meta_item mi
SET item_type = 'label', updated_at = NOW()
FROM system.engines e
WHERE mi.engine_id = e.id
  AND e.engine_type = 'neo4j'
  AND mi.item_type = 'collection';

-- 2) Neo4j relationship: relationship_type -> relationship
UPDATE metadata.meta_item mi
SET item_type = 'relationship', updated_at = NOW()
FROM system.engines e
WHERE mi.engine_id = e.id
  AND e.engine_type = 'neo4j'
  AND mi.item_type = 'relationship_type';
```

### 5.3 清理与校验

```sql
-- 不应再存在的旧值（Neo4j）
SELECT mi.id, mi.engine_id, mi.item_type, mi.name
FROM metadata.meta_item mi
JOIN system.engines e ON mi.engine_id = e.id
WHERE e.engine_type = 'neo4j'
  AND mi.item_type IN ('collection', 'relationship_type');
```

结果应为 0 行。

---

## 六、实施顺序（本阶段）

1. 合入接口分拆改造（前置）
2. 修改 Neo4j 插件过滤 `RTREE_*`
3. 修改 Meta Neo4j 落库 item_type（label/relationship）
4. 修改 Manager 读取逻辑
5. 执行数据迁移 SQL
6. 更新规范文档
7. 完成回归测试

---

## 七、回归测试清单

### 7.1 Neo4j 扫描

- 扫描后应出现 `item_type='label'`
- 扫描后应出现 `item_type='relationship'`
- 不应出现 `RTREE_*` 关系

### 7.2 MongoDB 扫描

- `item_type='collection'` 保持不变
- 预览与搜索行为不受影响

### 7.3 Manager 展示

- Neo4j label 列表可见
- Neo4j relationship 列表可见
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

1. Neo4j 新扫描数据只出现：`label`、`relationship`
2. Neo4j 数据中不再出现：`collection`、`relationship_type`
3. `RTREE_*` 不再进入元数据
4. MongoDB 行为不变
5. 文档、代码、数据库三者术语一致
