-- 迁移脚本：重命名 task_api_config 为 extension_api_config
-- 日期：2026-01-01
-- 说明：task_api_config 名称容易误导，实际是通用的 HTTP API 配置框架，重命名为 extension_api_config 更准确

-- 重命名列
ALTER TABLE system.engines
  RENAME COLUMN task_api_config TO extension_api_config;

-- 添加注释
COMMENT ON COLUMN system.engines.extension_api_config IS '扩展引擎 API 配置（JSONB，仅扩展引擎使用）';
