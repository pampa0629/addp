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
			Facts: &CatalogFactsCapability{
				Supported:  true,
				FieldInfo:  true,
				Statistics: true,
				Sampling:   true,
			},
		},
	}
}

func (p *dynamicSchemaOnlyPlugin) SampleDynamicSchema(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts CatalogFactsOptions) (*CatalogFacts, error) {
	return &CatalogFacts{Path: path}, nil
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

func (p *spatialEncodingReadablePlugin) ReadBatch(context.Context, ConnectionInfo, CatalogPath, BatchReadOptions) (*BatchData, error) {
	return &BatchData{}, nil
}

type spatialFeatureReadablePlugin struct {
	spatialEncodingReadablePlugin
}

func (p *spatialFeatureReadablePlugin) ReadSpatialFeature(context.Context, ConnectionInfo, CatalogPath, SpatialFeatureReadOptions) (*SpatialFeatureData, error) {
	return &SpatialFeatureData{}, nil
}

type spatialEncodingWritablePlugin struct {
	spatialEncodingPlugin
}

func (p *spatialEncodingWritablePlugin) WriteBatch(context.Context, ConnectionInfo, CatalogPath, *BatchData, BatchWriteOptions) error {
	return nil
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
