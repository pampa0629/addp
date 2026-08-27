package models

import (
	"encoding/json"
	"time"

	"gorm.io/datatypes"
)

const (
	RendererTypeTable = "table"
	RendererTypeChart = "chart"
	RendererTypeMap   = "map"
)

type View struct {
	ID                     string         `gorm:"type:uuid;primaryKey"`
	TenantID               int64          `gorm:"not null;index:idx_workbench_view_owner,priority:1"`
	OwnerUserID            int64          `gorm:"not null;index:idx_workbench_view_owner,priority:2"`
	Name                   string         `gorm:"type:varchar(200);not null"`
	Description            string         `gorm:"type:text;not null"`
	ServiceType            string         `gorm:"type:varchar(32);not null"`
	ServiceID              int64          `gorm:"not null"`
	ContractFingerprint    string         `gorm:"type:varchar(80);not null"`
	ParameterDefinitions   datatypes.JSON `gorm:"type:jsonb;not null"`
	QueryTemplate          datatypes.JSON `gorm:"type:jsonb;not null"`
	DefaultParameterValues datatypes.JSON `gorm:"type:jsonb;not null"`
	RendererType           string         `gorm:"type:varchar(32);not null"`
	RendererConfig         datatypes.JSON `gorm:"type:jsonb;not null"`
	Version                int64          `gorm:"not null"`
	CreatedAt              time.Time
	UpdatedAt              time.Time
}

func (View) TableName() string { return "workbench.views" }

type ServiceReference struct {
	ServiceType string `json:"service_type" binding:"required" enums:"query"`
	ServiceID   int64  `json:"service_id" binding:"required"`
}

type ViewResponse struct {
	ID                     string           `json:"id"`
	TenantID               int64            `json:"tenant_id"`
	OwnerUserID            int64            `json:"owner_user_id"`
	Name                   string           `json:"name"`
	Description            string           `json:"description"`
	ServiceRef             ServiceReference `json:"service_ref"`
	ContractFingerprint    string           `json:"contract_fingerprint"`
	ParameterDefinitions   json.RawMessage  `json:"parameter_definitions" swaggertype:"array,object"`
	QueryTemplate          json.RawMessage  `json:"query_template" swaggertype:"object"`
	DefaultParameterValues json.RawMessage  `json:"default_parameter_values" swaggertype:"object"`
	RendererType           string           `json:"renderer_type" enums:"table,chart,map"`
	RendererConfig         json.RawMessage  `json:"renderer_config" swaggertype:"object"`
	Version                int64            `json:"version"`
	CreatedAt              time.Time        `json:"created_at"`
	UpdatedAt              time.Time        `json:"updated_at"`
}

func ViewResponseOf(view View) ViewResponse {
	return ViewResponse{
		ID: view.ID, TenantID: view.TenantID, OwnerUserID: view.OwnerUserID,
		Name: view.Name, Description: view.Description,
		ServiceRef:             ServiceReference{ServiceType: view.ServiceType, ServiceID: view.ServiceID},
		ContractFingerprint:    view.ContractFingerprint,
		ParameterDefinitions:   json.RawMessage(view.ParameterDefinitions),
		QueryTemplate:          json.RawMessage(view.QueryTemplate),
		DefaultParameterValues: json.RawMessage(view.DefaultParameterValues),
		RendererType:           view.RendererType, RendererConfig: json.RawMessage(view.RendererConfig),
		Version: view.Version, CreatedAt: view.CreatedAt, UpdatedAt: view.UpdatedAt,
	}
}

type ViewWriteRequest struct {
	Name                   string                     `json:"name" binding:"required"`
	Description            string                     `json:"description"`
	ServiceRef             *ServiceReference          `json:"service_ref,omitempty"`
	ParameterDefinitions   []ViewParameterDefinition  `json:"parameter_definitions"`
	QueryTemplate          ViewQueryTemplate          `json:"query_template"`
	DefaultParameterValues map[string]json.RawMessage `json:"default_parameter_values"`
	RendererType           string                     `json:"renderer_type" binding:"required" enums:"table,chart,map"`
	RendererConfig         json.RawMessage            `json:"renderer_config" binding:"required" swaggertype:"object"`
	Version                *int64                     `json:"version,omitempty"`
}

type ViewParameterDefinition struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	ControlType string `json:"control_type"`
	Required    bool   `json:"required"`
}

type ViewQueryTemplate struct {
	Select           []string              `json:"select"`
	FixedFilter      *QueryFilter          `json:"fixed_filter"`
	ParameterFilters []ViewParameterFilter `json:"parameter_filters"`
	OrderBy          []QueryOrder          `json:"order_by"`
	PageLimit        int                   `json:"page_limit"`
	Format           string                `json:"format"`
}

type ViewParameterFilter struct {
	ParameterKey string `json:"parameter_key"`
	Field        string `json:"field"`
	Operator     string `json:"operator"`
}

type QueryOrder struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}

type QueryFilter struct {
	Field string        `json:"field,omitempty"`
	Op    string        `json:"op,omitempty"`
	Value interface{}   `json:"value,omitempty" swaggertype:"object"`
	And   []QueryFilter `json:"and,omitempty"`
	Or    []QueryFilter `json:"or,omitempty"`
	Not   *QueryFilter  `json:"not,omitempty"`
}

type TableRendererConfig struct {
	Columns []string `json:"columns"`
}

type ChartRendererConfig struct {
	ChartType string   `json:"chart_type"`
	Dimension string   `json:"dimension"`
	Measures  []string `json:"measures"`
}

type MapRendererConfig struct {
	GeometryField string   `json:"geometry_field"`
	TooltipFields []string `json:"tooltip_fields"`
}
