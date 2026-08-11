-- 收敛旧版本遗留的空物化扩展字段。非空未知字段继续由服务端拒绝，避免静默丢弃配置。
UPDATE model.logical_tables
SET materialization = materialization - 'extra_options'
WHERE materialization ? 'extra_options'
  AND jsonb_typeof(materialization->'extra_options') = 'string'
  AND btrim(materialization->>'extra_options') = '';
