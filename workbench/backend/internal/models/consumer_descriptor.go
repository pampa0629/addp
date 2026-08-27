package models

import "github.com/addp/common/datatype"

const ConsumerDescriptorSchemaVersion = "addp.service_consumer/v1"

type ConsumerDescriptor struct {
	SchemaVersion       string                       `json:"schema_version"`
	Ref                 ServiceReference             `json:"ref"`
	Title               string                       `json:"title"`
	Description         string                       `json:"description"`
	Status              string                       `json:"status"`
	AccessMode          string                       `json:"access_mode"`
	ContractFingerprint string                       `json:"contract_fingerprint"`
	Operations          []ConsumerOperation          `json:"operations"`
	InputContract       StructuredQueryInputContract `json:"input_contract"`
	OutputContract      TabularOutputContract        `json:"output_contract"`
}

type ConsumerOperation struct {
	Key        string `json:"key"`
	Method     string `json:"method"`
	Path       string `json:"path"`
	InputKind  string `json:"input_kind"`
	OutputKind string `json:"output_kind"`
}

type StructuredQueryInputContract struct {
	Kind             string                 `json:"kind"`
	Fields           []ConsumerQueryField   `json:"fields"`
	DefaultSelection []string               `json:"default_selection"`
	Filter           ConsumerFilterContract `json:"filter"`
	Order            ConsumerOrderContract  `json:"order"`
	Page             ConsumerPageContract   `json:"page"`
	Formats          []string               `json:"formats"`
	Intent           ConsumerQueryIntent    `json:"intent"`
}

type ConsumerQueryField struct {
	Name        string             `json:"name"`
	Type        datatype.FieldType `json:"type"`
	ElementType datatype.FieldType `json:"element_type,omitempty"`
	Nullable    bool               `json:"nullable"`
	Selectable  bool               `json:"selectable"`
	Filterable  bool               `json:"filterable"`
	Operators   []string           `json:"operators"`
	Sortable    bool               `json:"sortable"`
}

type ConsumerQueryIntent struct {
	Header        string   `json:"header"`
	AllowedValues []string `json:"allowed_values"`
	DefaultValue  string   `json:"default_value"`
}

type ConsumerFilterContract struct {
	Combinators []string `json:"combinators"`
	MaxDepth    int      `json:"max_depth"`
	MaxNodes    int      `json:"max_nodes"`
	MaxInValues int      `json:"max_in_values"`
}

type ConsumerOrderContract struct {
	Directions []string `json:"directions"`
	StableKey  []string `json:"stable_key"`
}

type ConsumerPageContract struct {
	Kind         string `json:"kind"`
	DefaultLimit int    `json:"default_limit"`
	MaxLimit     int    `json:"max_limit"`
}

type TabularOutputContract struct {
	Kind    string                   `json:"kind"`
	Fields  []ConsumerOutputField    `json:"fields"`
	Spatial *ConsumerSpatialContract `json:"spatial,omitempty"`
}

type ConsumerOutputField struct {
	Name        string             `json:"name"`
	Type        datatype.FieldType `json:"type"`
	ElementType datatype.FieldType `json:"element_type,omitempty"`
	Nullable    bool               `json:"nullable"`
	Comment     string             `json:"comment,omitempty"`
}

type ConsumerSpatialContract struct {
	PrimaryGeometryField string                  `json:"primary_geometry_field"`
	SRID                 *int                    `json:"srid,omitempty"`
	CRSRef               string                  `json:"crs_ref,omitempty"`
	GeometryFields       []ConsumerGeometryField `json:"geometry_fields"`
}

type ConsumerGeometryField struct {
	Name         string `json:"name"`
	GeometryType string `json:"geometry_type,omitempty"`
	SRID         *int   `json:"srid,omitempty"`
	CRSRef       string `json:"crs_ref,omitempty"`
	Dimension    *int   `json:"dimension,omitempty"`
}
