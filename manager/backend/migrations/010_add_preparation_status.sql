-- Add preparation_status column to quick_view table
-- Tracks the preparation phase check results (materialized view, spatial index, ANALYZE)

ALTER TABLE manager.quick_view
ADD COLUMN preparation_status JSONB;

CREATE INDEX idx_quick_view_preparation_status ON manager.quick_view ((preparation_status->>'overall_status'))
WHERE preparation_status IS NOT NULL;

COMMENT ON COLUMN manager.quick_view.preparation_status IS '准备阶段检查结果（物化视图、空间索引、ANALYZE）';
