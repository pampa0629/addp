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
			Families: []string{"tabular"},
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
				Read:         true,
				Write:        opts.Write,
				BatchRead:    true,
				BatchWrite:   opts.Write,
				Transactions: opts.Transactions,
				Formats:      []string{"table"},
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
			SupportedFormats: []string{"table"},
			PreferredWriter:  opts.WriterConnector,
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
	Transactions    bool
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
			Families: []string{"object"},
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
				Read:        true,
				Write:       true,
				StreamRead:  true,
				StreamWrite: true,
				RangeRead:   true,
				Formats:     []string{"csv", "geojson", "json", "parquet", "shapefile"},
			},
			Semantics:    []string{"bucket", "prefix_listing", "object", "stream_read", "range_read"},
			NotSupported: []string{"posix_random_write", "atomic_rename", "real_directory"},
		},
		Transfer: &TransferCapabilities{
			Read:  true,
			Write: true,
			ConnectorTypes: map[string]string{
				"reader": "s3",
				"writer": "s3",
			},
			SupportedFormats: []string{"csv", "geojson", "json", "parquet", "shapefile"},
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
	caps := NewObjectCapabilities(engineType)
	caps.EngineFamily = "file"
	caps.Storage.Families = []string{"file"}
	caps.Storage.CatalogModel = &CatalogModelSpec{
		PathVersion: CatalogPathVersion,
		RootTerm:    "root",
		Levels: []CatalogLevelSpec{
			{Term: "directory", Kinds: []string{"directory"}, Container: true},
			{Term: "file", Kinds: []string{"file"}, Item: true},
		},
	}
	caps.Storage.Catalog.NodeKinds = []string{"directory", "file"}
	caps.Storage.Semantics = []string{"directory", "file", "stream_read", "stream_write"}
	caps.Storage.NotSupported = nil
	caps.Transfer.ConnectorTypes = map[string]string{
		"reader": "nfs",
		"writer": "nfs",
	}
	caps.Preview.Modes = []string{"file_parse", "raw_text", "binary_metadata"}
	return caps
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
