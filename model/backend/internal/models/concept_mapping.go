package models

import "time"

type LogicalTableEntityMapping struct {
	ID            int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID      int64     `gorm:"not null;index" json:"tenant_id"`
	TableID       int64     `gorm:"not null;index" json:"table_id"`
	EntityID      int64     `gorm:"not null;index" json:"entity_id"`
	MappingRole   string    `gorm:"size:24;not null" json:"mapping_role" enums:"represents,derives_from"`
	EntityVersion int64     `gorm:"not null" json:"entity_version"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

func (LogicalTableEntityMapping) TableName() string {
	return "model.logical_table_entity_mappings"
}

type LogicalFieldAttributeMapping struct {
	ID                int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID          int64     `gorm:"not null;index" json:"tenant_id"`
	TableID           int64     `gorm:"not null;index" json:"table_id"`
	FieldID           int64     `gorm:"not null;index" json:"field_id"`
	EntityID          int64     `gorm:"not null" json:"entity_id"`
	EntityAttributeID int64     `gorm:"not null;index" json:"entity_attribute_id"`
	MappingRole       string    `gorm:"size:16;not null" json:"mapping_role" enums:"direct,derived"`
	CreatedAt         time.Time `json:"created_at"`
	UpdatedAt         time.Time `json:"updated_at"`
}

func (LogicalFieldAttributeMapping) TableName() string {
	return "model.logical_field_attribute_mappings"
}

type TableRelationEntityRelationMapping struct {
	ID                    int64     `gorm:"primaryKey;autoIncrement" json:"id"`
	TenantID              int64     `gorm:"not null;index" json:"tenant_id"`
	TableID               int64     `gorm:"not null;index" json:"table_id"`
	TableRelationID       int64     `gorm:"not null;index" json:"table_relation_id"`
	EntityRelationID      int64     `gorm:"not null;index" json:"entity_relation_id"`
	Orientation           string    `gorm:"size:16;not null" json:"orientation" enums:"same,inverse"`
	EntityRelationVersion int64     `gorm:"not null" json:"entity_relation_version"`
	CreatedAt             time.Time `json:"created_at"`
	UpdatedAt             time.Time `json:"updated_at"`
}

func (TableRelationEntityRelationMapping) TableName() string {
	return "model.table_relation_entity_relation_mappings"
}

type TableEntityMappingInput struct {
	EntityID    int64  `json:"entity_id" binding:"required,gt=0" minimum:"1"`
	MappingRole string `json:"mapping_role" binding:"required,oneof=represents derives_from" enums:"represents,derives_from"`
}

type FieldAttributeMappingInput struct {
	FieldID           int64  `json:"field_id" binding:"required,gt=0" minimum:"1"`
	EntityAttributeID int64  `json:"entity_attribute_id" binding:"required,gt=0" minimum:"1"`
	MappingRole       string `json:"mapping_role" binding:"required,oneof=direct derived" enums:"direct,derived"`
}

type RelationConceptMappingInput struct {
	TableRelationID  int64  `json:"table_relation_id" binding:"required,gt=0" minimum:"1"`
	EntityRelationID int64  `json:"entity_relation_id" binding:"required,gt=0" minimum:"1"`
	Orientation      string `json:"orientation" binding:"required,oneof=same inverse" enums:"same,inverse"`
}

type ReplaceConceptMappingsRequest struct {
	Version          int64                         `json:"version" binding:"required,gt=0" minimum:"1"`
	TableMappings    []TableEntityMappingInput     `json:"table_mappings"`
	FieldMappings    []FieldAttributeMappingInput  `json:"field_mappings"`
	RelationMappings []RelationConceptMappingInput `json:"relation_mappings"`
}

type TableEntityMappingView struct {
	LogicalTableEntityMapping
	EntityName           string `json:"entity_name"`
	EntityCode           string `json:"entity_code"`
	EntityStatus         string `json:"entity_status"`
	CurrentEntityVersion int64  `json:"current_entity_version"`
	InSync               bool   `json:"in_sync"`
}

type FieldAttributeMappingView struct {
	LogicalFieldAttributeMapping
	FieldName           string `json:"field_name"`
	FieldColumnName     string `json:"field_column_name"`
	EntityName          string `json:"entity_name"`
	EntityCode          string `json:"entity_code"`
	EntityAttributeName string `json:"entity_attribute_name"`
	AttributeColumnName string `json:"attribute_column_name"`
}

type RelationConceptMappingView struct {
	TableRelationEntityRelationMapping
	EntityRelationName           string `json:"entity_relation_name"`
	SourceEntityName             string `json:"source_entity_name"`
	TargetEntityName             string `json:"target_entity_name"`
	CurrentEntityRelationVersion int64  `json:"current_entity_relation_version"`
	InSync                       bool   `json:"in_sync"`
}

type ConceptMappingsResponse struct {
	Version          int64                        `json:"version"`
	TableMappings    []TableEntityMappingView     `json:"table_mappings"`
	FieldMappings    []FieldAttributeMappingView  `json:"field_mappings"`
	RelationMappings []RelationConceptMappingView `json:"relation_mappings"`
}
