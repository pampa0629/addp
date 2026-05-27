package plugin

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/addp/common/datatype"
)

// CatalogModelProvider declares an engine's catalog shape and terminology.
type CatalogModelProvider interface {
	EnginePlugin
	CatalogModel() CatalogModelSpec
}

// CatalogProvider lists real catalog nodes from an engine.
type CatalogProvider interface {
	EnginePlugin
	ListChildren(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogNode, error)
	ResolvePath(ctx context.Context, connInfo ConnectionInfo, path CatalogPath) (*CatalogNode, error)
}

// ItemMetadataProvider describes leaf items in a catalog.
type ItemMetadataProvider interface {
	EnginePlugin
	DescribeItem(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error)
}

// DocumentMetadataSamplingProvider samples document items to infer dynamic field info.
type DocumentMetadataSamplingProvider interface {
	EnginePlugin
	SampleDocumentMetadata(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error)
}

// GraphMetadataProvider describes graph structure facts for a graph item.
type GraphMetadataProvider interface {
	EnginePlugin
	DescribeGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*datatype.GraphInfo, error)
}

// GraphSampleProvider samples graph nodes and relationships for lightweight previews.
type GraphSampleProvider interface {
	EnginePlugin
	SampleGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts GraphSampleOptions) (*GraphData, error)
}

// StoreProvider is a marker for item content access capabilities.
type StoreProvider interface {
	EnginePlugin
	StoreSemantics() StoreSemantics
}

type ContentReadableProvider interface {
	StoreProvider
	OpenContent(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts ReadOptions) (io.ReadCloser, error)
}

type ContentWritableProvider interface {
	StoreProvider
	CreateContent(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts WriteOptions) (io.WriteCloser, error)
}

type ResourceDeleteProvider interface {
	StoreProvider
	DeleteResource(ctx context.Context, connInfo ConnectionInfo, path CatalogPath) error
}

type RangeReadableProvider interface {
	StoreProvider
	OpenRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts ReadOptions) (io.ReadCloser, error)
}

type RangeWritableProvider interface {
	StoreProvider
	WriteRange(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, offset int64, r io.Reader, opts WriteOptions) (int64, error)
}

type BatchReadableProvider interface {
	StoreProvider
	ReadBatch(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts BatchReadOptions) (*BatchData, error)
}

type TableReadSessionProvider interface {
	StoreProvider
	OpenTableReadSession(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts TableReadSessionOptions) (TableReadSession, error)
}

type TableReadSession interface {
	ReadBatch(ctx context.Context, limit int) (*BatchData, error)
	Close(ctx context.Context) error
}

type BatchWritableProvider interface {
	StoreProvider
	WriteBatch(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, batch *BatchData, opts BatchWriteOptions) error
}

type TableWriteSessionProvider interface {
	StoreProvider
	OpenTableWriteSession(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts TableWriteSessionOptions) (TableWriteSession, error)
}

type TableWriteSession interface {
	WriteBatch(ctx context.Context, batch *BatchData) error
	Close(ctx context.Context) error
	Abort(ctx context.Context) error
}

type TableWritePreparer interface {
	StoreProvider
	PrepareTableWrite(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts TableWriteOptions) error
}

type QueryRuntimeProvider interface {
	EnginePlugin
	QueryLanguages() []string
	GenerateSampleQuery(ctx context.Context, connInfo ConnectionInfo, opts SampleQueryOptions) (query string, language string)
	ExecuteRuntimeQuery(ctx context.Context, connInfo ConnectionInfo, req QueryRequest) (*QueryResult, error)
}

type SQLQueryRuntimeProvider interface {
	QueryRuntimeProvider
	SQLDialect() string
	ExecuteSQL(ctx context.Context, connInfo ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error)
}

type DocumentQueryRuntimeProvider interface {
	QueryRuntimeProvider
	ExecuteDocumentQuery(ctx context.Context, connInfo ConnectionInfo, command string, opts QueryOptions) (*QueryResult, error)
}

// GraphQueryProvider is a dedicated runtime for graph-shaped queries and results.
// It is intentionally separate from QueryRuntimeProvider so graph modules can
// consume graph-specific results without coupling ordinary table-oriented query flows.
type GraphQueryProvider interface {
	EnginePlugin
	ExecuteGraphQuery(ctx context.Context, connInfo ConnectionInfo, cypher string, opts QueryOptions) (*GraphQueryResult, error)
}

