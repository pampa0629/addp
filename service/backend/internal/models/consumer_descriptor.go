package models

import "github.com/addp/common/datatype"

const ConsumerDescriptorSchemaVersion = "addp.service_consumer/v1"

const (
	ConsumerServiceTypeQuery      = "query"
	ConsumerServiceTypeGraph      = "graph"
	ConsumerServiceTypeTile       = "tile"
	ConsumerServiceTypeRegistered = "registered"
)

const (
	ConsumerOutputKindTabular        = "tabular"
	ConsumerOutputKindSpatialTabular = "spatial_tabular"
)

type ConsumerServiceReference struct {
	ServiceType string `json:"service_type" enums:"query,graph,tile,registered"`
	ServiceID   uint   `json:"service_id"`
}

// ConsumerServiceSummary is the lightweight Service Consumer Catalog item.
type ConsumerServiceSummary struct {
	Ref                 ConsumerServiceReference `json:"ref"`
	Title               string                   `json:"title"`
	Description         string                   `json:"description"`
	AccessMode          string                   `json:"access_mode" enums:"public,private"`
	OutputKind          string                   `json:"output_kind" enums:"tabular,spatial_tabular"`
	ContractFingerprint string                   `json:"contract_fingerprint"`
}

type ConsumerServiceListResponse struct {
	Data       []ConsumerServiceSummary `json:"data"`
	Total      int64                    `json:"total"`
	Page       int                      `json:"page"`
	PageSize   int                      `json:"page_size"`
	TotalPages int64                    `json:"total_pages"`
}

// ConsumerDescriptor is the public, read-only service consumption contract.
// It intentionally excludes management DTO fields and internal execution facts.
type ConsumerDescriptor struct {
	SchemaVersion       string                       `json:"schema_version"`
	Ref                 ConsumerServiceReference     `json:"ref"`
	Title               string                       `json:"title"`
	Description         string                       `json:"description"`
	Status              string                       `json:"status" enums:"active"`
	AccessMode          string                       `json:"access_mode" enums:"public,private"`
	ContractFingerprint string                       `json:"contract_fingerprint"`
	Operations          []ConsumerOperation          `json:"operations"`
	InputContract       StructuredQueryInputContract `json:"input_contract"`
	OutputContract      TabularOutputContract        `json:"output_contract"`
}

type ConsumerOperation struct {
	Key        string `json:"key" enums:"query"`
	Method     string `json:"method" enums:"POST"`
	Path       string `json:"path"`
	InputKind  string `json:"input_kind" enums:"structured_query"`
	OutputKind string `json:"output_kind" enums:"tabular,spatial_tabular"`
}

type StructuredQueryInputContract struct {
	Kind             string                 `json:"kind" enums:"structured_query"`
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
	Kind         string `json:"kind" enums:"cursor"`
	DefaultLimit int    `json:"default_limit"`
	MaxLimit     int    `json:"max_limit"`
}

// ConsumerQueryIntent declares the optional header used to distinguish an
// interactive query from a single bounded export over the same operation.
type ConsumerQueryIntent struct {
	Header        string   `json:"header"`
	AllowedValues []string `json:"allowed_values"`
	DefaultValue  string   `json:"default_value"`
}

type TabularOutputContract struct {
	Kind    string                   `json:"kind" enums:"tabular,spatial_tabular"`
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

type ConsumerServiceListFilter struct {
	TenantID   uint
	Search     string
	OutputKind string
	Offset     int
	Limit      int
}
