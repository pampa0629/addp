package models

import "time"

// LogicalTable 逻辑表
type LogicalTable struct {
	ID               int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID         int64     `gorm:"not null;index" json:"tenant_id"`
	DomainID         *int64    `gorm:"index" json:"domain_id,omitempty"`
	EntityID         *int64    `json:"entity_id,omitempty"` // 关联实体（可选）
	Name             string    `gorm:"size:200;not null" json:"name"`
	Code             string    `gorm:"size:200;not null" json:"code"` // 英文表名（物化时使用）
	Description      string    `gorm:"type:text" json:"description"`
	TableType        string    `gorm:"size:30;not null" json:"table_type"`                // 建模角色：entity/fact/dimension
	Layer            string    `gorm:"size:20" json:"layer"`                              // ods/dwd/dws/ads
	Status           string    `gorm:"size:20;default:'draft'" json:"status"`             // draft/approved
	GrainDescription string    `gorm:"type:text" json:"grain_description"`                // 仅 fact 表：粒度声明（如"每行代表一笔支付事务"）
	SCDType          int       `gorm:"default:0" json:"scd_type"`                         // 仅 dimension 表：缓慢变化维类型 0=静态/1=覆盖/2=拉链/3=混合
	Materialization  JSONB     `gorm:"type:jsonb;serializer:json" json:"materialization"` // 物化配置
	CreatedBy        int64     `gorm:"not null" json:"created_by"`
	UpdatedBy        *int64    `json:"updated_by,omitempty"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func (LogicalTable) TableName() string {
	return "model.logical_tables"
}

// LogicalField 逻辑表字段
type LogicalField struct {
	ID           int64  `gorm:"primaryKey;autoIncrement" json:"id"`
	TableID      int64  `gorm:"not null;index" json:"table_id"`
	ElementID    *int64 `json:"element_id,omitempty"`                 // 引用数据元（可选）
	Name         string `gorm:"size:200;not null" json:"name"`        // 字段显示名
	ColumnName   string `gorm:"size:200;not null" json:"column_name"` // 物理列名
	DataType     string `gorm:"size:50;not null" json:"data_type"`    // string/int/bigint/float/decimal/date/datetime/bool/json/text
	Length       *int   `json:"length,omitempty"`
	Nullable     bool   `gorm:"default:true" json:"nullable"`
	IsPK         bool   `gorm:"default:false" json:"is_pk"`
	IsPartition  bool   `gorm:"default:false" json:"is_partition"` // 是否分区字段
	DefaultValue string `gorm:"type:text" json:"default_value"`
	Description  string `gorm:"type:text" json:"description"`
	SortOrder    int    `gorm:"default:0" json:"sort_order"`
	FieldRole    string `gorm:"size:30;default:'regular'" json:"field_role"`
	// 枚举：regular / measure_additive / measure_semi / measure_non / dimension_fk / degenerate_dim
	HierarchyID    *int64    `json:"hierarchy_id,omitempty"`    // → standard.dimension_hierarchies（无 DB FK 约束）
	HierarchyLevel *int      `json:"hierarchy_level,omitempty"` // 在层级中对应的层次编号
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (LogicalField) TableName() string {
	return "model.logical_fields"
}

// TableRelation 逻辑表间关系
type TableRelation struct {
	ID           int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID     int64     `gorm:"not null;index" json:"tenant_id"`
	SourceTable  int64     `gorm:"not null" json:"source_table"`
	SourceField  int64     `gorm:"not null" json:"source_field"`
	TargetTable  int64     `gorm:"not null" json:"target_table"`
	TargetField  int64     `gorm:"not null" json:"target_field"`
	RelationType string    `gorm:"size:20;not null" json:"relation_type"` // fk/join
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (TableRelation) TableName() string {
	return "model.table_relations"
}

// CreateLogicalTableRequest 创建逻辑表请求
type CreateLogicalTableRequest struct {
	DomainID         *int64                 `json:"domain_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	EntityID         *int64                 `json:"entity_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Name             string                 `json:"name" binding:"required,max=200" maxLength:"200"`
	Code             string                 `json:"code" binding:"required,max=200" maxLength:"200"`
	Description      string                 `json:"description"`
	TableType        string                 `json:"table_type" binding:"required,oneof=entity fact dimension" enums:"entity,fact,dimension"`
	Layer            string                 `json:"layer" binding:"required,max=20" maxLength:"20"`
	GrainDescription string                 `json:"grain_description"`
	SCDType          int                    `json:"scd_type"`
	Materialization  map[string]interface{} `json:"materialization"`
}

