package plugin

import (
	"context"
	"testing"
)

type dynamicSchemaOnlyPlugin struct {
	MockPlugin
}

func (p *dynamicSchemaOnlyPlugin) Capabilities() EngineCapabilities {
	return EngineCapabilities{
		SchemaVersion: CapabilitiesSchemaVersion,
		EngineType:    p.Type(),
		EngineFamily:  "document",
		Storage: &StorageCapabilities{
			Metadata: &MetadataCapability{
				Supported:  true,
				FieldInfo:  true,
				Statistics: true,
				Sampling:   true,
			},
		},
	}
}

func (p *dynamicSchemaOnlyPlugin) SampleDynamicSchema(ctx context.Context, connInfo ConnectionInfo, path CatalogPath, opts MetadataOptions) (*ItemMetadata, error) {
	return &ItemMetadata{Path: path}, nil
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
