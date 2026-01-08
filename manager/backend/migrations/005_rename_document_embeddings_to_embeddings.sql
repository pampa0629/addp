-- 重命名向量嵌入表：document_embeddings → embeddings
-- 目的：使用更通用的命名，准确反映多模态支持（文本、图片、视频、音频、文档）

-- 重命名表
ALTER TABLE manager.document_embeddings RENAME TO embeddings;

-- 重命名唯一约束
ALTER INDEX manager.uk_document_embeddings_fingerprint RENAME TO uk_embeddings_fingerprint;

-- 重命名索引
ALTER INDEX manager.idx_document_embeddings_vector RENAME TO idx_embeddings_vector;
ALTER INDEX manager.idx_document_embeddings_tenant RENAME TO idx_embeddings_tenant;
ALTER INDEX manager.idx_document_embeddings_engine RENAME TO idx_embeddings_engine;
ALTER INDEX manager.idx_document_embeddings_fingerprint RENAME TO idx_embeddings_fingerprint;
ALTER INDEX manager.idx_document_embeddings_data_updated RENAME TO idx_embeddings_data_updated;

-- 更新表注释
COMMENT ON TABLE manager.embeddings IS '向量嵌入表（支持多模态：文本、图片、视频、音频、文档）';
