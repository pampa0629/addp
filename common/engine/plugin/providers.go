package plugin

import (
	"context"
	"io"
	"strings"
	"time"

	"github.com/addp/common/datatype"
	"github.com/addp/common/resume"
)

// CatalogModelProvider declares an engine's catalog shape and terminology.
type CatalogModelProvider interface {
	EnginePlugin
	CatalogModel() CatalogModelSpec
}

// CatalogProvider lists real catalog entries from an engine.
//
// ListChildren requires an explicit structural root path or a business branch
// path below that root. It must not treat an empty CatalogPath as "root"; callers
// that need the structural root entry should use CatalogRootEntry instead.
type CatalogProvider interface {
	EnginePlugin
	ListChildren(ctx context.Context, connInfo ConnectionInfo, parent CatalogPath, opts ListOptions) ([]CatalogEntry, error)
	ResolvePath(ctx context.Context, connInfo ConnectionInfo, path CatalogPath) (*CatalogEntry, error)
}

// CatalogFactsProvider describes engine-native facts for catalog leaves.
//
// DescribeCatalogFacts requires a business leaf path below the explicit
// structural root. Root paths and empty paths are not facts targets.
type CatalogFactsProvider interface {
	EnginePlugin
	DescribeCatalogFacts(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error)
}

// DynamicSchemaSamplingProvider samples schema-flexible catalog leaves to infer dynamic field info.
type DynamicSchemaSamplingProvider interface {
	EnginePlugin
	SampleDynamicSchema(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error)
}

// GraphSampleProvider samples graph nodes and relationships for lightweight previews.
type GraphSampleProvider interface {
	EnginePlugin
	SampleGraph(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts GraphSampleOptions) (*GraphData, error)
}

// StoreProvider is a marker for catalog-path content access capabilities.
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

// WatermarkCursor is a provider-owned composite position. Values are encoded as
// canonical strings and interpreted using the source column types.
type WatermarkCursor struct {
	Values []string `json:"values"`
}

type BoundedWatermarkReadOptions struct {
	WatermarkField string
	TieBreakers    []string
	Start          *WatermarkCursor
}

// BoundedWatermarkReadProvider opens a finite, consistently bounded table read.
type BoundedWatermarkReadProvider interface {
	StoreProvider
	OpenBoundedWatermarkRead(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts BoundedWatermarkReadOptions) (BoundedWatermarkReadSession, error)
}

type BoundedWatermarkReadSession interface {
	UpperBound() *WatermarkCursor
	TableInfo() (*datatype.TableInfo, *datatype.SpatialInfo)
	PositionForRow(row map[string]interface{}) (*WatermarkCursor, error)
	ReadBatch(ctx context.Context, limit int) (*BatchData, error)
	Close(ctx context.Context) error
}

