package models

import (
	"time"

	"gorm.io/datatypes"
)

// DataProfile stores the latest successful profile for one stable data item
// and one frozen profiling configuration.
type DataProfile struct {
	ID                 uint               `gorm:"primaryKey" json:"id"`
	TenantID           uint               `gorm:"not null;uniqueIndex:idx_data_profiles_current,priority:1" json:"tenant_id"`
	ItemFingerprint    string             `gorm:"size:64;not null;uniqueIndex:idx_data_profiles_current,priority:2;index:idx_data_profiles_tenant_item,priority:2" json:"item_fingerprint"`
	ItemID             *uint              `gorm:"index:idx_data_profiles_tenant_item,priority:3" json:"item_id,omitempty"`
	EngineID           uint               `gorm:"not null" json:"engine_id"`
	Locator            string             `gorm:"type:text;not null" json:"locator"`
	SourceVersion      string             `gorm:"size:64;not null" json:"source_version"`
	DependencySnapshot datatypes.JSON     `gorm:"type:jsonb;not null;default:'{}'" json:"dependency_snapshot"`
	ProfileMode        string             `gorm:"size:32;not null;uniqueIndex:idx_data_profiles_current,priority:3" json:"profile_mode"`
	ProfileConfigHash  string             `gorm:"size:64;not null;uniqueIndex:idx_data_profiles_current,priority:4" json:"profile_config_hash"`
	SchemaVersion      string             `gorm:"size:64;not null" json:"schema_version"`
	SampleMethod       string             `gorm:"size:64;not null" json:"sample_method"`
	SampleSize         int64              `gorm:"not null" json:"sample_size"`
	RowsScanned        int64              `gorm:"not null" json:"rows_scanned"`
	RowCount           *int64             `json:"row_count,omitempty"`
	RowCountExact      bool               `gorm:"not null" json:"row_count_exact"`
	FieldCount         int                `gorm:"not null" json:"field_count"`
	Truncated          bool               `gorm:"not null" json:"truncated"`
	Partial            bool               `gorm:"not null" json:"partial"`
	Observations       datatypes.JSON     `gorm:"type:jsonb;not null;default:'[]'" json:"observations"`
	LastExecutionID    string             `gorm:"size:36;not null;index" json:"last_execution_id"`
	ProfiledAt         time.Time          `gorm:"not null" json:"profiled_at"`
	CreatedAt          time.Time          `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt          time.Time          `gorm:"autoUpdateTime;not null" json:"updated_at"`
	Fields             []DataProfileField `gorm:"foreignKey:ProfileID;constraint:OnDelete:CASCADE" json:"fields,omitempty"`
}

func (DataProfile) TableName() string {
	return "manager.data_profiles"
}

// DataProfileField stores one field projection from the stable data profile
// contract. The JSON value is versioned by the parent SchemaVersion.
type DataProfileField struct {
	ID        uint           `gorm:"primaryKey" json:"id"`
	ProfileID uint           `gorm:"not null;uniqueIndex:idx_data_profile_fields_profile_name,priority:1;index:idx_data_profile_fields_profile_position,priority:1" json:"profile_id"`
	Position  int            `gorm:"not null;index:idx_data_profile_fields_profile_position,priority:2" json:"position"`
	Name      string         `gorm:"size:512;not null;uniqueIndex:idx_data_profile_fields_profile_name,priority:2" json:"name"`
	Type      string         `gorm:"size:64;not null" json:"type"`
	Status    string         `gorm:"size:32;not null" json:"status"`
	Profile   datatypes.JSON `gorm:"type:jsonb;not null" json:"profile"`
	CreatedAt time.Time      `gorm:"autoCreateTime;not null" json:"created_at"`
	UpdatedAt time.Time      `gorm:"autoUpdateTime;not null" json:"updated_at"`
}

func (DataProfileField) TableName() string {
	return "manager.data_profile_fields"
}
