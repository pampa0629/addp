# ADDP Model元模型

[返回核心对象 ER 图](../addp核心对象ER图（元模型）.md)

实体、逻辑表、概念实现映射、维度层级和指标实现，以及 Standard 修订冻结边界。

## Model 模块核心对象（schema: model）

| 中文     | 英文标识符       | 说明                                              |
| -------- | ---------------- | ------------------------------------------------- |
| 逻辑表   | LogicalTable     | 数仓逻辑层的表定义（实体/事实/维度表，ODS/DWD/DWS/ADS 层） |
| 逻辑字段 | LogicalField     | 逻辑表的字段，草稿软引用 Standard.Element，审批冻结确定 ElementRevision           |

---

## Model 模块对象图

```mermaid
erDiagram
    DWLayer {
        uint id PK
        uint tenant_id FK
        string code "ods|dwd|dws|ads"
        string name
        string description
        int sort_order
        timestamp created_at
        timestamp updated_at
    }

    Entity {
        uint id PK
        uint tenant_id FK
        uint domain_id FK "软引用 Standard.Domain"
        string name
        string code UK
        string description
        string status
        timestamp created_at
        timestamp updated_at
    }

    EntityAttribute {
        uint id PK
        uint entity_id FK
        uint element_id "草稿长期引用 Standard.Element；无跨 schema DB FK"
        uint element_revision_id FK "软引用 Standard.ElementRevision"
        string name
        string column_name
        string data_type
        bool is_pk
        bool is_required
        string description
        int sort_order
    }

    EntityRelation {
        uint id PK
        uint tenant_id FK
        uint source_entity_id FK
        uint target_entity_id FK
        string relation_type "one_to_one|one_to_many|many_to_many"
        string description
    }

    LogicalTable {
        uint id PK
        uint tenant_id FK
        uint domain_id FK "软引用 Standard.Domain"
        uint dw_layer_id FK
        string name
        string code UK
        string table_type "entity|fact|dimension"
        string grain_description "粒度描述(事实表)"
        string scd_type "0|1|2|3 缓慢变化维类型(维度表)"
        string status
        timestamp created_at
        timestamp updated_at
    }

    LogicalTableEntityMapping {
        uint id PK
        uint tenant_id FK
        uint table_id FK
        uint entity_id FK
        string mapping_role "represents|derives_from"
        int entity_version "冻结实体版本"
    }

    LogicalFieldAttributeMapping {
        uint id PK
        uint tenant_id FK
        uint table_id FK
        uint field_id FK
        uint entity_id FK
        uint entity_attribute_id FK
        string mapping_role "direct|derived"
    }

    TableRelationEntityRelationMapping {
        uint id PK
        uint tenant_id FK
        uint table_id FK
        uint table_relation_id FK
        uint entity_relation_id FK
        string orientation "same|inverse"
        int entity_relation_version "冻结实体关系版本"
    }

    LogicalField {
        uint id PK
        uint table_id FK
        uint element_id "草稿长期引用 Standard.Element；无跨 schema DB FK"
        uint element_revision_id FK "软引用 Standard.ElementRevision(可选)"
        string name
        string column_name
        string data_type
        bool is_pk
        bool is_partition
        string field_role "regular|measure_additive|measure_semi|measure_non|dimension_fk|degenerate_dim"
        int sort_order
    }

    TableRelation {
        uint id PK
        uint tenant_id FK
        uint source_table_id FK
        uint source_field_id FK
        uint target_table_id FK
        uint target_field_id FK
        string relation_type "fk|join"
        string description
    }

    DimensionHierarchy {
        uint id PK
        uint tenant_id FK
        uint table_id FK "关联维度表"
        string name
        string description
        timestamp created_at
        timestamp updated_at
    }

    DimensionHierarchyLevel {
        uint id PK
        uint hierarchy_id FK
        uint field_id FK "关联 LogicalField"
        int level_num
        string level_name
    }

    MetricImplementation {
        uint id PK
        uint tenant_id FK
        uint fact_table_id FK
        uint metric_definition_id FK "软引用 Standard.MetricDefinition 稳定身份"
        uint metric_definition_revision_id FK "软引用 Standard.MetricDefinitionRevision"
        string name
        string grain "计算粒度"
        json source_config "事实来源与字段"
        json dimension_config "参与维度与连接"
        json filter_config "过滤条件"
        json expression_config "可执行表达式"
        string status
    }

    DWLayer ||--o{ LogicalTable : "所在层"
    Entity ||--o{ EntityAttribute : "含属性"
    Entity ||--o{ EntityRelation : "源实体"
    LogicalTable ||--o{ LogicalField : "含字段"
    LogicalTable ||--o{ TableRelation : "源表关系"
    LogicalTable ||--o{ LogicalTableEntityMapping : "实现概念实体"
    Entity ||--o{ LogicalTableEntityMapping : "被逻辑表实现"
    LogicalField ||--o{ LogicalFieldAttributeMapping : "实现概念属性"
    EntityAttribute ||--o{ LogicalFieldAttributeMapping : "被逻辑字段实现"
    TableRelation ||--o{ TableRelationEntityRelationMapping : "实现概念关系"
    EntityRelation ||--o{ TableRelationEntityRelationMapping : "被表关系实现"
    LogicalTable ||--o{ DimensionHierarchy : "含维度层级(维度表)"
    LogicalTable ||--o{ MetricImplementation : "指标实现(事实表)"
    DimensionHierarchy ||--o{ DimensionHierarchyLevel : "含层级"
    DimensionHierarchyLevel }o--|| LogicalField : "关联字段"
```

## 引用约束与待核对范围

- EntityAttribute 和 LogicalField 在草稿中保存 `element_id`；审批时冻结确定的 `element_revision_id`，正式消费不能动态跟随当前数据元版本，不建立跨 schema 外键。规则以 [Model 概念与数据约束规范](../../../model/docs/model概念与数据约束规范.md) 为准。待核对范围：审批、导入及展示路径是否均按此规则消费冻结修订；本页不提供这些路径的运行验收结论。
- MetricImplementation 同时保存指标稳定定义身份与确定的 `metric_definition_revision_id`，独占粒度、来源、连接、过滤和可执行表达式；不保留 FactMetricMapping。
- Entity 与 LogicalTable 通过表、字段、关系三类概念实现映射建立追溯；事实表和维度表复用唯一逻辑表，不保留 `LogicalTable.entity_id` 或第二套实体逻辑表。
- Model 独占 DimensionHierarchy 与成员；层级序号从 1 开始，成员只引用同一 LogicalTable 字段并共用父版本，LogicalField 不保存 `hierarchy_id + hierarchy_level`。

---
