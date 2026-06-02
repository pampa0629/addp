package plugin

func NewTabularCapabilities(engineType, namespaceTerm string, opts TabularCapabilityOptions) EngineCapabilities {
	if namespaceTerm == "" {
		namespaceTerm = "database"
	}
	if opts.DefaultLanguage == "" {
		opts.DefaultLanguage = "sql"
	}
	if opts.Description == "" {
		opts.Description = "SQL query"
	}

	caps := EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "tabular",
		Storage: &StorageCapabilities{
			CatalogModel: &CatalogModelSpec{
				PathVersion: CatalogPathVersion,
				RootTerm:    "server",
				Levels: []CatalogLevelSpec{
					{Term: namespaceTerm, Kinds: []string{"namespace"}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey(namespaceTerm)},
					{Term: "table", Kinds: []string{"table", "view", "materialized_view", "external_table"}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey("table")},
				},
			},
			Catalog: &CatalogCapability{
				Supported:       true,
				RealTime:        true,
				SystemFiltering: true,
				NodeKinds:       []string{"namespace", "table", "view", "materialized_view", "external_table"},
			},
			Facts: &CatalogFactsCapability{
				Supported:    true,
				FieldInfo:    true,
				Statistics:   true,
				Indexes:      true,
				Constraints:  true,
				SpatialFacts: opts.SpatialFacts,
				NativeFacts:  true,
			},
			Store: &StoreCapability{
				BatchRead:        true,
				TableReadSession: opts.TableReadSession,
				Delete:           opts.Delete,
			},
		},
		Compute: &ComputeCapabilities{
			Query: &QueryCapability{
				Supported:       true,
				Languages:       []string{"sql"},
				DefaultLanguage: opts.DefaultLanguage,
				ResultKinds:     []string{"table", "scalar"},
				SupportsExplain: opts.SupportsExplain,
				SupportsCancel:  opts.SupportsCancel,
			},
		},
	}

	if opts.BatchWrite {
		caps.Storage.Store.BatchWrite = true
	}
	if opts.TableWriteSession {
		caps.Storage.Store.TableWriteSession = true
	}
	if opts.TableWritePrepare {
		caps.Storage.Store.TableWritePrepare = true
	}
	if opts.Delete {
		caps.Storage.Store.Delete = true
	}

	return caps
}

type TabularCapabilityOptions struct {
	Write             bool
	BulkWrite         bool
	TableReadSession  bool
	BatchWrite        bool
	TableWriteSession bool
	TableWritePrepare bool
	Delete            bool
	SpatialFacts      bool
	SupportsExplain   bool
	SupportsCancel    bool
	DefaultLanguage   string
	Description       string
	WriterConnector   string
}

func NewObjectCapabilities(engineType string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "object",
		Storage: &StorageCapabilities{
			CatalogModel: &CatalogModelSpec{
				PathVersion: CatalogPathVersion,
				RootTerm:    "service",
				Levels: []CatalogLevelSpec{
					{Term: "bucket", Kinds: []string{"bucket"}, Role: CatalogRoleBranch, I18nKey: CatalogTermI18nKey("bucket")},
					{Term: "prefix", Kinds: []string{"prefix"}, Role: CatalogRoleBranch, Optional: true, I18nKey: CatalogTermI18nKey("prefix")},
					{Term: "object", Kinds: []string{"object"}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey("object")},
				},
			},
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"bucket", "prefix", "object"},
			},
			Facts: &CatalogFactsCapability{
				Supported:   true,
				NativeFacts: true,
			},
			Store: &StoreCapability{
				StreamRead:  true,
				RangeRead:   true,
				StreamWrite: true,
				Delete:      true,
			},
			Semantics:    []string{"bucket", "prefix_listing", "object", "stream_read", "range_read", "stream_write", "delete"},
			NotSupported: []string{"range_write", "real_directory"},
		},
	}
}

