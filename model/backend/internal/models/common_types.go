package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
)

// JSONB PostgreSQL JSONB 类型
type JSONB map[string]interface{}

func (j JSONB) Value() (driver.Value, error) {
	if j == nil {
		return nil, nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONB) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	switch v := value.(type) {
	case []byte:
		return json.Unmarshal(v, j)
	case string:
		return json.Unmarshal([]byte(v), j)
	}
	return fmt.Errorf("unsupported type: %T", value)
}

// VersionRequest 是已有主资源和聚合子资源删除、审批、重新打开的唯一版本请求。
type VersionRequest struct {
	Version int64 `json:"version" binding:"required,gt=0" minimum:"1"`
}

type VersionResponse struct {
	Version int64 `json:"version"`
}

// MaterializedTargetDecommissionRequest binds a destructive physical action to
// the current logical-table version and exact configured target.
type MaterializedTargetDecommissionRequest struct {
	Version             int64  `json:"version" binding:"required,gt=0" minimum:"1"`
	TargetParentLocator string `json:"target_parent_locator" binding:"required"`
	TargetName          string `json:"target_name" binding:"required"`
}

// EntityModelRevision 是 Tenant 实体模型集合的并发边界。
type EntityModelRevision struct {
	TenantID int64 `gorm:"primaryKey" json:"tenant_id"`
	Revision int64 `gorm:"not null;default:1" json:"revision"`
}

func (EntityModelRevision) TableName() string {
	return "model.entity_model_revisions"
}
