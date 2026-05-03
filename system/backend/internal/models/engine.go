package models

import (
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type ConnectionInfo = commonModels.ConnectionInfo
type ScanConfig = commonModels.ScanConfig
type Engine = commonModels.Engine
type JSONString = commonModels.JSONString

type EngineCreateRequest struct {
	Name           string         `json:"name" binding:"required"` // 显示名称（中文或英文）
	EngineType     string         `json:"engine_type" binding:"required"`
	EngineCategory string         `json:"engine_category"` // 引擎分类：standard/extension
	ConnectionInfo ConnectionInfo `json:"connection_info" binding:"required"`
	Description    string         `json:"description"`
	Capabilities   *JSONString    `json:"capabilities"` // 能力声明JSON
	ScanConfig     *ScanConfig    `json:"scan_config"`  // 扫描配置（可选）
}

type EngineUpdateRequest struct {
	Name           *string         `json:"name"` // 显示名称
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	IsActive       *bool           `json:"is_active"`
	Capabilities   *JSONString     `json:"capabilities"` // 能力声明JSON
	ScanConfig     *ScanConfig     `json:"scan_config"`  // 扫描配置（可选）
}
