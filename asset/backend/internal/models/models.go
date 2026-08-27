package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
)

// JSONBMap 用于 GORM 存储 JSONB 字段（对象类型）
type JSONBMap map[string]interface{}

func (j JSONBMap) Value() (driver.Value, error) {
	if j == nil {
		return "{}", nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONBMap) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	var bytes []byte
	switch v := value.(type) {
	case []byte:
		bytes = v
	case string:
		bytes = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into JSONBMap", value)
	}
	return json.Unmarshal(bytes, j)
}

// JSONBArray 用于 GORM 存储 JSONB 字段（数组类型）
type JSONBArray []string

func (j JSONBArray) Value() (driver.Value, error) {
	if j == nil {
		return "[]", nil
	}
	b, err := json.Marshal(j)
	return string(b), err
}

func (j *JSONBArray) Scan(value interface{}) error {
	if value == nil {
		*j = nil
		return nil
	}
	var data []byte
	switch v := value.(type) {
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		return fmt.Errorf("cannot scan type %T into JSONBArray", value)
	}
	return json.Unmarshal(data, j)
}

// TypeDefinition 资产类型注册表
// 每种资产类型（数据集、数据服务、指标等）在此表注册
type TypeDefinition struct {
	ID          int64     `gorm:"primaryKey"                                          json:"id"`
	TenantID    int64     `gorm:"not null;index"                                      json:"tenant_id"`
	Name        string    `gorm:"size:100;not null"                                   json:"name"`
	Code        string    `gorm:"size:50;not null;uniqueIndex:uidx_type_code_tenant"  json:"code"`
	IconURL     string    `gorm:"size:500"                                            json:"icon_url"`
	Description string    `gorm:"size:500"                                            json:"description"`
	Enabled     bool      `gorm:"default:true"                                        json:"enabled"`
	SortOrder   int       `gorm:"default:0"                                           json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (TypeDefinition) TableName() string { return "asset.type_definitions" }

// TypeFieldSchema 各资产类型的扩展字段定义（JSON Schema 描述）
type TypeFieldSchema struct {
	ID        int64    `gorm:"primaryKey"      json:"id"`
	TypeID    int64    `gorm:"not null;index"  json:"type_id"`
	FieldKey  string   `gorm:"size:100;not null" json:"field_key"`
	FieldName string   `gorm:"size:200;not null" json:"field_name"`
	FieldType string   `gorm:"size:50;not null"  json:"field_type"` // string/number/boolean/json/date
	Required  bool     `gorm:"default:false"   json:"required"`
	Schema    JSONBMap `gorm:"type:jsonb;default:'{}'" json:"schema"` // JSON Schema 约束
	SortOrder int      `gorm:"default:0"       json:"sort_order"`

	CreatedAt time.Time `json:"created_at"`
}

func (TypeFieldSchema) TableName() string { return "asset.type_field_schemas" }

// Catalog 资产目录树（多级层级结构，资产单一归属）
// 同级目录名称唯一性由数据库 partial index 保证（见 main.go 迁移 SQL）
type Catalog struct {
	ID          int64     `gorm:"primaryKey"        json:"id"`
	TenantID    int64     `gorm:"not null;index"    json:"tenant_id"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	ParentID    *int64    `gorm:"index"             json:"parent_id,omitempty"`
	SortOrder   int       `gorm:"default:0"         json:"sort_order"`
	Description string    `gorm:"size:500"          json:"description"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Catalog) TableName() string { return "asset.catalogs" }

// Asset 资产主表
type Asset struct {
	ID          int64      `gorm:"primaryKey"                                json:"id"`
	TenantID    int64      `gorm:"not null;index"                            json:"tenant_id"`
	Name        string     `gorm:"size:500;not null"                         json:"name"`
	Description string     `gorm:"size:2000"                                 json:"description"`
	TypeID      int64      `gorm:"not null;index"                            json:"type_id"`
	CatalogID   *int64     `gorm:"index"                                     json:"catalog_id,omitempty"`
	Tags        JSONBArray `gorm:"type:jsonb;default:'[]'"                   json:"tags"`
	Status      string     `gorm:"size:50;not null;default:'draft';index"    json:"status"` // draft/published/offline
	OwnerID     int64      `gorm:"not null;index"                            json:"owner_id"`
	Version     int64      `gorm:"not null;default:1"                        json:"version"`
	PublishedAt *time.Time `json:"published_at,omitempty"`
	CreatedBy   int64      `gorm:"not null"                                  json:"created_by"`
	UpdatedBy   *int64     `json:"updated_by,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Asset) TableName() string { return "asset.assets" }

const (
	AssetComponentRolePrimary    = "primary"
	AssetComponentRoleSupporting = "supporting"
)

// AssetComponent is an Asset-owned composition reference to a CatalogEntry.
// Catalog remains the owner of entry validity, source, semantics, and responsibility.
type AssetComponent struct {
	ID             int64     `gorm:"primaryKey" json:"id"`
	TenantID       int64     `gorm:"not null;index" json:"-"`
	AssetID        int64     `gorm:"not null;index" json:"asset_id"`
	CatalogEntryID uuid.UUID `gorm:"type:uuid;not null" json:"catalog_entry_id"`
	Role           string    `gorm:"size:16;not null" json:"role"`
	SortOrder      int       `gorm:"not null;default:0" json:"sort_order"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (AssetComponent) TableName() string { return "asset.asset_components" }

// AssetExtField 资产扩展字段值（按类型动态存储）
type AssetExtField struct {
	ID        int64     `gorm:"primaryKey"               json:"id"`
	AssetID   int64     `gorm:"not null;index"           json:"asset_id"`
	FieldKey  string    `gorm:"size:100;not null"        json:"field_key"`
	Value     JSONBMap  `gorm:"type:jsonb"               json:"value"` // 统一用 JSONB 存储任意类型
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (AssetExtField) TableName() string { return "asset.asset_ext_fields" }

// Application 资产申请记录（消费者向资产管理员申请使用权）
type Application struct {
	ID          int64      `gorm:"primaryKey"                                      json:"id"`
	TenantID    int64      `gorm:"not null;index"                                  json:"tenant_id"`
	AssetID     int64      `gorm:"not null;index"                                  json:"asset_id"`
	ApplicantID int64      `gorm:"not null;index"                                  json:"applicant_id"`
	Reason      string     `gorm:"size:2000"                                       json:"reason"`
	DurationDay int        `gorm:"default:30"                                      json:"duration_day"` // 申请使用时长（天）
	Status      string     `gorm:"size:50;not null;default:'pending';index"        json:"status"`       // pending/approved/rejected/revoked
	ReviewerID  *int64     `json:"reviewer_id,omitempty"`
	ReviewNote  string     `gorm:"size:2000"                                       json:"review_note"`
	ReviewedAt  *time.Time `json:"reviewed_at,omitempty"`
	ExpiresAt   *time.Time `json:"expires_at,omitempty"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
}

func (Application) TableName() string { return "asset.applications" }

const (
	AuthorizationStatusPending           = "pending"
	AuthorizationStatusEffective         = "effective"
	AuthorizationStatusRevocationPending = "revocation_pending"
	AuthorizationStatusRevoked           = "revoked"
)

// Authorization records Asset-owned approval fulfillment state. The owner module
// remains the only source of the final runtime access rule.
type Authorization struct {
	ID                   int64      `gorm:"primaryKey" json:"id"`
	TenantID             int64      `gorm:"not null;index" json:"tenant_id"`
	AssetID              int64      `gorm:"not null;index" json:"asset_id"`
	ApplicationID        *int64     `gorm:"uniqueIndex" json:"application_id"`
	UserID               int64      `gorm:"not null;index" json:"user_id"`
	Status               string     `gorm:"size:32;not null;default:'pending';index" json:"status"`
	TargetModule         string     `gorm:"size:64;not null;default:''" json:"target_module,omitempty"`
	TargetResourceType   string     `gorm:"size:64;not null;default:''" json:"target_resource_type,omitempty"`
	TargetResourceID     string     `gorm:"size:128;not null;default:''" json:"target_resource_id,omitempty"`
	ExpiresAt            *time.Time `json:"expires_at,omitempty"`
	FulfillmentAttempt   int        `gorm:"not null;default:0" json:"fulfillment_attempt"`
	FulfillmentLastError string     `gorm:"size:2000;not null;default:''" json:"-"`
	NextAttemptAt        *time.Time `gorm:"index" json:"-"`
	FulfilledAt          *time.Time `json:"fulfilled_at,omitempty"`
	RevokedAt            *time.Time `json:"revoked_at,omitempty"`
	RevokedBy            *int64     `json:"revoked_by,omitempty"`
	CreatedAt            time.Time  `json:"created_at"`
	UpdatedAt            time.Time  `json:"updated_at"`
}

func (Authorization) TableName() string { return "asset.authorizations" }

// Rating 资产评价记录（已授权用户可评价）
// 每用户每资产只能有一条评价记录（通过 uidx_rating_user_asset 唯一索引保证）
type Rating struct {
	ID        int64      `gorm:"primaryKey"                                          json:"id"`
	TenantID  int64      `gorm:"not null;index"                                      json:"tenant_id"`
	AssetID   int64      `gorm:"not null;uniqueIndex:uidx_rating_user_asset"         json:"asset_id"`
	UserID    int64      `gorm:"not null;uniqueIndex:uidx_rating_user_asset"         json:"user_id"`
	Score     float32    `gorm:"not null"                                            json:"score"` // 1-5 分
	Comment   string     `gorm:"size:2000"                                           json:"comment"`
	Tags      JSONBArray `gorm:"type:jsonb;default:'[]'"                             json:"tags"`       // 反馈标签（问题反馈时填写，如：数据质量问题/文档不清晰）
	IsHandled bool       `gorm:"default:false"                                       json:"is_handled"` // 管理员是否已处理问题反馈
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (Rating) TableName() string { return "asset.ratings" }