// UpdateLogicalTableRequest 更新逻辑表请求
type UpdateLogicalTableRequest struct {
	DomainID         *int64                 `json:"domain_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	EntityID         *int64                 `json:"entity_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	Name             string                 `json:"name" binding:"required,max=200" maxLength:"200"`
	Description      string                 `json:"description"`
	TableType        string                 `json:"table_type" binding:"required,oneof=entity fact dimension" enums:"entity,fact,dimension"`
	Layer            string                 `json:"layer" binding:"required,max=20" maxLength:"20"`
	GrainDescription string                 `json:"grain_description"`
	SCDType          *int                   `json:"scd_type" binding:"required"`
	Materialization  map[string]interface{} `json:"materialization" binding:"required"`
}

// PreviewLogicalTableDDLRequest 使用当前页面中的物化配置生成 DDL，不持久化配置。
type PreviewLogicalTableDDLRequest struct {
	Materialization map[string]interface{} `json:"materialization" binding:"required"`
}

// CreateTableRelationRequest 创建逻辑表关联请求
type CreateTableRelationRequest struct {
	TargetTable  int64  `json:"target_table" binding:"required,gt=0" minimum:"1"`
	SourceField  int64  `json:"source_field" binding:"required,gt=0" minimum:"1"`
	TargetField  int64  `json:"target_field" binding:"required,gt=0" minimum:"1"`
	RelationType string `json:"relation_type" binding:"omitempty,oneof=fk join" enums:"fk,join"` // fk/join，默认 fk
}

// CreateLogicalFieldRequest 创建逻辑表字段请求
type CreateLogicalFieldRequest struct {
	ElementID      *int64 `json:"element_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Name           string `json:"name" binding:"required,max=200" maxLength:"200"`
	ColumnName     string `json:"column_name" binding:"required,max=200" maxLength:"200"`
	DataType       string `json:"data_type" binding:"required,oneof=string int bigint float decimal date datetime bool json text geometry" enums:"string,int,bigint,float,decimal,date,datetime,bool,json,text,geometry"`
	Length         *int   `json:"length,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	Nullable       bool   `json:"nullable"`
	IsPK           bool   `json:"is_pk"`
	IsPartition    bool   `json:"is_partition"`
	DefaultValue   string `json:"default_value"`
	Description    string `json:"description"`
	SortOrder      int    `json:"sort_order" binding:"gte=0" minimum:"0"`
	FieldRole      string `json:"field_role" binding:"omitempty,oneof=regular measure_additive measure_semi measure_non dimension_fk degenerate_dim" enums:"regular,measure_additive,measure_semi,measure_non,dimension_fk,degenerate_dim"`
	HierarchyID    *int64 `json:"hierarchy_id,omitempty" binding:"omitempty,gt=0" minimum:"1"`
	HierarchyLevel *int   `json:"hierarchy_level,omitempty" binding:"omitempty,gte=0" minimum:"0"`
}

// UpdateLogicalFieldRequest 更新逻辑表字段请求
type UpdateLogicalFieldRequest struct {
	ElementID      *int64 `json:"element_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	Name           string `json:"name" binding:"required,max=200" maxLength:"200"`
	ColumnName     string `json:"column_name" binding:"required,max=200" maxLength:"200"`
	DataType       string `json:"data_type" binding:"required,oneof=string int bigint float decimal date datetime bool json text geometry" enums:"string,int,bigint,float,decimal,date,datetime,bool,json,text,geometry"`
	Length         *int   `json:"length" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	Nullable       *bool  `json:"nullable" binding:"required"`
	IsPK           *bool  `json:"is_pk" binding:"required"`
	IsPartition    *bool  `json:"is_partition" binding:"required"`
	DefaultValue   string `json:"default_value"`
	Description    string `json:"description"`
	SortOrder      *int   `json:"sort_order" binding:"required,gte=0" minimum:"0"`
	FieldRole      string `json:"field_role" binding:"required,oneof=regular measure_additive measure_semi measure_non dimension_fk degenerate_dim" enums:"regular,measure_additive,measure_semi,measure_non,dimension_fk,degenerate_dim"`
	HierarchyID    *int64 `json:"hierarchy_id" binding:"omitempty,gt=0" minimum:"1" extensions:"x-nullable"`
	HierarchyLevel *int   `json:"hierarchy_level" binding:"omitempty,gte=0" minimum:"0" extensions:"x-nullable"`
}