func NewFileCapabilities(engineType string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "file",
		Storage: &StorageCapabilities{
			CatalogModel: &CatalogModelSpec{
				PathVersion: CatalogPathVersion,
				RootTerm:    "root",
				Levels: []CatalogLevelSpec{
					{Term: "directory", Kinds: []string{"directory"}, Role: CatalogRoleBranch, Optional: true, I18nKey: CatalogTermI18nKey("directory")},
					{Term: "file", Kinds: []string{"file"}, Role: CatalogRoleLeaf, I18nKey: CatalogTermI18nKey("file")},
				},
			},
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"root", "directory", "file"},
			},
			Facts: &CatalogFactsCapability{
				Supported:   true,
				NativeFacts: true,
			},
			Store: &StoreCapability{
				StreamRead:  true,
				RangeRead:   true,
				StreamWrite: true,
				Delete:      true,
			},
			Semantics: []string{"root", "directory", "file", "stream_read", "range_read", "stream_write", "delete"},
		},
	}
}

func NewDynamicSchemaCapabilities(engineType string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "dynamic_schema",
		Storage: &StorageCapabilities{
			CatalogModel: PtrCatalogModel(DynamicSchemaCatalogModel()),
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"database", "collection"},
			},
			Facts: &CatalogFactsCapability{
				Supported:   true,
				FieldInfo:   true,
				Statistics:  true,
				Indexes:     true,
				Sampling:    true,
				NativeFacts: true,
			},
			Store: &StoreCapability{
				BatchRead: true,
			},
			Semantics: []string{"database", "collection", "dynamic_schema"},
		},
		Compute: &ComputeCapabilities{
			Query: &QueryCapability{
				Supported:       true,
				Languages:       []string{"mql"},
				DefaultLanguage: "mql",
				ResultKinds:     []string{"document", "table"},
			},
		},
	}
}

func NewGraphCapabilities(engineType string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "graph",
		Storage: &StorageCapabilities{
			CatalogModel: PtrCatalogModel(GraphCatalogModel()),
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"database", "graph"},
			},
			Facts: &CatalogFactsCapability{
				Supported:   true,
				NativeFacts: true,
			},
			Semantics: []string{"database", "graph"},
		},
		Compute: &ComputeCapabilities{
			Query: &QueryCapability{
				Supported:       true,
				Languages:       []string{"cypher"},
				DefaultLanguage: "cypher",
				ResultKinds:     []string{"graph", "table"},
			},
		},
	}
}

func NewWorkflowCapabilities(engineType, runtimeAPI string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "workflow",
		Compute: &ComputeCapabilities{
			Workflow: &WorkflowCapability{
				Supported:        true,
				RuntimeAPI:       runtimeAPI,
				DynamicOperators: true,
			},
		},
	}
}

func PtrCatalogModel(model CatalogModelSpec) *CatalogModelSpec {
	return &model
}

func StoreSemanticsFromCapabilities(caps EngineCapabilities) StoreSemantics {
	if caps.Storage == nil {
		return StoreSemantics{}
	}
	if len(caps.Storage.Semantics) == 0 && caps.Storage.Store != nil {
		return StoreSemantics{Semantics: storeCapabilitySemantics(caps.Storage.Store), NotSupported: caps.Storage.NotSupported}
	}
	return StoreSemantics{
		Semantics:    caps.Storage.Semantics,
		NotSupported: caps.Storage.NotSupported,
	}
}

func storeCapabilitySemantics(store *StoreCapability) []string {
	if store == nil {
		return nil
	}
	semantics := make([]string, 0, 7)
	if store.StreamRead {
		semantics = append(semantics, "stream_read")
	}
	if store.StreamWrite {
		semantics = append(semantics, "stream_write")
	}
	if store.RangeRead {
		semantics = append(semantics, "range_read")
	}
	if store.RangeWrite {
		semantics = append(semantics, "range_write")
	}
	if store.Delete {
		semantics = append(semantics, "delete")
	}
	if store.BatchRead {
		semantics = append(semantics, "batch_read")
	}
	if store.TableReadSession {
		semantics = append(semantics, "table_read_session")
	}
	if store.BatchWrite {
		semantics = append(semantics, "batch_write")
	}
	if store.TableWriteSession {
		semantics = append(semantics, "table_write_session")
	}
	if store.TableWritePrepare {
		semantics = append(semantics, "table_write_prepare")
	}
	return semantics
}

func NewScriptCapabilities(engineType string, modes, languages []string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "script",
		Compute: &ComputeCapabilities{
			Script: &ScriptCapability{
				Supported: true,
				Modes:     modes,
				Languages: languages,
			},
		},
	}
}
