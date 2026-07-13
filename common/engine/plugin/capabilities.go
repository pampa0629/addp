package plugin

import "encoding/json"

const (
	CapabilitiesSchemaVersion = "engine.capabilities/v1"
	CatalogPathVersion        = "catalog.path/v1"

	EngineExtensionSpatialWorkspaces = "spatial_workspaces"

	SpatialWorkspaceStateNotDetected      = "not_detected"
	SpatialWorkspaceStateDetected         = "detected"
	SpatialWorkspaceStateEnabled          = "enabled"
	SpatialWorkspaceStateUnavailable      = "unavailable"
	SpatialWorkspaceStatePermissionDenied = "permission_denied"

	SpatialWorkspaceRiskLow    = "low"
	SpatialWorkspaceRiskMedium = "medium"
	SpatialWorkspaceRiskHigh   = "high"
)

type EngineCapabilities struct {
	SchemaVersion string                 `json:"schema_version"`
	EngineType    string                 `json:"engine_type"`
	EngineFamily  string                 `json:"engine_family"`
	Storage       *StorageCapabilities   `json:"storage,omitempty"`
	Compute       *ComputeCapabilities   `json:"compute,omitempty"`
	Limits        map[string]interface{} `json:"limits,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
}

// SpatialWorkspaceFact describes a vendor/ecosystem-specific spatial workspace
// detected inside a general storage engine instance, such as SuperMap SDX+ or
// ArcGIS SDE on PostgreSQL. It lives under capabilities.extensions because it
// is an instance fact, not a core ADDP provider capability.
type SpatialWorkspaceFact struct {
	Ecosystem            string                 `json:"ecosystem"`
	Kind                 string                 `json:"kind"`
	State                string                 `json:"state"`
	BackendEngineType    string                 `json:"backend_engine_type,omitempty"`
	RuntimeEngineType    string                 `json:"runtime_engine_type,omitempty"`
	BoundRuntimeEngineID *uint                  `json:"bound_runtime_engine_id,omitempty"`
	CanEnable            bool                   `json:"can_enable,omitempty"`
	RiskLevel            string                 `json:"risk_level,omitempty"`
	Evidence             map[string]interface{} `json:"evidence,omitempty"`
}

func SetSpatialWorkspacesExtension(capabilities *EngineCapabilities, workspaces []SpatialWorkspaceFact) {
	if capabilities == nil || len(workspaces) == 0 {
		return
	}
	if capabilities.Extensions == nil {
		capabilities.Extensions = map[string]interface{}{}
	}
	capabilities.Extensions[EngineExtensionSpatialWorkspaces] = workspaces
}

func SpatialWorkspacesFromExtensions(extensions map[string]interface{}) ([]SpatialWorkspaceFact, error) {
	if len(extensions) == 0 {
		return nil, nil
	}
	raw, ok := extensions[EngineExtensionSpatialWorkspaces]
	if !ok || raw == nil {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var workspaces []SpatialWorkspaceFact
	if err := json.Unmarshal(data, &workspaces); err != nil {
		return nil, err
	}
	return workspaces, nil
}

type StorageCapabilities struct {
	CatalogModel *CatalogModelSpec       `json:"catalog_model,omitempty"`
	Catalog      *CatalogCapability      `json:"catalog,omitempty"`
	Facts        *CatalogFactsCapability `json:"facts,omitempty"`
	Store        *StoreCapability        `json:"store,omitempty"`
	Semantics    []string                `json:"semantics,omitempty"`
	NotSupported []string                `json:"not_supported,omitempty"`
}

type CatalogModelSpec struct {
	PathVersion string             `json:"path_version"`
	RootTerm    string             `json:"root_term"`
	Levels      []CatalogLevelSpec `json:"levels"`
}

type CatalogLevelSpec struct {
	Term     string   `json:"term"`
	Kinds    []string `json:"kinds"`
	Role     string   `json:"role"`
	Optional bool     `json:"optional,omitempty"`
	I18nKey  string   `json:"i18n_key,omitempty"`
}

const (
	CatalogTermServer = "server"
)

func CatalogTermI18nKey(term string) string {
	if term == "" {
		return ""
	}
	return "engine.term." + term
}

func CatalogLevelI18nKey(model CatalogModelSpec, term string) string {
	for _, level := range model.Levels {
		if level.Term == term {
			if level.I18nKey != "" {
				return level.I18nKey
			}
			return CatalogTermI18nKey(term)
		}
		for _, kind := range level.Kinds {
			if kind == term {
				return CatalogTermI18nKey(term)
			}
		}
	}
	return CatalogTermI18nKey(term)
}

// CatalogFirstBusinessBranch 返回 root 下第一层可展开的业务 branch。
func CatalogFirstBusinessBranch(model CatalogModelSpec) (CatalogLevelSpec, bool) {
	for _, level := range model.Levels {
		if level.Role == CatalogRoleBranch {
			return level, true
		}
	}
	return CatalogLevelSpec{}, false
}

// CatalogBusinessLevels 返回 root 下的业务层级定义。
func CatalogBusinessLevels(model CatalogModelSpec) []CatalogLevelSpec {
	return append([]CatalogLevelSpec(nil), model.Levels...)
}

// CatalogLeafTerm 返回 catalog model 中声明的 leaf 层术语。
func CatalogLeafTerm(model CatalogModelSpec) string {
	for _, level := range model.Levels {
		if level.Role == CatalogRoleLeaf && level.Term != "" {
			return level.Term
		}
	}
	return ""
}

type CatalogCapability struct {
	Supported       bool     `json:"supported"`
	RealTime        bool     `json:"real_time"`
	SupportsSearch  bool     `json:"supports_search,omitempty"`
	SupportsFilter  bool     `json:"supports_filter,omitempty"`
	SystemFiltering bool     `json:"system_filtering,omitempty"`
	NodeKinds       []string `json:"node_kinds,omitempty"`
}

type CatalogFactsCapability struct {
	Supported    bool `json:"supported"`
	FieldInfo    bool `json:"field_info,omitempty"`
	Statistics   bool `json:"statistics,omitempty"`
	Indexes      bool `json:"indexes,omitempty"`
	Constraints  bool `json:"constraints,omitempty"`
	SpatialFacts bool `json:"spatial_facts,omitempty"`
	Sampling     bool `json:"sampling,omitempty"`
	NativeFacts  bool `json:"native_facts,omitempty"`
}

type StoreCapability struct {
	StreamRead                  bool                                   `json:"stream_read,omitempty"`
	StreamWrite                 bool                                   `json:"stream_write,omitempty"`
	RangeRead                   bool                                   `json:"range_read,omitempty"`
	RangeWrite                  bool                                   `json:"range_write,omitempty"`
	Delete                      bool                                   `json:"delete,omitempty"`
	BatchRead                   bool                                   `json:"batch_read,omitempty"`
	TableReadSession            bool                                   `json:"table_read_session,omitempty"`
	TableReadSpatialTransform   bool                                   `json:"table_read_spatial_transform,omitempty"`
	BatchWrite                  bool                                   `json:"batch_write,omitempty"`
	TableWriteSession           bool                                   `json:"table_write_session,omitempty"`
	TableWritePrepare           bool                                   `json:"table_write_prepare,omitempty"`
	BoundedWatermarkRead        bool                                   `json:"bounded_watermark_read,omitempty"`
	ChangeStreamRead            *ChangeStreamReadCapability            `json:"change_stream_read,omitempty"`
	TableUpsert                 *TableUpsertCapability                 `json:"table_upsert,omitempty"`
	PartitionedTableChangeApply *PartitionedTableChangeApplyCapability `json:"partitioned_table_change_apply,omitempty"`
	TableSpatialEncoding        *NativeTableSpatialEncodingCapability  `json:"table_spatial_encoding,omitempty"`
}

type ChangeStreamReadCapability struct {
	Supported     bool     `json:"supported"`
	Partitioned   bool     `json:"partitioned"`
	Seek          bool     `json:"seek"`
	PauseResume   bool     `json:"pause_resume"`
	PositionTypes []string `json:"position_types"`
}

type TableUpsertCapability struct {
	Supported  bool `json:"supported"`
	Idempotent bool `json:"idempotent"`
}

type PartitionedTableChangeApplyCapability struct {
	Supported            bool     `json:"supported"`
	AtomicPositionCommit bool     `json:"atomic_position_commit"`
	Monotonic            bool     `json:"monotonic"`
	PositionTypes        []string `json:"position_types"`
	Operations           []string `json:"operations"`
}

// NativeTableSpatialEncodingCapability describes geometry row encodings that a
// native table provider can exchange across the ADDP table pipeline.
type NativeTableSpatialEncodingCapability struct {
	GeometryReadEncodings  []string `json:"geometry_read_encodings,omitempty"`
	GeometryWriteEncodings []string `json:"geometry_write_encodings,omitempty"`
	ReadTransform          bool     `json:"read_transform,omitempty"`
	WriteTransform         bool     `json:"write_transform,omitempty"`
	NativeSpatialFunctions bool     `json:"native_spatial_functions,omitempty"`
}

type ComputeCapabilities struct {
	Query    *QueryCapability    `json:"query,omitempty"`
	Workflow *WorkflowCapability `json:"workflow,omitempty"`
	Script   *ScriptCapability   `json:"script,omitempty"`
}

type QueryCapability struct {
	Supported       bool     `json:"supported"`
	Languages       []string `json:"languages"`
	DefaultLanguage string   `json:"default_language,omitempty"`
	ResultKinds     []string `json:"result_kinds,omitempty"`
	ReadOnly        bool     `json:"read_only,omitempty"`
	SupportsExplain bool     `json:"supports_explain,omitempty"`
	SupportsCancel  bool     `json:"supports_cancel,omitempty"`
}

type WorkflowCapability struct {
	Supported             bool     `json:"supported"`
	RuntimeAPI            string   `json:"runtime_api"`
	DynamicOperators      bool     `json:"dynamic_operators"`
	SupportedOperatorMode []string `json:"supported_operator_mode,omitempty"`
}

type ScriptCapability struct {
	Supported bool     `json:"supported"`
	Modes     []string `json:"modes"`
	Languages []string `json:"languages,omitempty"`
}

func MarshalEngineCapabilities(capabilities EngineCapabilities) (string, error) {
	if capabilities.SchemaVersion == "" {
		capabilities.SchemaVersion = CapabilitiesSchemaVersion
	}
	data, err := json.Marshal(capabilities)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func ParseEngineCapabilities(capabilitiesJSON string) (*EngineCapabilities, error) {
	if capabilitiesJSON == "" {
		return nil, nil
	}

	var capabilities EngineCapabilities
	if err := json.Unmarshal([]byte(capabilitiesJSON), &capabilities); err != nil {
		return nil, err
	}

	return &capabilities, nil
}