type WorkflowRuntimeProvider interface {
	EnginePlugin
	RuntimeEndpoint(ctx context.Context, connInfo ConnectionInfo) (string, error)
	ListOperators(ctx context.Context, connInfo ConnectionInfo) ([]OperatorMetadata, error)
	ExecuteWorkflow(ctx context.Context, connInfo ConnectionInfo, req WorkflowExecuteRequest) (*WorkflowExecuteResult, error)
}

type ScriptRuntimeProvider interface {
	EnginePlugin
	RuntimeEndpoint(ctx context.Context, connInfo ConnectionInfo) (string, error)
	OpenSession(ctx context.Context, connInfo ConnectionInfo, req ScriptSessionRequest) (*ScriptSession, error)
}

type CatalogPath struct {
	Version  string           `json:"version"`
	EngineID uint             `json:"engine_id,omitempty"`
	Segments []CatalogSegment `json:"segments"`
}

func (p CatalogPath) StringPath() string {
	parts := make([]string, 0, len(p.Segments))
	for _, segment := range p.Segments {
		if segment.Name != "" {
			part := strings.Trim(segment.Name, "/")
			if part != "" {
				parts = append(parts, part)
			}
		}
	}
	return strings.Join(parts, "/")
}

type CatalogSegment struct {
	Term string `json:"term"`
	Kind string `json:"kind"`
	Name string `json:"name"`
}

