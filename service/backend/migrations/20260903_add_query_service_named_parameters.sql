ALTER TABLE service.query_services
    ADD COLUMN IF NOT EXISTS named_parameters jsonb NOT NULL DEFAULT '[]'::jsonb;

COMMENT ON COLUMN service.query_services.named_parameters IS
'SQL 模式查询服务的强类型标量命名参数定义；表模式必须为空';
