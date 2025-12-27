-- 启用 pgvector 扩展
CREATE EXTENSION IF NOT EXISTS vector;

-- 创建 schema
CREATE SCHEMA IF NOT EXISTS vector_store;

-- 创建多模态向量表（支持动态维度）
CREATE TABLE IF NOT EXISTS vector_store.embeddings (
    id TEXT PRIMARY KEY,
    file_path TEXT NOT NULL,
    file_name TEXT NOT NULL,
    file_size BIGINT,
    modality TEXT NOT NULL,  -- 'image', 'text', 'video', 'multi_images'
    model TEXT NOT NULL DEFAULT 'tongyi-embedding-vision-plus',
    embedding vector,  -- 动态维度，由 ALTER TABLE 后续设置
    dimension INT NOT NULL,
    metadata JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (file_path, modality, model)
);

-- 创建辅助索引
CREATE INDEX IF NOT EXISTS embeddings_file_path_idx
ON vector_store.embeddings(file_path);

CREATE INDEX IF NOT EXISTS embeddings_modality_idx
ON vector_store.embeddings(modality);

CREATE INDEX IF NOT EXISTS embeddings_created_at_idx
ON vector_store.embeddings(created_at DESC);

-- 添加注释
COMMENT ON TABLE vector_store.embeddings IS '多模态向量存储表（图片/文本/视频）';
COMMENT ON COLUMN vector_store.embeddings.embedding IS '向量数据（维度由首次 API 调用确定）';
COMMENT ON COLUMN vector_store.embeddings.modality IS '模态类型：image（单图）、text（文本）、video（视频）、multi_images（多图）';
COMMENT ON COLUMN vector_store.embeddings.model IS '模型名称，默认：tongyi-embedding-vision-plus';
