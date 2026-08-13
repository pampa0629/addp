package models

import "time"

// Entity 业务实体
type Entity struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID    int64     `gorm:"not null;index" json:"tenant_id"`
	DomainID    *int64    `gorm:"index" json:"domain_id,omitempty"`
	Name        string    `gorm:"size:200;not null" json:"name"`
	Code        string    `gorm:"size:100;not null" json:"code"`
	Description string    `gorm:"type:text" json:"description"`
	Status      string    `gorm:"size:20;default:'draft'" json:"status"` // draft/approved
	CreatedBy   int64     `gorm:"not null" json:"created_by"`
	UpdatedBy   *int64    `json:"updated_by,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (Entity) TableName() string {
	return "model.entities"
}

// EntityAttribute 实体属性
type EntityAttribute struct {
	ID          int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	EntityID    int64     `gorm:"not null;index" json:"entity_id"`
	ElementID   *int64    `json:"element_id,omitempty"` // 引用数据元（可选）
	Name        string    `gorm:"size:200;not null" json:"name"`
	ColumnName  string    `gorm:"size:200;not null" json:"column_name"`
	DataType    string    `gorm:"size:50;not null" json:"data_type"`
	IsPK        bool      `gorm:"default:false" json:"is_pk"`
	Nullable    bool      `gorm:"default:true" json:"nullable"`
	Description string    `gorm:"type:text" json:"description"`
	SortOrder   int       `gorm:"default:0" json:"sort_order"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (EntityAttribute) TableName() string {
	return "model.entity_attributes"
}

// EntityRelation 实体关系
type EntityRelation struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID     int64     `gorm:"not null;index" json:"tenant_id"`
	SourceEntity int64     `gorm:"not null" json:"source_entity"`
	TargetEntity int64     `gorm:"not null" json:"target_entity"`
	RelationType string    `gorm:"size:20;not null" json:"relation_type"` // one_to_one/one_to_many/many_to_many
	Name         string    `gorm:"size:200" json:"name"`
	Description  string    `gorm:"type:text" json:"description"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (EntityRelation) TableName() string {
	return "model.entity_relations"
}

// CreateEntityRequest 创建实体请求
type CreateEntityRequest struct {
	DomainID    *int64 `json:"domain_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	Code        string `json:"code" binding:"required,max=100" maxLength:"100"`
	Description string `json:"description"`
}

// UpdateEntityRequest 更新实体请求
type UpdateEntityRequest struct {
	DomainID    *int64 `json:"domain_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	Description string `json:"description"`
}

// CreateEntityAttributeRequest 创建实体属性请求
type CreateEntityAttributeRequest struct {
	ElementID   *int64 `json:"element_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	ColumnName  string `json:"column_name" binding:"required,max=200" maxLength:"200"`
	DataType    string `json:"data_type" binding:"required,oneof=string int bigint float decimal date datetime bool json text geometry" enums:"string,int,bigint,float,decimal,date,datetime,bool,json,text,geometry"`
	IsPK        bool   `json:"is_pk"`
	Nullable    bool   `json:"nullable"`
	Description string `json:"description"`
	SortOrder   int    `json:"sort_order" binding:"gte=0" minimum:"0"`
}

// UpdateEntityAttributeRequest 更新实体属性请求
type UpdateEntityAttributeRequest struct {
	ElementID   *int64 `json:"element_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	Name        string `json:"name" binding:"required,max=200" maxLength:"200"`
	ColumnName  string `json:"column_name" binding:"required,max=200" maxLength:"200"`
	DataType    string `json:"data_type" binding:"required,oneof=string int bigint float decimal date datetime bool json text geometry" enums:"string,int,bigint,float,decimal,date,datetime,bool,json,text,geometry"`
	IsPK        *bool  `json:"is_pk" binding:"required"`
	Nullable    *bool  `json:"nullable" binding:"required"`
	Description string `json:"description"`
	SortOrder   *int   `json:"sort_order" binding:"required,gte=0" minimum:"0"`
}

// CreateEntityRelationRequest 创建实体关系请求
type CreateEntityRelationRequest struct {
	SourceEntity int64  `json:"source_entity" binding:"required,gt=0" minimum:"1"`
	TargetEntity int64  `json:"target_entity" binding:"required,gt=0" minimum:"1"`
	RelationType string `json:"relation_type" binding:"required,oneof=one_to_one one_to_many many_to_many"`
	Name         string `json:"name" binding:"max=200" maxLength:"200"`
	Description  string `json:"description"`
}

// UpdateEntityRelationRequest 更新实体关系请求
type UpdateEntityRelationRequest struct {
	RelationType string `json:"relation_type" binding:"required,oneof=one_to_one one_to_many many_to_many"`
	Name         string `json:"name" binding:"max=200" maxLength:"200"`
	Description  string `json:"description"`
}

// MermaidImportRequest Mermaid导入请求
type MermaidImportRequest struct {
	MermaidCode string `json:"mermaid_code" binding:"required"`
}

// MermaidImportResult Mermaid导入结果
type MermaidImportResult struct {
	CreatedEntities  int `json:"created_entities"`
	CreatedRelations int `json:"created_relations"`
}
