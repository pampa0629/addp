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
					{Term: namespaceTerm, Kinds: []string{"namespace"}, Container: true},
					{Term: "table", Kinds: []string{"table", "view", "materialized_view", "external_table"}, Item: true},
				},
			},
			Catalog: &CatalogCapability{
				Supported:       true,
				RealTime:        true,
				SystemFiltering: true,
				NodeKinds:       []string{"namespace", "table", "view", "materialized_view", "external_table"},
			},
			Metadata: &MetadataCapability{
				Supported:       true,
				FieldSchema:     true,
				Statistics:      true,
				Indexes:         true,
				Constraints:     true,
				SpatialMetadata: opts.SpatialMetadata,
				NativeMetadata:  true,
			},
			Store: &StoreCapability{
				BatchRead: true,
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
		Transfer: &TransferCapabilities{
			Read:       true,
			Write:      opts.Write,
			BulkWrite:  opts.BulkWrite,
			Checkpoint: true,
			ConnectorTypes: map[string]string{
				"reader": "jdbc",
				"writer": opts.WriterConnector,
			},
			PreferredWriter: opts.WriterConnector,
		},
		Preview: &PreviewCapabilities{
			Supported:    true,
			Modes:        []string{"tabular_rows"},
			MaxRows:      1000,
			UsesComposer: true,
		},
	}

	if caps.Transfer.ConnectorTypes["writer"] == "" {
		caps.Transfer.ConnectorTypes["writer"] = "jdbc"
		caps.Transfer.PreferredWriter = "jdbc"
	}
	if !opts.Write {
		caps.Transfer.Write = false
		caps.Transfer.BulkWrite = false
		caps.Transfer.PreferredWriter = ""
		delete(caps.Transfer.ConnectorTypes, "writer")
	}

	return caps
}

type TabularCapabilityOptions struct {
	Write           bool
	BulkWrite       bool
	SpatialMetadata bool
	SupportsExplain bool
	SupportsCancel  bool
	DefaultLanguage string
	Description     string
	WriterConnector string
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
					{Term: "bucket", Kinds: []string{"bucket"}, Container: true},
					{Term: "prefix", Kinds: []string{"prefix"}, Container: true, Optional: true},
					{Term: "object", Kinds: []string{"object"}, Item: true},
				},
			},
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"bucket", "prefix", "object"},
			},
			Metadata: &MetadataCapability{
				Supported:      true,
				NativeMetadata: true,
			},
			Store: &StoreCapability{
				StreamRead: true,
				RangeRead:  true,
			},
			Semantics:    []string{"bucket", "prefix_listing", "object", "stream_read", "range_read"},
			NotSupported: []string{"range_write", "real_directory"},
		},
		Transfer: &TransferCapabilities{
			Read:  true,
			Write: true,
			ConnectorTypes: map[string]string{
				"reader": "s3",
				"writer": "s3",
			},
		},
		Preview: &PreviewCapabilities{
			Supported:    true,
			Modes:        []string{"object_parse", "raw_text", "binary_metadata"},
			MaxBytes:     10 * 1024 * 1024,
			UsesComposer: true,
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
					{Term: "directory", Kinds: []string{"directory"}, Container: true, Optional: true},
					{Term: "file", Kinds: []string{"file"}, Item: true},
				},
			},
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"root", "directory", "file"},
			},
			Metadata: &MetadataCapability{
				Supported:      true,
				NativeMetadata: true,
			},
			Store: &StoreCapability{
				StreamRead:  true,
				RangeRead:   true,
				StreamWrite: true,
			},
			Semantics: []string{"root", "directory", "file", "stream_read", "range_read", "stream_write"},
		},
		Transfer: &TransferCapabilities{
			Read:  true,
			Write: true,
			ConnectorTypes: map[string]string{
				"reader": "nfs",
				"writer": "nfs",
			},
		},
		Preview: &PreviewCapabilities{
			Supported:    true,
			Modes:        []string{"file_parse", "raw_text", "binary_metadata"},
			MaxBytes:     10 * 1024 * 1024,
			UsesComposer: true,
		},
	}
}

func NewDocumentCapabilities(engineType string) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "document",
		Storage: &StorageCapabilities{
			CatalogModel: PtrCatalogModel(DocumentCatalogModel()),
			Catalog: &CatalogCapability{
				Supported: true,
				RealTime:  true,
				NodeKinds: []string{"database", "collection"},
			},
			Metadata: &MetadataCapability{
				Supported:      true,
				FieldSchema:    true,
				Statistics:     true,
				Indexes:        true,
				Sampling:       true,
				NativeMetadata: true,
			},
			Store: &StoreCapability{
				BatchRead: true,
			},
			Semantics: []string{"database", "collection", "document"},
		},
		Compute: &ComputeCapabilities{
			Query: &QueryCapability{
				Supported:       true,
				Languages:       []string{"mql"},
				DefaultLanguage: "mql",
				ResultKinds:     []string{"document", "table"},
			},
		},
		Transfer: &TransferCapabilities{
			Read:      true,
			Write:     true,
			BulkWrite: true,
		},
		Preview: &PreviewCapabilities{
			Supported:    true,
			Modes:        []string{"document_samples"},
			MaxRows:      1000,
			UsesComposer: true,
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
				NodeKinds: []string{"database", "label", "relationship"},
			},
			Metadata: &MetadataCapability{
				Supported:      true,
				NativeMetadata: true,
			},
			Store: &StoreCapability{
				BatchRead: true,
			},
			Semantics: []string{"database", "label", "relationship", "node", "edge"},
		},
		Compute: &ComputeCapabilities{
			Query: &QueryCapability{
				Supported:       true,
				Languages:       []string{"cypher"},
				DefaultLanguage: "cypher",
				ResultKinds:     []string{"graph", "table"},
			},
		},
		Preview: &PreviewCapabilities{
			Supported:    true,
			Modes:        []string{"graph_sample"},
			MaxRows:      1000,
			UsesComposer: true,
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
	return StoreSemantics{
		Semantics:    caps.Storage.Semantics,
		NotSupported: caps.Storage.NotSupported,
	}
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
