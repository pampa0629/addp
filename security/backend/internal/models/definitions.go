package models

import "time"

type SecurityClassification struct {
	ID          int64     `gorm:"primaryKey" json:"id,string"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_security_classifications_tenant_code" json:"tenant_id,string"`
	Code        string    `gorm:"size:80;not null;uniqueIndex:uq_security_classifications_tenant_code" json:"code"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	ParentID    *int64    `gorm:"index" json:"parent_id,omitempty,string"`
	SortOrder   int       `gorm:"not null;default:0" json:"sort_order"`
	Version     int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy   int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy   *int64    `json:"updated_by,omitempty,string"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (SecurityClassification) TableName() string { return "security.security_classifications" }

type SecurityGrade struct {
	ID          int64     `gorm:"primaryKey" json:"id,string"`
	TenantID    int64     `gorm:"not null;index;uniqueIndex:uq_security_grades_tenant_code" json:"tenant_id,string"`
	Code        string    `gorm:"size:80;not null;uniqueIndex:uq_security_grades_tenant_code" json:"code"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Description string    `gorm:"type:text" json:"description"`
	RiskOrder   int       `gorm:"not null" json:"risk_order"`
	Version     int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy   int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy   *int64    `json:"updated_by,omitempty,string"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (SecurityGrade) TableName() string { return "security.security_grades" }

type SensitiveDataType struct {
	ID                       int64     `gorm:"primaryKey" json:"id,string"`
	TenantID                 int64     `gorm:"not null;index;uniqueIndex:uq_sensitive_data_types_tenant_code" json:"tenant_id,string"`
	Code                     string    `gorm:"size:80;not null;uniqueIndex:uq_sensitive_data_types_tenant_code" json:"code"`
	Name                     string    `gorm:"size:200;not null" json:"name"`
	Description              string    `gorm:"type:text" json:"description"`
	SecurityClassificationID int64     `gorm:"not null;index" json:"security_classification_id,string"`
	DefaultSecurityGradeID   int64     `gorm:"not null;index" json:"default_security_grade_id,string"`
	ProtectionThreshold      float64   `gorm:"not null;default:0.8" json:"protection_threshold"`
	Version                  int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy                int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy                *int64    `json:"updated_by,omitempty,string"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func (SensitiveDataType) TableName() string { return "security.sensitive_data_types" }

type ProtectionBaseline struct {
	ID                  int64     `gorm:"primaryKey" json:"id,string"`
	TenantID            int64     `gorm:"not null;index;uniqueIndex:uq_protection_baselines_target" json:"tenant_id,string"`
	SensitiveDataTypeID int64     `gorm:"not null;uniqueIndex:uq_protection_baselines_target" json:"sensitive_data_type_id,string"`
	SecurityGradeID     int64     `gorm:"not null;uniqueIndex:uq_protection_baselines_target" json:"security_grade_id,string"`
	Effect              string    `gorm:"size:20;not null" json:"effect"`
	Algorithm           string    `gorm:"size:80" json:"algorithm"`
	KeepPrefix          int       `gorm:"not null;default:0" json:"keep_prefix"`
	KeepSuffix          int       `gorm:"not null;default:0" json:"keep_suffix"`
	InvalidValueEffect  string    `gorm:"size:20;not null;default:suppress" json:"invalid_value_effect"`
	Enabled             bool      `gorm:"not null;default:true" json:"enabled"`
	Version             int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy           int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy           *int64    `json:"updated_by,omitempty,string"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (ProtectionBaseline) TableName() string { return "security.protection_baselines" }

type DefinitionRequest struct {
	Code        string `json:"code" binding:"required"`
	Name        string `json:"name" binding:"required"`
	Description string `json:"description"`
	ParentID    *int64 `json:"parent_id,omitempty"`
	SortOrder   int    `json:"sort_order"`
	RiskOrder   int    `json:"risk_order"`
	Version     int64  `json:"version"`
}

type SensitiveDataTypeRequest struct {
	Code                     string  `json:"code" binding:"required"`
	Name                     string  `json:"name" binding:"required"`
	Description              string  `json:"description"`
	SecurityClassificationID int64   `json:"security_classification_id" binding:"required"`
	DefaultSecurityGradeID   int64   `json:"default_security_grade_id" binding:"required"`
	ProtectionThreshold      float64 `json:"protection_threshold"`
	Version                  int64   `json:"version"`
}

type ProtectionBaselineRequest struct {
	SensitiveDataTypeID int64  `json:"sensitive_data_type_id" binding:"required"`
	SecurityGradeID     int64  `json:"security_grade_id" binding:"required"`
	Effect              string `json:"effect" binding:"required"`
	Algorithm           string `json:"algorithm"`
	KeepPrefix          int    `json:"keep_prefix"`
	KeepSuffix          int    `json:"keep_suffix"`
	InvalidValueEffect  string `json:"invalid_value_effect"`
	Enabled             *bool  `json:"enabled"`
	Version             int64  `json:"version"`
}

type DeleteProtectionBaselineRequest struct {
	Version int64 `json:"version" binding:"required"`
}
