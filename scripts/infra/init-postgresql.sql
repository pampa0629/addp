-- 全域数据平台数据库初始化脚本
-- 为各个模块创建独立的 schema
--
-- 重要变更（2026-01-09）:
-- - 所有表结构由各模块的 GORM AutoMigrate 创建
-- - 本脚本仅负责：扩展安装、Schema 创建、触发器函数
-- - 旧版 CREATE TABLE 语句已移除，请参考各模块的 models 定义
--
-- 空间数据与向量扩展支持:
-- - PostGIS: 空间数据类型和空间函数 (geometry, geography等)
-- - pgvector: 向量嵌入和相似度搜索 (vector类型, 支持多模态AI应用)
--   * manager schema: Manager 模块专用（向量嵌入表）

-- ==================== PostGIS 扩展 ====================
-- PostGIS 扩展预装于 PostGIS 镜像中
-- ARM64: imresamu/postgis-arm64:15-3.4
-- AMD64: postgis/postgis:15-3.4
CREATE EXTENSION IF NOT EXISTS postgis;
CREATE EXTENSION IF NOT EXISTS postgis_topology;

-- ==================== 创建 Schema ====================
-- 所有表由各模块的 GORM AutoMigrate 自动创建
-- 各模块启动时会自动创建和迁移表结构

-- System 模块 (用户、租户、引擎、应用、API密钥)
CREATE SCHEMA IF NOT EXISTS system;

-- Manager 模块 (搜索历史、向量嵌入、快显缓存)
CREATE SCHEMA IF NOT EXISTS manager;

-- Manager 模块需要使用 pgvector 扩展来存储向量嵌入
-- 在 manager schema 中创建 vector 扩展，使 vector 类型可用
CREATE EXTENSION IF NOT EXISTS vector SCHEMA manager;

-- Meta 模块 (元数据节点、元数据条目、扫描任务)
CREATE SCHEMA IF NOT EXISTS metadata;

-- Transfer 模块 (数据传输任务、执行记录、字段映射)
CREATE SCHEMA IF NOT EXISTS transfer;

-- Develop 模块 (SQL脚本、空间任务、执行结果)
CREATE SCHEMA IF NOT EXISTS develop;

-- Orchestrator 模块 (工作流编排)
CREATE SCHEMA IF NOT EXISTS orchestrator;

-- Gateway 模块 (API访问日志)
CREATE SCHEMA IF NOT EXISTS gateway;

-- Copilot 模块 (AI对话、会话管理)
CREATE SCHEMA IF NOT EXISTS copilot;

-- Model 模块 (数据标准与建模)
CREATE SCHEMA IF NOT EXISTS model;

-- Common 模块 (跨模块共享的数据表：统一执行记录表等)
CREATE SCHEMA IF NOT EXISTS common;

-- Asset 模块 (数据资产管理：编目、申请、授权、评价)
CREATE SCHEMA IF NOT EXISTS asset;

-- ==================== 创建更新时间戳触发器函数 ====================
-- 此函数用于自动更新 updated_at 字段
-- 各模块的 AutoMigrate 可能会创建表，但不会创建触发器
-- 因此在这里预先定义触发器函数，模块代码可以引用
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = CURRENT_TIMESTAMP;
    RETURN NEW;
END;
$$ language 'plpgsql';

-- ==================== 注释 ====================
COMMENT ON SCHEMA system IS 'System 模块：用户、租户、引擎、应用、API密钥等核心系统数据';
COMMENT ON SCHEMA manager IS 'Manager 模块：搜索历史、向量嵌入、快显缓存等数据管理功能';
COMMENT ON SCHEMA metadata IS 'Meta 模块：元数据节点、元数据条目、扫描任务等元数据服务';
COMMENT ON SCHEMA transfer IS 'Transfer 模块：数据传输任务、执行记录、字段映射等';
COMMENT ON SCHEMA develop IS 'Develop 模块：SQL脚本、空间任务、执行结果等开发工作台功能';
COMMENT ON SCHEMA orchestrator IS 'Orchestrator 模块：工作流编排相关表';
COMMENT ON SCHEMA gateway IS 'Gateway 模块：API访问日志等网关功能';
COMMENT ON SCHEMA copilot IS 'Copilot 模块：AI对话、会话管理、上下文处理';
COMMENT ON SCHEMA model IS 'Model 模块：数据标准、数据建模、指标管理';
COMMENT ON SCHEMA common IS 'Common 模块：跨模块共享的数据表（统一执行记录表等）';
COMMENT ON SCHEMA asset IS 'Asset 模块：数据资产编目、申请与授权管理';

COMMENT ON FUNCTION update_updated_at_column() IS '触发器函数：自动更新 updated_at 时间戳';

-- ==================== 表创建说明 ====================
-- 所有表由各模块的 GORM AutoMigrate 自动创建和迁移
--
-- 各模块数据库初始化位置:
-- - System:      system/backend/internal/repository/database.go
-- - Manager:     manager/backend/internal/repository/database.go
-- - Meta:        meta/backend/internal/repository/database.go
-- - Transfer:    transfer/backend/cmd/server/main.go
-- - Develop:     develop/backend/internal/repository/database.go
-- - Service:     service/backend/internal/repository/database.go
-- - Orchestrator: (待实现)
-- - Gateway:     gateway/backend/internal/repository/database.go
-- - Copilot:     copilot/backend/database.py (SQLAlchemy)
--
-- 如需查看表结构定义，请参考各模块的 internal/models/ 目录
-- 例如:
-- - System 模块表: system/backend/internal/models/*.go
-- - Manager 模块表: manager/backend/internal/models/*.go
-- - Meta 模块表: meta/backend/internal/models/*.go
--
-- 旧版本 CREATE TABLE 语句已备份至: init-postgresql.sql.bak.<date>

COMMIT;
