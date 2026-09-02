package plugin

import (
	"context"
	"strings"
	"testing"
)

type dynamicSchemaOnlyPlugin struct {
	MockPlugin
}

func (p *dynamicSchemaOnlyPlugin) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    p.Type(),
		EngineFamily:  "dynamic_schema",
		Storage: &StorageCapabilities{
			Facts: &EngineCatalogFactsCapability{
				Supported:  true,
				FieldInfo:  true,
				Statistics: true,
				Sampling:   true,
			},
		},
	}
}

func (p *dynamicSchemaOnlyPlugin) SampleDynamicSchema(ctx context.Context, connInfo ConnectionInfo, path EngineCatalogPath, opts EngineCatalogFactsOptions) (*EngineCatalogFacts, error) {
	return &EngineCatalogFacts{Path: path}, nil
}

func TestValidatePluginCapabilitiesAcceptsDynamicSchemaSamplingProvider(t *testing.T) {
	t.Parallel()

	plugin := &dynamicSchemaOnlyPlugin{
		MockPlugin: MockPlugin{
			TypeValue:        "dynamic_schema",
			DisplayNameValue: "Dynamic Schema",
		},
	}

	if err := ValidatePluginCapabilities(plugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

type spatialEncodingPlugin struct {
	MockPlugin
	caps EngineCapabilities
}

func (p *spatialEncodingPlugin) Capabilities() EngineCapabilities {
	return p.caps
}

func (p *spatialEncodingPlugin) StoreSemantics() StoreSemantics {
	return StoreSemanticsFromCapabilities(p.caps)
}

type spatialEncodingReadablePlugin struct {
	spatialEncodingPlugin
}

func (p *spatialEncodingReadablePlugin) ReadBatch(context.Context, ConnectionInfo, EngineCatalogPath, BatchReadOptions) (*BatchData, error) {
	return &BatchData{}, nil
}

type spatialFeatureReadablePlugin struct {
	spatialEncodingReadablePlugin
}

func (p *spatialFeatureReadablePlugin) ReadSpatialFeature(context.Context, ConnectionInfo, EngineCatalogPath, SpatialFeatureReadOptions) (*SpatialFeatureData, error) {
	return &SpatialFeatureData{}, nil
}

type spatialEncodingWritablePlugin struct {
	spatialEncodingPlugin
}

func (p *spatialEncodingWritablePlugin) WriteBatch(context.Context, ConnectionInfo, EngineCatalogPath, *BatchData, BatchWriteOptions) error {
	return nil
}

type recordReadSessionPlugin struct {
	spatialEncodingPlugin
}

func (p *recordReadSessionPlugin) OpenRecordReadSession(context.Context, ConnectionInfo, EngineCatalogPath, RecordReadSessionOptions) (RecordReadSession, error) {
	return nil, nil
}

type encodedRecordReadSessionPlugin struct {
	spatialEncodingPlugin
}

func (p *encodedRecordReadSessionPlugin) OpenEncodedRecordReadSession(context.Context, ConnectionInfo, EngineCatalogPath, EncodedRecordReadSessionOptions) (EncodedRecordReadSession, error) {
	return nil, nil
}

func TestValidatePluginCapabilitiesAcceptsRecordReadSessionDeclaration(t *testing.T) {
	plugin := &recordReadSessionPlugin{spatialEncodingPlugin: spatialEncodingPlugin{
		MockPlugin: MockPlugin{TypeValue: "record_reader"},
		caps: EngineCapabilities{
			SchemaVersion: CapabilitiesSchemaVersion,
			EngineType:    "record_reader",
			EngineFamily:  "dynamic_schema",
			Storage: &StorageCapabilities{Store: &StoreCapability{
				RecordReadSession: true,
			}},
		},
	}}

	if err := ValidatePluginCapabilities(plugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func TestValidatePluginCapabilitiesRejectsRecordReadSessionMismatch(t *testing.T) {
	t.Run("declared_without_provider", func(t *testing.T) {
		plugin := &spatialEncodingPlugin{
			MockPlugin: MockPlugin{TypeValue: "declared_record_reader"},
			caps: EngineCapabilities{
				SchemaVersion: CapabilitiesSchemaVersion,
				EngineType:    "declared_record_reader",
				EngineFamily:  "dynamic_schema",
				Storage: &StorageCapabilities{Store: &StoreCapability{
					RecordReadSession: true,
				}},
			},
		}
		err := ValidatePluginCapabilities(plugin)
		if err == nil || !strings.Contains(err.Error(), "does not implement RecordReadSessionProvider") {
			t.Fatalf("ValidatePluginCapabilities() error = %v, want missing provider error", err)
		}
	})

	t.Run("provider_without_declaration", func(t *testing.T) {
		plugin := &recordReadSessionPlugin{spatialEncodingPlugin: spatialEncodingPlugin{
			MockPlugin: MockPlugin{TypeValue: "undeclared_record_reader"},
			caps: EngineCapabilities{
				SchemaVersion: CapabilitiesSchemaVersion,
				EngineType:    "undeclared_record_reader",
				EngineFamily:  "dynamic_schema",
				Storage:       &StorageCapabilities{Store: &StoreCapability{}},
			},
		}}
		err := ValidatePluginCapabilities(plugin)
		if err == nil || !strings.Contains(err.Error(), "does not declare record_read_session") {
			t.Fatalf("ValidatePluginCapabilities() error = %v, want missing declaration error", err)
		}
	})
}

func TestValidatePluginCapabilitiesValidatesEncodedRecordReadSession(t *testing.T) {
	plugin := &encodedRecordReadSessionPlugin{spatialEncodingPlugin: spatialEncodingPlugin{
		MockPlugin: MockPlugin{TypeValue: "encoded_record_reader"},
		caps: EngineCapabilities{
			SchemaVersion: CapabilitiesSchemaVersion,
			EngineType:    "encoded_record_reader",
			EngineFamily:  "dynamic_schema",
			Storage: &StorageCapabilities{Store: &StoreCapability{
				EncodedRecordReadSession: &EncodedRecordReadSessionCapability{Formats: []string{"mongodb_extended_jsonl"}},
			}},
		},
	}}
	if err := ValidatePluginCapabilities(plugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}

	plugin.caps.Storage.Store.EncodedRecordReadSession.Formats = nil
	if err := ValidatePluginCapabilities(plugin); err == nil || !strings.Contains(err.Error(), "formats are required") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want formats error", err)
	}
}

func TestValidatePluginCapabilitiesRejectsSpatialEncodingWithoutReader(t *testing.T) {
	plugin := &spatialEncodingPlugin{
		MockPlugin: MockPlugin{TypeValue: "spatial_read_only"},
		caps:       spatialEncodingCapabilities("spatial_read_only", &NativeTableSpatialEncodingCapability{GeometryReadEncodings: []string{"ewkb"}}),
	}

	err := ValidatePluginCapabilities(plugin)
	if err == nil || !strings.Contains(err.Error(), "table_spatial_encoding read capability") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want spatial read provider error", err)
	}
}

func TestValidatePluginCapabilitiesRejectsSpatialEncodingWithoutWriter(t *testing.T) {
	plugin := &spatialEncodingPlugin{
		MockPlugin: MockPlugin{TypeValue: "spatial_write_only"},
		caps:       spatialEncodingCapabilities("spatial_write_only", &NativeTableSpatialEncodingCapability{GeometryWriteEncodings: []string{"ewkb"}}),
	}

	err := ValidatePluginCapabilities(plugin)
	if err == nil || !strings.Contains(err.Error(), "table_spatial_encoding write capability") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want spatial write provider error", err)
	}
}

func TestValidatePluginCapabilitiesAcceptsSpatialEncodingWithMatchingProvider(t *testing.T) {
	readPlugin := &spatialEncodingReadablePlugin{
		spatialEncodingPlugin: spatialEncodingPlugin{
			MockPlugin: MockPlugin{TypeValue: "spatial_reader"},
			caps: spatialEncodingCapabilities("spatial_reader", &NativeTableSpatialEncodingCapability{
				GeometryReadEncodings: []string{"ewkb"},
				ReadTransform:         true,
			}),
		},
	}
	readPlugin.caps.Storage.Store.BatchRead = true
	if err := ValidatePluginCapabilities(readPlugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities(readPlugin) error = %v", err)
	}

	writePlugin := &spatialEncodingWritablePlugin{
		spatialEncodingPlugin: spatialEncodingPlugin{
			MockPlugin: MockPlugin{TypeValue: "spatial_writer"},
			caps: spatialEncodingCapabilities("spatial_writer", &NativeTableSpatialEncodingCapability{
				GeometryWriteEncodings: []string{"ewkb"},
			}),
		},
	}
	writePlugin.caps.Storage.Store.BatchWrite = true
	if err := ValidatePluginCapabilities(writePlugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities(writePlugin) error = %v", err)
	}
}

func TestValidatePluginCapabilitiesRejectsNativeSpatialFunctionsWithoutFeatureReader(t *testing.T) {
	plugin := &spatialEncodingReadablePlugin{
		spatialEncodingPlugin: spatialEncodingPlugin{
			MockPlugin: MockPlugin{TypeValue: "native_spatial_without_feature_reader"},
			caps: spatialEncodingCapabilities("native_spatial_without_feature_reader", &NativeTableSpatialEncodingCapability{
				GeometryReadEncodings:  []string{"ewkb"},
				NativeSpatialFunctions: true,
			}),
		},
	}
	plugin.caps.Storage.Store.BatchRead = true

	err := ValidatePluginCapabilities(plugin)
	if err == nil || !strings.Contains(err.Error(), "SpatialFeatureReadProvider") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want spatial feature reader error", err)
	}
}

