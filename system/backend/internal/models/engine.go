package models

import (
	commonModels "github.com/addp/common/models"
)

// 直接使用 Common 模块的类型，避免重复定义
type ConnectionInfo = commonModels.ConnectionInfo
type Engine = commonModels.Engine
type EngineRuntimeDescriptor = commonModels.EngineRuntimeDescriptor
type EngineRuntimeEndpoint = commonModels.EngineRuntimeEndpoint
type JSONString = commonModels.JSONString

const (
	EngineLifecycleActive   = commonModels.EngineLifecycleActive
	EngineLifecycleDisabled = commonModels.EngineLifecycleDisabled
	EngineLifecycleDeleting = commonModels.EngineLifecycleDeleting

	ExternalArtifactPolicyDelete  = commonModels.ExternalArtifactPolicyDelete
	ExternalArtifactPolicyAbandon = commonModels.ExternalArtifactPolicyAbandon
)

type EngineCreateRequest struct {
	Name           string         `json:"name" binding:"required"` // 显示名称（中文或英文）
	EngineType     string         `json:"engine_type" binding:"required"`
	EngineOrigin   string         `json:"engine_origin"` // 引擎来源：general/extension
	ConnectionInfo ConnectionInfo `json:"connection_info" binding:"required"`
	Description    string         `json:"description"`
	Capabilities   *JSONString    `json:"capabilities"` // 能力声明JSON
}

type EngineUpdateRequest struct {
	Name           *string         `json:"name"` // 显示名称
	ConnectionInfo *ConnectionInfo `json:"connection_info"`
	Description    *string         `json:"description"`
	LifecycleState *string         `json:"lifecycle_state"`
	Capabilities   *JSONString     `json:"capabilities"` // 能力声明JSON
}

type EngineDeleteRequest struct {
	AssessmentID           string `json:"assessment_id" binding:"required"`
	ConfirmationToken      string `json:"confirmation_token" binding:"required"`
	ExternalArtifactPolicy string `json:"external_artifact_policy"`
}

type EngineDeletionAssessmentRequest struct {
	ExternalArtifactPolicy string `json:"external_artifact_policy"`
}

type EngineDeletionAssessmentResponse struct {
	AssessmentID string `json:"assessment_id"`
}
