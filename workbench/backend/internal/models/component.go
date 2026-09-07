package models

import (
	"encoding/json"
)

const (
	RendererTypeTable = "table"
	RendererTypeChart = "chart"
	RendererTypeMap   = "map"
	RendererTypeValue = "value"
)

type ServiceReference struct {
	ServiceType string `json:"service_type" binding:"required" enums:"query"`
	ServiceID   int64  `json:"service_id" binding:"required"`
}

type ComponentConfiguration struct {
	Name                   string                         `json:"name" binding:"required"`
	Description            string                         `json:"description"`
	ServiceRef             *ServiceReference              `json:"service_ref,omitempty"`
	ParameterDefinitions   []ComponentParameterDefinition `json:"parameter_definitions"`
	QueryTemplate          ComponentQueryTemplate         `json:"query_template"`
	DefaultParameterValues map[string]json.RawMessage     `json:"default_parameter_values"`
	RendererType           string                         `json:"renderer_type" binding:"required" enums:"table,chart,map,value"`
	RendererConfig         json.RawMessage                `json:"renderer_config" binding:"required" swaggertype:"object"`
}

type ComponentParameterDefinition struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	ControlType string `json:"control_type"`
	Required    bool   `json:"required"`
}

type ComponentQueryTemplate struct {
	Select                 []string                         `json:"select"`
	FixedFilter            *QueryFilter                     `json:"fixed_filter"`
	ParameterFilters       []ComponentParameterFilter       `json:"parameter_filters"`
	NamedParameterBindings []ComponentNamedParameterBinding `json:"named_parameter_bindings"`
	OrderBy                []QueryOrder                     `json:"order_by"`
	PageLimit              int                              `json:"page_limit"`
	Format                 string                           `json:"format"`
}

type ComponentParameterFilter struct {
	ParameterKey string `json:"parameter_key"`
	Field        string `json:"field"`
	Operator     string `json:"operator"`
}

type ComponentNamedParameterBinding struct {
	ParameterKey string `json:"parameter_key"`
	Name         string `json:"name"`
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
	Columns            []string            `json:"columns"`
	FieldPresentations []FieldPresentation `json:"field_presentations,omitempty"`
}

type ChartRendererConfig struct {
	ChartType          string              `json:"chart_type"`
	Dimension          string              `json:"dimension"`
	Measures           []string            `json:"measures"`
	FieldPresentations []FieldPresentation `json:"field_presentations,omitempty"`
}

type MapRendererConfig struct {
	GeometryField      string                  `json:"geometry_field"`
	LabelField         string                  `json:"label_field,omitempty"`
	TooltipFields      []string                `json:"tooltip_fields"`
	Style              *MapRendererStyleConfig `json:"style,omitempty"`
	FieldPresentations []FieldPresentation     `json:"field_presentations,omitempty"`
}

type FieldPresentation struct {
	Field          string                  `json:"field"`
	Label          string                  `json:"label"`
	Unit           string                  `json:"unit,omitempty"`
	Precision      *int                    `json:"precision,omitempty"`
	TemporalFormat string                  `json:"temporal_format,omitempty"`
	Width          *int                    `json:"width,omitempty"`
	StateRules     []StatePresentationRule `json:"state_rules,omitempty"`
}

type StatePresentationRule struct {
	Operator string          `json:"operator" enums:"eq,lt,lte,gt,gte"`
	Operand  json.RawMessage `json:"operand" swaggertype:"object"`
	Label    string          `json:"label"`
	Tone     string          `json:"tone" enums:"info,success,warning,danger"`
}

type MapRendererStyleConfig struct {
	Mode        string `json:"mode"`
	Field       string `json:"field,omitempty"`
	Palette     string `json:"palette"`
	LegendTitle string `json:"legend_title,omitempty"`
}

type ValueRendererConfig struct {
	Items []ValueRendererItem `json:"items"`
}

type ValueRendererItem struct {
	Field      string                  `json:"field"`
	Label      string                  `json:"label"`
	Unit       string                  `json:"unit"`
	Precision  int                     `json:"precision"`
	StateRules []StatePresentationRule `json:"state_rules,omitempty"`
}
