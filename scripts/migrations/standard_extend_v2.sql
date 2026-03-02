-- Standard 模块扩展迁移脚本 v2
-- 新增：计量单位、指标体系、数据分类分级、标准文档管理
-- 修改：elements 表（unit 字段替换为 unit_id，新增 security_level、classification_id）

BEGIN;

-- ============================================================
-- 1. 度量类别表
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.measurement_categories (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT NOT NULL,
    name        VARCHAR(100) NOT NULL,
    code        VARCHAR(50)  NOT NULL,
    description TEXT,
    sort_order  INT          NOT NULL DEFAULT 0,
    is_system   BOOLEAN      NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_mcat_tenant ON standard.measurement_categories(tenant_id);

-- ============================================================
-- 2. 计量单位表
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.units (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT      NOT NULL,
    category_id BIGINT      NOT NULL REFERENCES standard.measurement_categories(id),
    name        VARCHAR(100) NOT NULL,
    symbol      VARCHAR(30),
    description TEXT,
    sort_order  INT          NOT NULL DEFAULT 0,
    is_system   BOOLEAN      NOT NULL DEFAULT false,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_unit_tenant    ON standard.units(tenant_id);
CREATE INDEX IF NOT EXISTS idx_unit_category  ON standard.units(category_id);

-- ============================================================
-- 3. 数据分类表（树形，用户自定义）
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.classifications (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    name        VARCHAR(100) NOT NULL,
    code        VARCHAR(50)  NOT NULL,
    description TEXT,
    parent_id   BIGINT REFERENCES standard.classifications(id),
    sort_order  INT          NOT NULL DEFAULT 0,
    created_by  BIGINT       NOT NULL,
    updated_by  BIGINT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_classification_tenant    ON standard.classifications(tenant_id);
CREATE INDEX IF NOT EXISTS idx_classification_parent    ON standard.classifications(parent_id);

-- ============================================================
-- 4. 数据分级表（固定 L1-L4，每租户一份，用户可修改名称/描述/颜色）
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.grading_levels (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    level       VARCHAR(10)  NOT NULL,   -- L1/L2/L3/L4
    name        VARCHAR(50)  NOT NULL,
    description TEXT,
    color       VARCHAR(20),             -- 标签颜色（十六进制，如 #67C23A）
    sort_order  INT          NOT NULL DEFAULT 0,
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, level)
);

CREATE INDEX IF NOT EXISTS idx_grading_tenant ON standard.grading_levels(tenant_id);

-- ============================================================
-- 5. 指标目录（独立树形结构，独立于业务域）
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.metric_categories (
    id          BIGSERIAL PRIMARY KEY,
    tenant_id   BIGINT       NOT NULL,
    name        VARCHAR(100) NOT NULL,
    code        VARCHAR(50)  NOT NULL,
    description TEXT,
    parent_id   BIGINT REFERENCES standard.metric_categories(id),
    sort_order  INT          NOT NULL DEFAULT 0,
    created_by  BIGINT       NOT NULL,
    updated_by  BIGINT,
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_mcat2_tenant ON standard.metric_categories(tenant_id);
CREATE INDEX IF NOT EXISTS idx_mcat2_parent ON standard.metric_categories(parent_id);

-- ============================================================
-- 6. 指标定义表
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.metrics (
    id                BIGSERIAL PRIMARY KEY,
    tenant_id         BIGINT       NOT NULL,
    category_id       BIGINT REFERENCES standard.metric_categories(id),
    domain_id         BIGINT,                   -- 辅助标注，关联的主业务域
    name              VARCHAR(200) NOT NULL,
    code              VARCHAR(100) NOT NULL,
    type              VARCHAR(20)  NOT NULL,     -- atomic/derived/composite
    definition        TEXT,                      -- 业务口径描述
    formula           TEXT,                      -- 复合指标：计算公式描述
    unit_id           BIGINT REFERENCES standard.units(id),
    base_metric_id    BIGINT REFERENCES standard.metrics(id),   -- 派生指标的基准原子指标
    derivation_config JSONB,                     -- 派生配置：时间粒度、过滤条件、维度
    status            VARCHAR(20)  NOT NULL DEFAULT 'draft',
    steward_id        BIGINT,
    tags              JSONB,
    created_by        BIGINT       NOT NULL,
    updated_by        BIGINT,
    created_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    UNIQUE (tenant_id, code)
);

CREATE INDEX IF NOT EXISTS idx_metric_tenant   ON standard.metrics(tenant_id);
CREATE INDEX IF NOT EXISTS idx_metric_category ON standard.metrics(category_id);
CREATE INDEX IF NOT EXISTS idx_metric_status   ON standard.metrics(status);

-- ============================================================
-- 7. 指标与数据元关联（原子指标使用）
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.metric_element_mappings (
    id         BIGSERIAL PRIMARY KEY,
    metric_id  BIGINT NOT NULL REFERENCES standard.metrics(id) ON DELETE CASCADE,
    element_id BIGINT NOT NULL,
    note       TEXT,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (metric_id, element_id)
);

-- ============================================================
-- 8. 复合指标依赖关系
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.metric_dependencies (
    id             BIGSERIAL PRIMARY KEY,
    from_metric_id BIGINT NOT NULL REFERENCES standard.metrics(id) ON DELETE CASCADE,
    to_metric_id   BIGINT NOT NULL REFERENCES standard.metrics(id),
    coefficient    NUMERIC,
    note           TEXT,
    created_at     TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (from_metric_id, to_metric_id)
);

-- ============================================================
-- 9. 标准文档表
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.documents (
    id           BIGSERIAL PRIMARY KEY,
    tenant_id    BIGINT       NOT NULL,
    name         VARCHAR(200) NOT NULL,
    doc_type     VARCHAR(50)  NOT NULL DEFAULT 'reference',  -- national/industry/internal/reference
    source_org   VARCHAR(200),
    version      VARCHAR(50),
    publish_date DATE,
    description  TEXT,
    file_key     TEXT,          -- MinIO 存储路径
    file_name    VARCHAR(200),
    file_size    BIGINT,
    created_by   BIGINT        NOT NULL,
    updated_by   BIGINT,
    created_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ   NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_doc_tenant ON standard.documents(tenant_id);

-- ============================================================
-- 10. 文档关联表（文档 ↔ 数据元/术语/指标）
-- ============================================================
CREATE TABLE IF NOT EXISTS standard.document_element_mappings (
    id                 BIGSERIAL PRIMARY KEY,
    document_id        BIGINT NOT NULL REFERENCES standard.documents(id) ON DELETE CASCADE,
    element_id         BIGINT NOT NULL,
    reference_location TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, element_id)
);

CREATE TABLE IF NOT EXISTS standard.document_glossary_mappings (
    id                 BIGSERIAL PRIMARY KEY,
    document_id        BIGINT NOT NULL REFERENCES standard.documents(id) ON DELETE CASCADE,
    glossary_id        BIGINT NOT NULL,
    reference_location TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, glossary_id)
);

CREATE TABLE IF NOT EXISTS standard.document_metric_mappings (
    id                 BIGSERIAL PRIMARY KEY,
    document_id        BIGINT NOT NULL REFERENCES standard.documents(id) ON DELETE CASCADE,
    metric_id          BIGINT NOT NULL,
    reference_location TEXT,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (document_id, metric_id)
);

-- ============================================================
-- 11. 修改 elements 表：删除旧 unit 字段，新增结构化字段
-- ============================================================
ALTER TABLE standard.elements
    DROP COLUMN IF EXISTS unit,
    ADD COLUMN IF NOT EXISTS unit_id           BIGINT REFERENCES standard.units(id),
    ADD COLUMN IF NOT EXISTS security_level    VARCHAR(10),   -- L1/L2/L3/L4
    ADD COLUMN IF NOT EXISTS classification_id BIGINT REFERENCES standard.classifications(id);

CREATE INDEX IF NOT EXISTS idx_element_unit   ON standard.elements(unit_id);
CREATE INDEX IF NOT EXISTS idx_element_clsfy  ON standard.elements(classification_id);

-- ============================================================
-- 12. 初始化数据：分级标准（默认租户 id=1）
-- ============================================================
INSERT INTO standard.grading_levels (tenant_id, level, name, description, color, sort_order)
VALUES
    (1, 'L1', '公开',   '可对外发布，无敏感性',               '#67C23A', 1),
    (1, 'L2', '内部',   '限内部员工访问，不得对外公开',        '#409EFF', 2),
    (1, 'L3', '机密',   '严格受控，需定向授权方可访问',        '#E6A23C', 3),
    (1, 'L4', '绝密',   '最高保护级别，极少数人可访问',        '#F56C6C', 4)
ON CONFLICT (tenant_id, level) DO NOTHING;

-- ============================================================
-- 13. 初始化数据：度量类别和常用单位（默认租户 id=1）
-- ============================================================
INSERT INTO standard.measurement_categories (tenant_id, name, code, description, sort_order, is_system)
VALUES
    (1, '计数',   'count',       '表示数量、个数、次数等无量纲单位',  1, true),
    (1, '货币',   'currency',    '表示金额、价格等货币值',            2, true),
    (1, '质量',   'mass',        '表示物体质量',                      3, true),
    (1, '长度',   'length',      '表示距离、长度',                    4, true),
    (1, '面积',   'area',        '表示面积大小',                      5, true),
    (1, '体积',   'volume',      '表示体积或容积',                    6, true),
    (1, '时间',   'time',        '表示时间长度',                      7, true),
    (1, '温度',   'temperature', '表示温度',                          8, true),
    (1, '百分比', 'percentage',  '表示比率、百分比',                  9, true),
    (1, '存储',   'storage',     '表示数据存储大小',                  10, true)
ON CONFLICT (tenant_id, code) DO NOTHING;

-- 计数单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '个',  '个',  '计量个体数量',  1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '件',  '件',  '计量件数',      2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '次',  '次',  '计量操作次数',  3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '条',  '条',  '计量记录条数',  4, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '批',  '批',  '计量批次',      5, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '人',  '人',  '计量人数',      6, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '户',  '户',  '计量户数',      7, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='count'
ON CONFLICT DO NOTHING;

-- 货币单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '元',   '¥',   '人民币元',   1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='currency'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '万元', '万¥', '人民币万元', 2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='currency'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '亿元', '亿¥', '人民币亿元', 3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='currency'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '美元', '$',   '美元',       4, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='currency'
ON CONFLICT DO NOTHING;

-- 质量单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '克',   'g',  '克',   1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='mass'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '千克', 'kg', '千克', 2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='mass'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '吨',   't',  '吨',   3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='mass'
ON CONFLICT DO NOTHING;

-- 长度单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '毫米', 'mm', '毫米', 1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='length'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '厘米', 'cm', '厘米', 2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='length'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '米',   'm',  '米',   3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='length'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '千米', 'km', '千米', 4, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='length'
ON CONFLICT DO NOTHING;

-- 面积单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '平方米', 'm²', '平方米', 1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='area'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '亩',     '亩', '亩',     2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='area'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '公顷',   'ha', '公顷',   3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='area'
ON CONFLICT DO NOTHING;

-- 时间单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '秒',   's',   '秒',   1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '分钟', 'min', '分钟', 2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '小时', 'h',   '小时', 3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '天',   'd',   '天',   4, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '月',   'mo',  '月',   5, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '年',   'yr',  '年',   6, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='time'
ON CONFLICT DO NOTHING;

-- 百分比
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, '百分比', '%', '百分比', 1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='percentage'
ON CONFLICT DO NOTHING;

-- 存储单位
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, 'MB', 'MB', '兆字节',  1, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='storage'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, 'GB', 'GB', '吉字节',  2, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='storage'
ON CONFLICT DO NOTHING;
INSERT INTO standard.units (tenant_id, category_id, name, symbol, description, sort_order, is_system)
SELECT 1, id, 'TB', 'TB', '太字节',  3, true FROM standard.measurement_categories WHERE tenant_id=1 AND code='storage'
ON CONFLICT DO NOTHING;

COMMIT;
