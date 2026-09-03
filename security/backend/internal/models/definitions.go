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
	Version                  int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy                int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy                *int64    `json:"updated_by,omitempty,string"`
	CreatedAt                time.Time `json:"created_at"`
	UpdatedAt                time.Time `json:"updated_at"`
}

func (SensitiveDataType) TableName() string { return "security.sensitive_data_types" }

// Detector binds one trusted, platform-installed detector capability to one
// tenant-owned sensitive data type. Executable detector logic never lives in
// this row.
type Detector struct {
	ID                  int64     `gorm:"primaryKey" json:"id,string"`
	TenantID            int64     `gorm:"not null;index;uniqueIndex:uq_security_detectors_tenant_capability" json:"tenant_id,string"`
	CapabilityKey       string    `gorm:"size:160;not null;uniqueIndex:uq_security_detectors_tenant_capability" json:"capability_key"`
	SensitiveDataTypeID int64     `gorm:"not null;index" json:"sensitive_data_type_id,string"`
	ConfidenceThreshold float64   `gorm:"not null;default:0.9" json:"confidence_threshold"`
	Enabled             bool      `gorm:"not null;default:true" json:"enabled"`
	Version             int64     `gorm:"not null;default:1" json:"version,string"`
	CreatedBy           int64     `gorm:"not null" json:"created_by,string"`
	UpdatedBy           *int64    `json:"updated_by,omitempty,string"`
	CreatedAt           time.Time `json:"created_at"`
	UpdatedAt           time.Time `json:"updated_at"`
}

func (Detector) TableName() string { return "security.detectors" }

type DetectorCapability struct {
	Key                  string   `json:"key"`
	Code                 string   `json:"code"`
	Version              string   `json:"version"`
	NameI18nKey          string   `json:"name_i18n_key"`
	DescriptionI18nKey   string   `json:"description_i18n_key"`
	MethodI18nKey        string   `json:"method_i18n_key"`
	PrivacyI18nKey       string   `json:"privacy_i18n_key"`
	LimitationsI18nKey   string   `json:"limitations_i18n_key"`
	TargetKind           string   `json:"target_kind"`
	EvidenceSource       string   `json:"evidence_source"`
	SupportedItemTypes   []string `json:"supported_item_types"`
	SupportedFieldTypes  []string `json:"supported_field_types"`
	RecommendedThreshold float64  `json:"recommended_threshold"`
}

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

type DefinitionProfile struct {
	Key                 string `json:"key"`
	NameI18nKey         string `json:"name_i18n_key"`
	DescriptionI18nKey  string `json:"description_i18n_key"`
	ClassificationCount int    `json:"classification_count"`
	GradeCount          int    `json:"grade_count"`
}

type DefinitionProfileApplicationRequest struct {
	ProfileKey string `json:"profile_key" binding:"required"`
}

type DefinitionProfileApplication struct {
	ProfileKey             string `json:"profile_key"`
	CreatedClassifications int    `json:"created_classifications"`
	CreatedGrades          int    `json:"created_grades"`
}

type SensitiveDataTypeRequest struct {
	Code                     string `json:"code" binding:"required"`
	Name                     string `json:"name" binding:"required"`
	Description              string `json:"description"`
	SecurityClassificationID int64  `json:"security_classification_id" binding:"required"`
	DefaultSecurityGradeID   int64  `json:"default_security_grade_id" binding:"required"`
	Version                  int64  `json:"version"`
}

type DetectorRequest struct {
	CapabilityKey       string  `json:"capability_key" binding:"required"`
	SensitiveDataTypeID int64   `json:"sensitive_data_type_id" binding:"required"`
	ConfidenceThreshold float64 `json:"confidence_threshold" binding:"required"`
	Enabled             *bool   `json:"enabled"`
	Version             int64   `json:"version"`
}

type DeleteDetectorRequest struct {
	Version int64 `json:"version" binding:"required"`
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
