-- 添加资源连接状态缓存字段
-- 用于优化Meta模块的扫描性能，避免每次都实时连接检测

ALTER TABLE system.resources
ADD COLUMN IF NOT EXISTS connection_status VARCHAR(20) DEFAULT 'unknown',
ADD COLUMN IF NOT EXISTS last_check_at TIMESTAMP,
ADD COLUMN IF NOT EXISTS check_message TEXT;

-- 添加索引以加速状态查询
CREATE INDEX IF NOT EXISTS idx_resources_connection_status ON system.resources(connection_status);

-- 注释说明
COMMENT ON COLUMN system.resources.connection_status IS '连接状态缓存：online/offline/unknown/checking';
COMMENT ON COLUMN system.resources.last_check_at IS '上次连接检测时间';
COMMENT ON COLUMN system.resources.check_message IS '检测结果消息（如错误信息）';
