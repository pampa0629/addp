package models

import (
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type ConnectionInfo = commonModels.ConnectionInfo
type ScanConfig = commonModels.ScanConfig
type Engine = commonModels.Engine

type EngineCreateRequest struct {
	Name           string         `json:"name" binding:"required"`
	DisplayName    string         `json:"display_name"` // 中文显示名称（可选，未提供时默认等于 name）
	EngineType   string         `json:"engine_type" binding:"required"`
	ConnectionInfo ConnectionInfo `json:"connection_info" binding:"required"`
	Description    string         `json:"description"`
	ScanConfig     *ScanConfig    `json:"scan_config"` // 扫描配置（可选）
}

type EngineUpdateRequest struct {
	Name           *string         `json:"name"`
	DisplayName    *string         `json:"display_name"` // 支持更新显示名称
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	ScanConfig     *ScanConfig     `json:"scan_config"` // 扫描配置（可选）
}