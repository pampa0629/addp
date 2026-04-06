-- 图查询服务表
-- 用于存储 Neo4j 等图数据库的数据服务发布配置

CREATE TABLE IF NOT EXISTS service.graph_query_services (
    id              BIGSERIAL PRIMARY KEY,
    tenant_id       BIGINT NOT NULL,
    service_name    VARCHAR(255) NOT NULL,
    title           VARCHAR(255) NOT NULL,
    description     TEXT,
    keywords        TEXT[],

    -- 引擎配置
    engine_id       BIGINT NOT NULL,
    database_name   VARCHAR(255) NOT NULL DEFAULT 'neo4j',

    -- 配置类型: 'label'（节点标签向导）| 'cypher'（手写参数化查询）
    config_type     VARCHAR(50) NOT NULL,

    -- label 模式：选择节点标签
    node_label      VARCHAR(255),

    -- cypher 模式：手写参数化 Cypher
    cypher_query    TEXT,

    -- 数据配置（同时存储参数定义和模式特定配置）
    -- label 模式: {
    --   "properties":["id","name","age"],
    --   "filterable_properties":["name","age"]
    -- }
    -- cypher 模式: {
    --   "result_type":"table|graph|both",
    --   "parameters":[{"name":"city","type":"string","required":true}]
    -- }
    data_config     JSONB NOT NULL DEFAULT '{}',

    -- 访问控制
    public_access   BOOLEAN NOT NULL DEFAULT FALSE,
    max_records     INTEGER NOT NULL DEFAULT 500,

    -- 状态
    status          VARCHAR(50) NOT NULL DEFAULT 'active',
    error_message   TEXT,

    -- 审计
    created_by      BIGINT NOT NULL,
    created_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMP WITH TIME ZONE NOT NULL DEFAULT NOW(),

    CONSTRAINT unique_graph_service_name UNIQUE (tenant_id, service_name),
    CONSTRAINT chk_graph_config_type CHECK (config_type IN ('label', 'cypher')),
    CONSTRAINT chk_graph_status CHECK (status IN ('active', 'inactive', 'error'))
);

CREATE INDEX idx_graph_query_services_tenant     ON service.graph_query_services(tenant_id);
CREATE INDEX idx_graph_query_services_engine     ON service.graph_query_services(engine_id);
CREATE INDEX idx_graph_query_services_status     ON service.graph_query_services(status);
CREATE INDEX idx_graph_query_services_config_gin ON service.graph_query_services USING GIN (data_config);
