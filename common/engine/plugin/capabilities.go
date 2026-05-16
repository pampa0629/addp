package plugin

import "encoding/json"

const (
	CapabilitiesSchemaVersion = "engine.capabilities/v1"
	CatalogPathVersion        = "catalog.path/v1"
)

type EngineCapabilities struct {
	SchemaVersion string                 `json:"schema_version"`
	EngineType    string                 `json:"engine_type"`
	EngineFamily  string                 `json:"engine_family"`
	Storage       *StorageCapabilities   `json:"storage,omitempty"`
	Compute       *ComputeCapabilities   `json:"compute,omitempty"`
	Transfer      *TransferCapabilities  `json:"transfer,omitempty"`
	Preview       *PreviewCapabilities   `json:"preview,omitempty"`
	Limits        map[string]interface{} `json:"limits,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
}

type StorageCapabilities struct {
	CatalogModel *CatalogModelSpec   `json:"catalog_model,omitempty"`
	Catalog      *CatalogCapability  `json:"catalog,omitempty"`
	Metadata     *MetadataCapability `json:"metadata,omitempty"`
	Store        *StoreCapability    `json:"store,omitempty"`
	Semantics    []string            `json:"semantics,omitempty"`
	NotSupported []string            `json:"not_supported,omitempty"`
}

type CatalogModelSpec struct {
	PathVersion string             `json:"path_version"`
	RootTerm    string             `json:"root_term"`
	Levels      []CatalogLevelSpec `json:"levels"`
}

type CatalogLevelSpec struct {
	Term      string   `json:"term"`
	Kinds     []string `json:"kinds"`
	Container bool     `json:"container"`
	Item      bool     `json:"item,omitempty"`
	Optional  bool     `json:"optional,omitempty"`
	I18nKey   string   `json:"i18n_key,omitempty"`
}

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

// CatalogNamespaceLevel 返回 catalog model 中第一层可展开的 namespace/container 定义。
func CatalogNamespaceLevel(model CatalogModelSpec) (CatalogLevelSpec, bool) {
	for _, level := range model.Levels {
		if level.Container {
			return level, true
		}
	}
	return CatalogLevelSpec{}, false
}

// CatalogItemTerm 返回 catalog model 中声明的 item 层术语。
func CatalogItemTerm(model CatalogModelSpec) string {
	for _, level := range model.Levels {
		if level.Item && level.Term != "" {
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

type MetadataCapability struct {
	Supported       bool `json:"supported"`
	FieldSchema     bool `json:"field_schema,omitempty"`
	Statistics      bool `json:"statistics,omitempty"`
	Indexes         bool `json:"indexes,omitempty"`
	Constraints     bool `json:"constraints,omitempty"`
	SpatialMetadata bool `json:"spatial_metadata,omitempty"`
	Sampling        bool `json:"sampling,omitempty"`
	NativeMetadata  bool `json:"native_metadata,omitempty"`
}

type StoreCapability struct {
	StreamRead        bool `json:"stream_read,omitempty"`
	StreamWrite       bool `json:"stream_write,omitempty"`
	RangeRead         bool `json:"range_read,omitempty"`
	RangeWrite        bool `json:"range_write,omitempty"`
	BatchRead         bool `json:"batch_read,omitempty"`
	TableReadSession  bool `json:"table_read_session,omitempty"`
	BatchWrite        bool `json:"batch_write,omitempty"`
	TableWriteSession bool `json:"table_write_session,omitempty"`
	TableWritePrepare bool `json:"table_write_prepare,omitempty"`
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

type TransferCapabilities struct {
	Read            bool              `json:"read"`
	Write           bool              `json:"write"`
	BulkWrite       bool              `json:"bulk_write,omitempty"`
	StreamRead      bool              `json:"stream_read,omitempty"`
	Checkpoint      bool              `json:"checkpoint,omitempty"`
	ParallelRead    bool              `json:"parallel_read,omitempty"`
	ParallelWrite   bool              `json:"parallel_write,omitempty"`
	ConnectorTypes  map[string]string `json:"connector_types,omitempty"`
	PreferredWriter string            `json:"preferred_writer,omitempty"`
}

type PreviewCapabilities struct {
	Supported     bool     `json:"supported"`
	Modes         []string `json:"modes"`
	MaxRows       int      `json:"max_rows,omitempty"`
	MaxBytes      int64    `json:"max_bytes,omitempty"`
	UsesComposer  bool     `json:"uses_composer,omitempty"`
	DirectPreview bool     `json:"direct_preview,omitempty"`
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
