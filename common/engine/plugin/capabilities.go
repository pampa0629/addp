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
	Limits        map[string]interface{} `json:"limits,omitempty"`
	Extensions    map[string]interface{} `json:"extensions,omitempty"`
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

// CatalogNamespaceLevel 返回 catalog model 中第一层可展开的 branch 定义。
func CatalogNamespaceLevel(model CatalogModelSpec) (CatalogLevelSpec, bool) {
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

// CatalogFirstBusinessBranch 返回 root 下第一层可展开的业务 branch。
func CatalogFirstBusinessBranch(model CatalogModelSpec) (CatalogLevelSpec, bool) {
	return CatalogNamespaceLevel(model)
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
	StreamRead        bool `json:"stream_read,omitempty"`
	StreamWrite       bool `json:"stream_write,omitempty"`
	RangeRead         bool `json:"range_read,omitempty"`
	RangeWrite        bool `json:"range_write,omitempty"`
	Delete            bool `json:"delete,omitempty"`
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