func TestValidatePluginCapabilitiesRejectsSpatialFeatureReaderWithoutDeclaration(t *testing.T) {
	plugin := &spatialFeatureReadablePlugin{
		spatialEncodingReadablePlugin: spatialEncodingReadablePlugin{
			spatialEncodingPlugin: spatialEncodingPlugin{
				MockPlugin: MockPlugin{TypeValue: "undeclared_spatial_feature_reader"},
				caps: spatialEncodingCapabilities("undeclared_spatial_feature_reader", &NativeTableSpatialEncodingCapability{
					GeometryReadEncodings: []string{"ewkb"},
				}),
			},
		},
	}
	plugin.caps.Storage.Store.BatchRead = true

	err := ValidatePluginCapabilities(plugin)
	if err == nil || !strings.Contains(err.Error(), "does not declare native_spatial_functions") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want undeclared native spatial functions error", err)
	}
}

func TestValidatePluginCapabilitiesAcceptsNativeSpatialFunctionsWithFeatureReader(t *testing.T) {
	plugin := &spatialFeatureReadablePlugin{
		spatialEncodingReadablePlugin: spatialEncodingReadablePlugin{
			spatialEncodingPlugin: spatialEncodingPlugin{
				MockPlugin: MockPlugin{TypeValue: "native_spatial_feature_reader"},
				caps: spatialEncodingCapabilities("native_spatial_feature_reader", &NativeTableSpatialEncodingCapability{
					GeometryReadEncodings:  []string{"ewkb"},
					NativeSpatialFunctions: true,
				}),
			},
		},
	}
	plugin.caps.Storage.Store.BatchRead = true

	if err := ValidatePluginCapabilities(plugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func spatialEncodingCapabilities(engineType string, spatial *NativeTableSpatialEncodingCapability) EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    engineType,
		EngineFamily:  "test",
		Storage: &StorageCapabilities{
			Store: &StoreCapability{
				TableSpatialEncoding: spatial,
			},
		},
	}
}