type CatalogNode struct {
	Name        string                 `json:"name"`
	Path        CatalogPath            `json:"path"`
	Term        string                 `json:"term"`
	Kind        string                 `json:"kind"`
	IsContainer bool                   `json:"is_container"`
	IsItem      bool                   `json:"is_item"`
	Stats       map[string]interface{} `json:"stats,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
	Actions     []string               `json:"actions,omitempty"`
}

type ListOptions struct {
	Recursive bool
	Limit     int
	Offset    int
	Filter    map[string]interface{}
}

type MetadataOptions struct {
	IncludeStatistics bool
	IncludeIndexes    bool
	IncludeSamples    bool
	SampleSize        int
}

const (
	GraphSampleKindNodeShape         = "node_shape"
	GraphSampleKindRelationshipShape = "relationship_shape"
)

// GraphSampleFilter describes the graph shape selected for sampling.
type GraphSampleFilter struct {
	Kind             string
	Labels           []string
	RelationshipType string
	FromLabels       []string
	ToLabels         []string
}

func (f GraphSampleFilter) IsZero() bool {
	return strings.TrimSpace(f.Kind) == "" &&
		len(cleanGraphSampleFilterStrings(f.Labels)) == 0 &&
		strings.TrimSpace(f.RelationshipType) == "" &&
		len(cleanGraphSampleFilterStrings(f.FromLabels)) == 0 &&
		len(cleanGraphSampleFilterStrings(f.ToLabels)) == 0
}

func (f GraphSampleFilter) Clone() GraphSampleFilter {
	return GraphSampleFilter{
		Kind:             strings.TrimSpace(f.Kind),
		Labels:           cleanGraphSampleFilterStrings(f.Labels),
		RelationshipType: strings.TrimSpace(f.RelationshipType),
		FromLabels:       cleanGraphSampleFilterStrings(f.FromLabels),
		ToLabels:         cleanGraphSampleFilterStrings(f.ToLabels),
	}
}

type GraphSampleOptions struct {
	Limit  int
	Filter GraphSampleFilter
}

type ItemMetadata struct {
	Path       CatalogPath             `json:"path"`
	Kind       string                  `json:"kind"`
	Table      *datatype.TableInfo     `json:"table,omitempty"`
	Document   *datatype.DocumentInfo  `json:"document,omitempty"`
	Media      *datatype.MediaInfo     `json:"media,omitempty"`
	Container  *datatype.ContainerInfo `json:"container,omitempty"`
	Graph      *datatype.GraphInfo     `json:"graph,omitempty"`
	Fields     []datatype.FieldInfo    `json:"fields,omitempty"`
	Indexes    []IndexInfo             `json:"indexes,omitempty"`
	Stats      map[string]interface{}  `json:"stats,omitempty"`
	Attributes map[string]interface{}  `json:"attributes,omitempty"`
	UpdatedAt  *time.Time              `json:"updated_at,omitempty"`
}

func cleanGraphSampleFilterStrings(values []string) []string {
	if len(values) == 0 {
		return nil
	}
	result := make([]string, 0, len(values))
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			result = append(result, text)
		}
	}
	return result
}

type StoreSemantics struct {
	Semantics    []string `json:"semantics,omitempty"`
	NotSupported []string `json:"not_supported,omitempty"`
}

type ReadOptions struct {
	Offset int64
	Length int64
}

type WriteOptions struct {
	ContentType string
	Overwrite   bool
	Metadata    map[string]string
}

type BatchReadOptions struct {
	Limit  int
	Offset int64
	Query  string
}

type TableReadSessionOptions struct {
	Query    string
	Metadata map[string]interface{}
}

type BatchWriteOptions struct {
	Method string
}

type TableWriteSessionOptions struct {
	Method      string
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
}

type TableWriteOptions struct {
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
}

type BatchData struct {
	Rows     []map[string]interface{} `json:"rows"`
	Fields   []datatype.FieldInfo     `json:"fields,omitempty"`
	Spatial  *datatype.SpatialInfo    `json:"spatial,omitempty"`
	Metadata map[string]interface{}   `json:"metadata,omitempty"`
	Offset   int64                    `json:"offset,omitempty"`
}

type SampleQueryOptions struct {
	Path CatalogPath
}

type QueryRequest struct {
	EngineID uint
	Language string
	Query    string
	Options  QueryOptions
}

type QueryOptions struct {
	EngineID   uint
	EngineType string
	Limit      int
	Timeout    time.Duration
	ReadOnly   bool
}

type OperatorMetadata struct {
	ID                  string                 `json:"id,omitempty"`
	Name                string                 `json:"name"`
	DisplayName         string                 `json:"display_name,omitempty"`
	Type                string                 `json:"type,omitempty"`
	Category            string                 `json:"category,omitempty"`
	Description         string                 `json:"description,omitempty"`
	BriefDescription    string                 `json:"brief_description,omitempty"`
	DetailedDescription map[string]interface{} `json:"detailed_description,omitempty"`
	Parameters          []ParameterMetadata    `json:"parameters,omitempty"`
	Inputs              []interface{}          `json:"inputs,omitempty"`
	OutputPorts         []OutputPortMetadata   `json:"output_ports,omitempty"`
	Outputs             []datatype.FieldInfo   `json:"outputs,omitempty"`
	Module              string                 `json:"module,omitempty"`
	Attributes          map[string]interface{} `json:"attributes,omitempty"`
}

type ParameterMetadata struct {
	Name        string                       `json:"name"`
	Type        string                       `json:"type"`
	Required    bool                         `json:"required"`
	Default     interface{}                  `json:"default,omitempty"`
	Description string                       `json:"description,omitempty"`
	Enum        []string                     `json:"enum,omitempty"`
	Min         *float64                     `json:"min,omitempty"`
	Max         *float64                     `json:"max,omitempty"`
	Pattern     string                       `json:"pattern,omitempty"`
	ItemType    string                       `json:"item_type,omitempty"`
	Properties  map[string]ParameterMetadata `json:"properties,omitempty"`
	DependsOn   string                       `json:"depends_on,omitempty"`
	ShowWhen    map[string]interface{}       `json:"show_when,omitempty"`
	Notes       string                       `json:"notes,omitempty"`
	UIType      string                       `json:"ui_type,omitempty"`
	UIConfig    map[string]interface{}       `json:"ui_config,omitempty"`
}

type OutputPortMetadata struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"is_default"`
}

type WorkflowExecuteRequest struct {
	WorkflowDef map[string]interface{} `json:"workflow_def"`
	InputData   map[string]interface{} `json:"input_data,omitempty"`
	Runtime     map[string]interface{} `json:"runtime,omitempty"`
}

type WorkflowExecuteResult struct {
	Status      string                 `json:"status"`
	ExecutionID string                 `json:"execution_id,omitempty"`
	Result      map[string]interface{} `json:"result,omitempty"`
	Error       string                 `json:"error,omitempty"`
}

type ScriptSessionRequest struct {
	Mode     string                 `json:"mode"`
	Language string                 `json:"language,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ScriptSession struct {
	ID       string                 `json:"id"`
	Endpoint string                 `json:"endpoint,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`
}