// ResumeMarkerProvider is an optional capability implemented by read sessions
// that can expose a stable read marker after successful reads.
type ResumeMarkerProvider interface {
	ResumeMarker() *resume.Marker
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

// CommitMarkerProvider is an optional capability implemented by write sessions
// that can expose a stable commit marker after successful commits.
type CommitMarkerProvider interface {
	CommitMarker() *resume.Marker
}

type TableWritePreparer interface {
	StoreProvider
	PrepareTableWrite(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts TableWriteOptions) error
}

type TableUpsertOptions struct {
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
	Keys        []string
}

// TableUpsertProvider prepares and applies idempotent key-based table changes.
type TableUpsertProvider interface {
	StoreProvider
	PrepareTableUpsert(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts TableUpsertOptions) error
	UpsertBatch(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, batch *BatchData, opts TableUpsertOptions) error
}

const (
	ChangeStreamPositionTypeKafkaOffset = "kafka_offset"
	ChangeStreamPositionVersionV1       = "v1"
	TableChangeOperationUpsert          = "upsert"
	TableChangeOperationDelete          = "delete"
	TableChangeOperationSkip            = "skip"
	ChangeStreamInitialEarliest         = "earliest"
	ChangeStreamInitialLatest           = "latest"
)

// ChangeStreamPosition is a provider-owned partition position. Values use
// canonical strings so the provider can preserve exact native values.
type ChangeStreamPosition struct {
	Type      string            `json:"type"`
	Version   string            `json:"version"`
	Partition string            `json:"partition"`
	Values    map[string]string `json:"values"`
}

type ChangeRecordHeader struct {
	Key   string `json:"key"`
	Value []byte `json:"value"`
}

// ChangeRecord is an undecoded native record returned by a change stream.
type ChangeRecord struct {
	Topic     string               `json:"topic"`
	Partition string               `json:"partition"`
	Offset    int64                `json:"offset"`
	Timestamp time.Time            `json:"timestamp"`
	Headers   []ChangeRecordHeader `json:"headers,omitempty"`
	Key       []byte               `json:"key,omitempty"`
	Value     []byte               `json:"value"`
	Position  ChangeStreamPosition `json:"position"`
}

type ChangeRecordBatch struct {
	Records      []ChangeRecord                  `json:"records"`
	EndPositions map[string]ChangeStreamPosition `json:"end_positions"`
}

type ChangeStreamReadOptions struct {
	ConsumerGroup      string
	CommittedPositions map[string]ChangeStreamPosition
	InitialPosition    string
	PollTimeout        time.Duration
	MaxBytes           int
}

// ChangeStreamPositionRange describes the currently retained provider range
// for one partition. Earliest and Latest use the same position identity as
// records returned by OpenChangeStream; Latest is the next offset at the end
// of the partition, not the last record offset.
type ChangeStreamPositionRange struct {
	Partition string               `json:"partition"`
	Earliest  ChangeStreamPosition `json:"earliest"`
	Latest    ChangeStreamPosition `json:"latest"`
}

type ChangeStreamReaderProvider interface {
	StoreProvider
	OpenChangeStream(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts ChangeStreamReadOptions) (ChangeStreamReader, error)
}

type ChangeStreamReader interface {
	Poll(ctx context.Context, maxRecords int) (*ChangeRecordBatch, error)
	PositionRanges(ctx context.Context) ([]ChangeStreamPositionRange, error)
	Assignments() []string
	Pause(ctx context.Context, partitions []string) error
	Resume(ctx context.Context, partitions []string) error
	Close(ctx context.Context) error
}

type PartitionedTableChange struct {
	Operation string                 `json:"operation"`
	Position  ChangeStreamPosition   `json:"position"`
	Row       map[string]interface{} `json:"row"`
}

type PartitionedTableChangeBatch struct {
	Partition     string                   `json:"partition"`
	StartPosition ChangeStreamPosition     `json:"start_position"`
	EndPosition   ChangeStreamPosition     `json:"end_position"`
	Changes       []PartitionedTableChange `json:"changes"`
}

type PartitionedTableChangeApplyOptions struct {
	ApplyIdentity       string
	SourceIdentity      string
	Fields              []datatype.FieldInfo
	SpatialInfo         *datatype.SpatialInfo
	Keys                []string
	RequireTargetAbsent bool
}

type PartitionedTableChangeApplyResult struct {
	AppliedRecords int                  `json:"applied_records"`
	SkippedRecords int                  `json:"skipped_records"`
	Position       ChangeStreamPosition `json:"position"`
}

// PartitionedTableChangeApplyProvider atomically applies mapped changes and a
// monotonic target-side partition position in the target engine.
type PartitionedTableChangeApplyProvider interface {
	StoreProvider
	PreparePartitionedTableChangeApply(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts PartitionedTableChangeApplyOptions) error
	ApplyPartitionedTableChanges(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, batch *PartitionedTableChangeBatch, opts PartitionedTableChangeApplyOptions) (*PartitionedTableChangeApplyResult, error)
}

type QueryRuntimeProvider interface {
	EnginePlugin
	QueryLanguages() []string
	GenerateSampleQuery(ctx context.Context, connInfo ConnectionInfo, opts SampleQueryOptions) (query string, language string)
	ExecuteRuntimeQuery(ctx context.Context, connInfo ConnectionInfo, req QueryRequest) (*QueryResult, error)
}

type FederatedQueryRuntimeProvider interface {
	EnginePlugin
	QueryLanguages() []string
	ResolveSourceEngineIDs(query string, candidates []FederatedQuerySource) []uint
	ResolveObjectTableReferences(query string, candidates []FederatedQuerySource) []FederatedQueryObjectTableReference
	ExecuteFederatedQuery(ctx context.Context, runtimeConn ConnectionInfo, req FederatedQueryRequest) (*QueryResult, error)
}

type FederatedQuerySource struct {
	ID             uint
	Name           string
	EngineType     string
	LifecycleState string
}

type FederatedQueryObjectTableReference struct {
	SourceName string
	TableName  string
}

type SQLQueryRuntimeProvider interface {
	QueryRuntimeProvider
	SQLDialect() string
	ExecuteSQL(ctx context.Context, connInfo ConnectionInfo, sql string, opts QueryOptions) (*QueryResult, error)
}

// ParameterizedSQLQueryRuntimeProvider explicitly declares that QueryOptions.Args
// are bound by the driver instead of interpolated into SQL text.
type ParameterizedSQLQueryRuntimeProvider interface {
	SQLQueryRuntimeProvider
	SupportsParameterizedQueries() bool
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
	ListOperators(ctx context.Context, connInfo ConnectionInfo) ([]OperatorDescriptor, error)
	ExecuteWorkflow(ctx context.Context, connInfo ConnectionInfo, req WorkflowExecuteRequest) (*WorkflowExecuteResult, error)
	InvokeOperator(ctx context.Context, connInfo ConnectionInfo, operatorName string, req OperatorInvokeRequest) (*OperatorInvokeResult, error)
	GetExecutionStatus(ctx context.Context, connInfo ConnectionInfo, executionID string) (*WorkflowExecutionStatus, error)
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
	for i, segment := range p.Segments {
		if i == 0 && IsCatalogRootSegment(segment) {
			continue
		}
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

const (
	CatalogRoleBranch = "branch"
	CatalogRoleLeaf   = "leaf"
)

type CatalogEntry struct {
	Name      string               `json:"name"`
	Path      CatalogPath          `json:"path"`
	Term      string               `json:"term"`
	Kind      string               `json:"kind"`
	Role      string               `json:"role"`
	Table     *datatype.TableInfo  `json:"table,omitempty"`
	Storage   *CatalogStorageFacts `json:"storage,omitempty"`
	LeafCount *int                 `json:"leaf_count,omitempty"`
	UpdatedAt *time.Time           `json:"updated_at,omitempty"`
}

type ListOptions struct {
	Recursive bool
	Limit     int
	Offset    int
}

type CatalogFactsOptions struct {
	IncludeStatistics   bool
	IncludeIndexes      bool
	IncludeSpatialFacts bool
	IncludeSamples      bool
	SampleSize          int
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

type CatalogFacts struct {
	Path      CatalogPath           `json:"path"`
	Kind      string                `json:"kind"`
	Table     *datatype.TableInfo   `json:"table,omitempty"`
	Graph     *datatype.GraphInfo   `json:"graph,omitempty"`
	Topic     *TopicFacts           `json:"topic,omitempty"`
	Spatial   *datatype.SpatialInfo `json:"spatial,omitempty"`
	Storage   *CatalogStorageFacts  `json:"storage,omitempty"`
	Indexes   []IndexFacts          `json:"indexes,omitempty"`
	UpdatedAt *time.Time            `json:"updated_at,omitempty"`
}

type TopicFacts struct {
	PartitionCount    int                   `json:"partition_count"`
	ReplicationFactor int                   `json:"replication_factor"`
	Partitions        []TopicPartitionFacts `json:"partitions,omitempty"`
}

type TopicPartitionFacts struct {
	Partition      int32   `json:"partition"`
	Leader         int32   `json:"leader"`
	Replicas       []int32 `json:"replicas,omitempty"`
	ISR            []int32 `json:"isr,omitempty"`
	EarliestOffset int64   `json:"earliest_offset"`
	LatestOffset   int64   `json:"latest_offset"`
}

type CatalogStorageFacts struct {
	Name        string `json:"name,omitempty"`
	Path        string `json:"path,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	ETag        string `json:"etag,omitempty"`
	Extension   string `json:"extension,omitempty"`
	SizeBytes   *int64 `json:"size_bytes,omitempty"`
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
	ContentType  string
	Overwrite    bool
	UserMetadata map[string]string
}

type BatchReadOptions struct {
	Limit  int
	Offset int64
	Query  string
	Args   []interface{}
	Hints  map[string]interface{}
}

type TableReadSessionOptions struct {
	Query        string
	Hints        map[string]interface{}
	ResumeMarker *resume.Marker
}

const (
	TableReadHintGeometryEncoding        = "geometry_encoding"
	TableReadHintGeometryField           = "geometry_field"
	TableReadHintGeometryTargetSRID      = "geometry_target_srid"
	TableReadHintGeometryTransformPolicy = "geometry_transform_policy"
)

type BatchWriteOptions struct {
	Method string
}

type TableWriteSessionOptions struct {
	Method       string
	Fields       []datatype.FieldInfo
	SpatialInfo  *datatype.SpatialInfo
	ResumeMarker *resume.Marker
}

type TableWriteOptions struct {
	Fields      []datatype.FieldInfo
	SpatialInfo *datatype.SpatialInfo
}

type BatchData struct {
	Rows    []map[string]interface{} `json:"rows"`
	Fields  []datatype.FieldInfo     `json:"fields,omitempty"`
	Spatial *datatype.SpatialInfo    `json:"spatial,omitempty"`
	Hints   map[string]interface{}   `json:"hints,omitempty"`
	Offset  int64                    `json:"offset,omitempty"`
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

type FederatedQueryRequest struct {
	ExecutionID              string                       `json:"execution_id"`
	ExecutionAuthorizationID string                       `json:"execution_authorization_id"`
	SourceEngineIDs          []uint                       `json:"source_engine_ids"`
	ObjectTables             map[string]map[string]string `json:"object_tables,omitempty"`
	Query                    string                       `json:"query"`
	Language                 string                       `json:"language"`
	Options                  QueryOptions                 `json:"options"`
	CallerAccessToken        string                       `json:"-"`
}

type QueryOptions struct {
	EngineID   uint          `json:"engine_id,omitempty"`
	EngineType string        `json:"engine_type,omitempty"`
	Limit      int           `json:"limit,omitempty"`
	Offset     int           `json:"offset,omitempty"`
	Timeout    time.Duration `json:"timeout,omitempty"`
	ReadOnly   bool          `json:"read_only"`
	Args       []interface{} `json:"args,omitempty"`
}

type OperatorDescriptor struct {
	ID                  string                 `json:"id,omitempty"`
	Name                string                 `json:"name"`
	DisplayName         string                 `json:"display_name,omitempty"`
	EngineType          string                 `json:"engine_type,omitempty"`
	Type                string                 `json:"type,omitempty"`
	Category            string                 `json:"category,omitempty"`
	CategoryPath        []string               `json:"category_path,omitempty"`
	Description         string                 `json:"description,omitempty"`
	BriefDescription    string                 `json:"brief_description,omitempty"`
	DetailedDescription map[string]interface{} `json:"detailed_description,omitempty"`
	Parameters          []ParameterDescriptor  `json:"parameters,omitempty"`
	Inputs              []interface{}          `json:"inputs,omitempty"`
	OutputPorts         []OutputPortDescriptor `json:"output_ports,omitempty"`
	ExecutionModes      []string               `json:"execution_modes,omitempty"`
	Effects             []string               `json:"effects,omitempty"`
	Attributes          map[string]interface{} `json:"attributes,omitempty"`
}

type ParameterDescriptor struct {
	Name        string                         `json:"name"`
	Type        string                         `json:"type"`
	ParamType   string                         `json:"param_type,omitempty"`
	Required    bool                           `json:"required"`
	Default     interface{}                    `json:"default,omitempty"`
	Description string                         `json:"description,omitempty"`
	Enum        []string                       `json:"enum,omitempty"`
	Min         *float64                       `json:"min,omitempty"`
	Max         *float64                       `json:"max,omitempty"`
	Pattern     string                         `json:"pattern,omitempty"`
	ItemType    string                         `json:"item_type,omitempty"`
	Properties  map[string]ParameterDescriptor `json:"properties,omitempty"`
	DependsOn   string                         `json:"depends_on,omitempty"`
	ShowWhen    map[string]interface{}         `json:"show_when,omitempty"`
	Notes       string                         `json:"notes,omitempty"`
	UIType      string                         `json:"ui_type,omitempty"`
	UIConfig    map[string]interface{}         `json:"ui_config,omitempty"`
}

type OutputPortDescriptor struct {
	Name        string `json:"name"`
	Type        string `json:"type"`
	Description string `json:"description,omitempty"`
	IsDefault   bool   `json:"is_default"`
}

type WorkflowExecuteRequest struct {
	WorkflowDef map[string]interface{}  `json:"workflow_def"`
	InputData   map[string]interface{}  `json:"input_data,omitempty"`
	EngineID    uint                    `json:"engine_id,omitempty"`
	Runtime     *WorkflowRuntimeContext `json:"runtime,omitempty"`
	Timeout     time.Duration           `json:"-"`
}

type WorkflowRuntimeContext struct {
	ExecutionAuthorization WorkflowExecutionAuthorization `json:"execution_authorization"`
}

type WorkflowExecutionAuthorization struct {
	ID      int64    `json:"id"`
	Effects []string `json:"effects"`
}

type WorkflowExecuteResult struct {
	Status          string                 `json:"status"`
	ExecutionID     string                 `json:"execution_id,omitempty"`
	Result          map[string]interface{} `json:"result,omitempty"`
	Error           string                 `json:"error,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	Details         string                 `json:"details,omitempty"`
	ExecutionTimeMs *float64               `json:"execution_time_ms,omitempty"`
}

type OperatorInvokeRequest struct {
	Params        map[string]interface{} `json:"params,omitempty"`
	EngineID      uint                   `json:"engine_id,omitempty"`
	BinaryPayload *BinaryPayload         `json:"binary_payload,omitempty"`
	Timeout       time.Duration          `json:"-"`
}

type OperatorInvokeResult struct {
	Status          string                 `json:"status"`
	ExecutionID     string                 `json:"execution_id,omitempty"`
	Result          map[string]interface{} `json:"result,omitempty"`
	BinaryPayload   *BinaryPayload         `json:"binary_payload,omitempty"`
	Error           string                 `json:"error,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	Details         string                 `json:"details,omitempty"`
	ExecutionTimeMs *float64               `json:"execution_time_ms,omitempty"`
}

type BinaryPayload struct {
	ContentType string                 `json:"content_type,omitempty"`
	Encoding    string                 `json:"encoding,omitempty"`
	Name        string                 `json:"name,omitempty"`
	Data        []byte                 `json:"data,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type WorkflowExecutionStatus struct {
	Status          string                 `json:"status"`
	ExecutionID     string                 `json:"execution_id,omitempty"`
	Result          interface{}            `json:"result,omitempty"`
	AllResults      map[string]interface{} `json:"all_results,omitempty"`
	Message         string                 `json:"message,omitempty"`
	TaskOrder       []string               `json:"task_order,omitempty"`
	Error           string                 `json:"error,omitempty"`
	ErrorCode       string                 `json:"error_code,omitempty"`
	Details         string                 `json:"details,omitempty"`
	Progress        int                    `json:"progress,omitempty"`
	StartedAt       string                 `json:"started_at,omitempty"`
	ExecutionTimeMs *float64               `json:"execution_time_ms,omitempty"`
	Raw             map[string]interface{} `json:"raw,omitempty"`
}

type ScriptSessionRequest struct {
	Mode     string                 `json:"mode"`
	Language string                 `json:"language,omitempty"`
	Options  map[string]interface{} `json:"options,omitempty"`
}

type ScriptSession struct {
	ID       string                 `json:"id"`
	Endpoint string                 `json:"endpoint,omitempty"`
	Info     map[string]interface{} `json:"info,omitempty"`
}

// InteractiveScriptSessionRequest is the standard control-plane request for
// a short-lived, owner-proxied interactive script session.
type InteractiveScriptSessionRequest struct {
	SessionID            string `json:"session_id"`
	TenantID             uint   `json:"tenant_id"`
	UserID               uint   `json:"user_id"`
	TaskID               uint   `json:"task_id"`
	NotebookPath         string `json:"notebook_path"`
	Kernel               string `json:"kernel"`
	BasePath             string `json:"base_path"`
	TTLSeconds           int    `json:"ttl_seconds"`
	OwnerAPIEndpoint     string `json:"owner_api_endpoint"`
	OwnerCapabilityToken string `json:"owner_capability_token"`
}

// InteractiveScriptSession contains internal proxy facts. Callers must never
// expose Endpoint or RuntimeToken to a browser.
type InteractiveScriptSession struct {
	SessionID    string    `json:"session_id"`
	Endpoint     string    `json:"endpoint"`
	RuntimeToken string    `json:"runtime_token"`
	NotebookName string    `json:"notebook_name"`
	ExpiresAt    time.Time `json:"expires_at"`
}
