package models

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"time"
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
	bytes, ok := value.([]byte)
	if !ok {
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
	ID            int64     `gorm:"primaryKey"                                          json:"id"`
	TenantID      int64     `gorm:"not null;index"                                      json:"tenant_id"`
	Name          string    `gorm:"size:100;not null"                                   json:"name"`
	Code          string    `gorm:"size:50;not null;uniqueIndex:uidx_type_code_tenant"  json:"code"`
	SourceModule  string    `gorm:"size:50"                                             json:"source_module"`   // meta/service/standard/develop/manual
	AuthHandler   string    `gorm:"size:100"                                            json:"auth_handler"`    // soft/token
	EntryType     string    `gorm:"size:50"                                             json:"entry_type"`      // preview/link/iframe/token
	DiscoveryPath string    `gorm:"size:255"                                            json:"discovery_path"`  // 可发现资产的 API 路径，如 /api/meta/assets/discoverable
	IconURL       string    `gorm:"size:500"                                            json:"icon_url"`
	Description   string    `gorm:"size:500"                                            json:"description"`
	Enabled       bool      `gorm:"default:true"                                        json:"enabled"`
	SortOrder     int       `gorm:"default:0"                                           json:"sort_order"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
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
	ID              int64      `gorm:"primaryKey"                                json:"id"`
	TenantID        int64      `gorm:"not null;index"                            json:"tenant_id"`
	Name            string     `gorm:"size:500;not null"                         json:"name"`
	Description     string     `gorm:"size:2000"                                 json:"description"`
	TypeID          int64      `gorm:"not null;index"                            json:"type_id"`
	CatalogID       *int64     `gorm:"index"                                     json:"catalog_id,omitempty"`
	Tags            JSONBArray `gorm:"type:jsonb;default:'[]'"                   json:"tags"`
	Status          string     `gorm:"size:50;not null;default:'draft';index"    json:"status"` // draft/published/offline
	OwnerID         int64      `gorm:"not null;index"                            json:"owner_id"`
	SourceModule    string     `gorm:"size:50"                                   json:"source_module"`    // 来源模块
	SourceReference string     `gorm:"size:500"                                  json:"source_reference"` // 来源引用
	Fingerprint     string     `gorm:"size:64;uniqueIndex:uidx_asset_fingerprint_tenant,where:fingerprint != ''" json:"fingerprint"` // 去重指纹
	SourceAvailable bool       `gorm:"not null;default:true"                     json:"source_available"` // 来源资源是否仍然存在
	PublishedAt     *time.Time `json:"published_at,omitempty"`
	CreatedBy       int64      `gorm:"not null"                                  json:"created_by"`
	UpdatedBy       *int64     `json:"updated_by,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

func (Asset) TableName() string { return "asset.assets" }

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

// Authorization 授权记录（审批通过后自动生成）
type Authorization struct {
	ID            int64      `gorm:"primaryKey"              json:"id"`
	TenantID      int64      `gorm:"not null;index"          json:"tenant_id"`
	AssetID       int64      `gorm:"not null;index"          json:"asset_id"`
	ApplicationID *int64     `gorm:"index"                   json:"application_id,omitempty"` // 关联申请记录，可为空（管理员直接授权）
	UserID        int64      `gorm:"not null;index"          json:"user_id"`
	Credential    string     `gorm:"size:2000"               json:"credential"`              // 授权凭证（如 API Token），软性授权为空
	ExpiresAt     *time.Time `json:"expires_at,omitempty"`
	IsActive      bool       `gorm:"default:true;index"      json:"is_active"`
	RevokedAt     *time.Time `json:"revoked_at,omitempty"`
	RevokedBy     *int64     `json:"revoked_by,omitempty"`
	CreatedAt     time.Time  `json:"created_at"`
	UpdatedAt     time.Time  `json:"updated_at"`
}

func (Authorization) TableName() string { return "asset.authorizations" }

// Rating 资产评价记录（已授权用户可评价）
type Rating struct {
	ID        int64      `gorm:"primaryKey"              json:"id"`
	TenantID  int64      `gorm:"not null;index"          json:"tenant_id"`
	AssetID   int64      `gorm:"not null;index"          json:"asset_id"`
	UserID    int64      `gorm:"not null;index"          json:"user_id"`
	Score     float32    `gorm:"not null"                json:"score"`    // 1-5 分
	Comment   string     `gorm:"size:2000"               json:"comment"`
	Tags      JSONBArray `gorm:"type:jsonb;default:'[]'" json:"tags"`     // 反馈标签（如：数据质量问题/文档不清晰等）
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

func (Rating) TableName() string { return "asset.ratings" }