type queryParameterCapabilityPlugin struct {
	MockPlugin
	caps EngineCapabilities
}

func (p *queryParameterCapabilityPlugin) Capabilities() EngineCapabilities { return p.caps }
func (p *queryParameterCapabilityPlugin) QueryLanguages() []string         { return []string{"sql"} }
func (p *queryParameterCapabilityPlugin) GenerateSampleQuery(context.Context, ConnectionInfo, SampleQueryOptions) (string, string) {
	return "", "sql"
}
func (p *queryParameterCapabilityPlugin) PrepareQuery(context.Context, ConnectionInfo, QueryRequest) (PreparedQuery, error) {
	analysis, err := NewQueryAnalysis("sql", QuerySchemaCoverageUnknown)
	if err != nil {
		return nil, err
	}
	return NewPreparedQuery(analysis, nil, nil, func(context.Context) (*QueryResult, error) { return &QueryResult{}, nil })
}

func TestValidatePluginCapabilitiesAcceptsQueryParameters(t *testing.T) {
	plugin := &queryParameterCapabilityPlugin{
		MockPlugin: MockPlugin{TypeValue: "parameterized_query"},
		caps: EngineCapabilities{
			SchemaVersion: CapabilitiesSchemaVersion,
			EngineType:    "parameterized_query",
			EngineFamily:  "test",
			Compute: &ComputeCapabilities{Query: &QueryCapability{
				Supported: true,
				Languages: []string{"sql"},
				Parameters: &QueryParameterCapability{
					Supported: true,
					Languages: []string{"sql"},
					Types:     []string{"string", "integer", "number", "boolean"},
				},
			}},
		},
	}

	if err := ValidatePluginCapabilities(plugin); err != nil {
		t.Fatalf("ValidatePluginCapabilities() error = %v", err)
	}
}

func TestValidatePluginCapabilitiesRejectsInvalidQueryParameters(t *testing.T) {
	plugin := &queryParameterCapabilityPlugin{
		MockPlugin: MockPlugin{TypeValue: "invalid_parameterized_query"},
		caps: EngineCapabilities{
			SchemaVersion: CapabilitiesSchemaVersion,
			EngineType:    "invalid_parameterized_query",
			EngineFamily:  "test",
			Compute: &ComputeCapabilities{Query: &QueryCapability{
				Supported: true,
				Languages: []string{"sql"},
				Parameters: &QueryParameterCapability{
					Supported: true,
					Languages: []string{"cypher"},
					Types:     []string{"object"},
				},
			}},
		},
	}

	err := ValidatePluginCapabilities(plugin)
	if err == nil || !strings.Contains(err.Error(), "unsupported language") {
		t.Fatalf("ValidatePluginCapabilities() error = %v, want unsupported language error", err)
	}
}
