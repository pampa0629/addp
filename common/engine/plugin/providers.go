package plugin

import (
	"context"
	"io"
	"strings"
	"time"
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

// DocumentMetadataSamplingProvider samples document items to infer dynamic field schema.
type DocumentMetadataSamplingProvider interface {
	EnginePlugin
	SampleDocumentMetadata(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error)
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

type BatchWritableProvider interface {
	StoreProvider
	WriteBatch(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, batch *BatchData, opts BatchWriteOptions) error
}

type PreviewProvider interface {
	EnginePlugin
	PreviewItem(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts PreviewOptions) (*PreviewResult, error)
}

type TransferAdapter interface {
	EnginePlugin
	BuildReaderConfig(ctx context.Context, engine Engine, item CatalogPath, opts TransferReadOptions) (*TransferConnectorConfig, error)
	BuildWriterConfig(ctx context.Context, engine Engine, target CatalogPath, opts TransferWriteOptions) (*TransferConnectorConfig, error)
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
			parts = append(parts, strings.Trim(segment.Name, "/"))
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

type ItemMetadata struct {
	Path       CatalogPath            `json:"path"`
	Kind       string                 `json:"kind"`
	Fields     []FieldInfo            `json:"fields,omitempty"`
	Indexes    []IndexInfo            `json:"indexes,omitempty"`
	Stats      map[string]interface{} `json:"stats,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
	UpdatedAt  *time.Time             `json:"updated_at,omitempty"`
}

type FieldInfo struct {
	Name       string                 `json:"name"`
	Type       string                 `json:"type"`
	NativeType string                 `json:"native_type,omitempty"`
	Nullable   bool                   `json:"nullable"`
	PrimaryKey bool                   `json:"primary_key,omitempty"`
	Comment    string                 `json:"comment,omitempty"`
	Attributes map[string]interface{} `json:"attributes,omitempty"`
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

type BatchWriteOptions struct {
	Mode string
}

type BatchData struct {
	Rows     []map[string]interface{} `json:"rows"`
	Fields   []FieldInfo              `json:"fields,omitempty"`
	Metadata map[string]interface{}   `json:"metadata,omitempty"`
	Offset   int64                    `json:"offset,omitempty"`
}

type PreviewOptions struct {
	Mode     string
	MaxRows  int
	MaxBytes int64
}

type PreviewResult struct {
	Mode     string                   `json:"mode"`
	Fields   []FieldInfo              `json:"fields,omitempty"`
	Rows     []map[string]interface{} `json:"rows,omitempty"`
	Graph    *GraphData               `json:"graph,omitempty"`
	Metadata map[string]interface{}   `json:"metadata,omitempty"`
}

type TransferReadOptions struct {
	Format    string
	BatchSize int
}

type TransferWriteOptions struct {
	Format    string
	BatchSize int
	Mode      string
}

type TransferConnectorConfig struct {
	Type      string                 `json:"type"`
	Config    map[string]interface{} `json:"config"`
	BatchSize int                    `json:"batch_size,omitempty"`
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
	Name        string                 `json:"name"`
	DisplayName string                 `json:"display_name,omitempty"`
	Category    string                 `json:"category,omitempty"`
	Inputs      []FieldInfo            `json:"inputs,omitempty"`
	Outputs     []FieldInfo            `json:"outputs,omitempty"`
	Attributes  map[string]interface{} `json:"attributes,omitempty"`
}

type WorkflowExecuteRequest struct {
	WorkflowDef map[string]interface{} `json:"workflow_def"`
	InputData   map[string]interface{} `json:"input_data,omitempty"`
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
