-- ⚠️ 此脚本已废弃（2026-01-09）
-- 表结构现已由 GORM AutoMigrate 管理
-- 见：manager/backend/internal/models/embedding.go
-- 见：manager/backend/internal/repository/database.go
--
-- 保留此文件作为参考文档，了解向量嵌入表的设计思路和索引配置
-- 实际表创建由 Manager 模块启动时的 AutoMigrate 自动执行
--
-- ==================== 原始脚本内容（仅供参考） ====================

-- 创建向量嵌入表（在 manager schema）
-- 用于存储多模态对象（文本、图片、视频、音频、文档）的向量嵌入
-- 迁移说明：向量化功能从 Meta 模块迁移到 Manager 模块
--          schema 从 vector_store 迁移到 manager
--          添加 fingerprint 和 data_updated_at 支持去重

-- 确保 vector 扩展已启用（已在容器中安装，这里只需启用）
CREATE EXTENSION IF NOT EXISTS vector;

CREATE TABLE IF NOT EXISTS manager.embeddings (
    id SERIAL PRIMARY KEY,
    engine_id INTEGER NOT NULL,                    -- 引擎ID（来自 system.engines）
    bucket VARCHAR(255) NOT NULL,                  -- 存储桶名称
    object_key TEXT NOT NULL,                      -- 对象路径（相对于bucket）
    fingerprint VARCHAR(64) NOT NULL,              -- 数据指纹（使用 common.models.GenerateObjectFingerprint）
    data_updated_at TIMESTAMP,                     -- 对象的最后修改时间（从 MinIO LastModified）
    embedding vector(1024) NOT NULL,               -- 向量嵌入（维度 1024）
    modality VARCHAR(20) NOT NULL CHECK (modality IN ('text', 'image', 'audio', 'video', 'document')),
    model VARCHAR(100) NOT NULL,                   -- 向量模型名称
    file_size BIGINT,                              -- 文件大小（字节）
    content_type VARCHAR(255),                     -- MIME类型
    metadata JSONB,                                -- 额外元数据
    tenant_id INTEGER,                             -- 租户ID（用于多租户隔离）
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),

    -- 唯一约束：同一指纹和模态类型只保留一条记录
    CONSTRAINT uk_embeddings_fingerprint UNIQUE (fingerprint, modality)
);

-- 向量相似度搜索索引（使用 IVFFlat 索引，余弦距离）
CREATE INDEX idx_embeddings_vector
ON manager.embeddings
USING ivfflat (embedding vector_cosine_ops)
WITH (lists = 100);

-- 租户隔离索引
CREATE INDEX idx_embeddings_tenant
ON manager.embeddings (tenant_id);

-- 引擎查询索引
CREATE INDEX idx_embeddings_engine
ON manager.embeddings (engine_id, bucket);

-- 指纹索引（用于快速检查是否已向量化）
CREATE INDEX idx_embeddings_fingerprint
ON manager.embeddings (fingerprint);

-- 数据更新时间索引（用于检测过期向量）
CREATE INDEX idx_embeddings_data_updated
ON manager.embeddings (data_updated_at);

-- 添加注释
COMMENT ON TABLE manager.embeddings IS '向量嵌入表（支持多模态：文本、图片、视频、音频、文档）';
COMMENT ON COLUMN manager.embeddings.fingerprint IS '数据指纹（SHA256，用于去重）';
COMMENT ON COLUMN manager.embeddings.data_updated_at IS '对象最后修改时间（用于检测数据变化，决定是否重新向量化）';
COMMENT ON COLUMN manager.embeddings.embedding IS '向量嵌入（维度1024，使用阿里云 qwen2.5-vl-embedding 模型）';
