package plugin

import (
	"fmt"
	"reflect"
)

// ValidatePluginCapabilities verifies that a plugin's declared capabilities
// match the providers it actually implements.
func ValidatePluginCapabilities(p EnginePlugin) error {
	if p == nil {
		return fmt.Errorf("plugin cannot be nil")
	}
	caps := p.Capabilities()
	if caps.SchemaVersion != CapabilitiesSchemaVersion {
		return fmt.Errorf("%s has invalid schema_version %q", p.Type(), caps.SchemaVersion)
	}
	if caps.EngineType != p.Type() {
		return fmt.Errorf("%s capabilities engine_type mismatch: %q", p.Type(), caps.EngineType)
	}
	if caps.EngineFamily == "" {
		return fmt.Errorf("%s capabilities engine_family is empty", p.Type())
	}

	if err := validateStorageCapabilities(p, caps.Storage); err != nil {
		return err
	}
	if err := validateComputeCapabilities(p, caps.Compute); err != nil {
		return err
	}
	return nil
}

func validateStorageCapabilities(p EnginePlugin, storage *StorageCapabilities) error {
	if storage == nil {
		return nil
	}
	if storage.CatalogModel != nil {
		provider, ok := p.(CatalogModelProvider)
		if !ok {
			return fmt.Errorf("%s declares storage.catalog_model but does not implement CatalogModelProvider", p.Type())
		}
		if !reflect.DeepEqual(*storage.CatalogModel, provider.CatalogModel()) {
			return fmt.Errorf("%s storage.catalog_model does not match CatalogModelProvider", p.Type())
		}
	}
	if storage.Catalog != nil && storage.Catalog.Supported {
		if _, ok := p.(CatalogProvider); !ok {
			return fmt.Errorf("%s declares catalog support but does not implement CatalogProvider", p.Type())
		}
	}
	if storage.Metadata != nil && storage.Metadata.Supported {
		if _, ok := p.(ItemMetadataProvider); !ok {
			if _, ok := p.(DynamicSchemaSamplingProvider); !ok {
				return fmt.Errorf("%s declares metadata support but does not implement metadata provider", p.Type())
			}
		}
	}
	if storage.Store != nil {
		if err := validateStoreCapabilities(p, storage.Store); err != nil {
			return err
		}
	}
	return nil
}

func validateStoreCapabilities(p EnginePlugin, store *StoreCapability) error {
	if store.StreamRead {
		if _, ok := p.(ContentReadableProvider); !ok {
			return fmt.Errorf("%s declares stream_read but does not implement ContentReadableProvider", p.Type())
		}
	}
	if store.StreamWrite {
		if _, ok := p.(ContentWritableProvider); !ok {
			return fmt.Errorf("%s declares stream_write but does not implement ContentWritableProvider", p.Type())
		}
	}
	if store.RangeRead {
		if _, ok := p.(RangeReadableProvider); !ok {
			return fmt.Errorf("%s declares range_read but does not implement RangeReadableProvider", p.Type())
		}
	}
	if store.RangeWrite {
		if _, ok := p.(RangeWritableProvider); !ok {
			return fmt.Errorf("%s declares range_write but does not implement RangeWritableProvider", p.Type())
		}
	}
	if store.Delete {
		if _, ok := p.(ResourceDeleteProvider); !ok {
			return fmt.Errorf("%s declares delete but does not implement ResourceDeleteProvider", p.Type())
		}
	}
	if store.BatchRead {
		if _, ok := p.(BatchReadableProvider); !ok {
			return fmt.Errorf("%s declares batch_read but does not implement BatchReadableProvider", p.Type())
		}
	}
	if store.TableReadSession {
		if _, ok := p.(TableReadSessionProvider); !ok {
			return fmt.Errorf("%s declares table_read_session but does not implement TableReadSessionProvider", p.Type())
		}
	}
	if store.BatchWrite {
		if _, ok := p.(BatchWritableProvider); !ok {
			return fmt.Errorf("%s declares batch_write but does not implement BatchWritableProvider", p.Type())
		}
	}
	if store.TableWriteSession {
		if _, ok := p.(TableWriteSessionProvider); !ok {
			return fmt.Errorf("%s declares table_write_session but does not implement TableWriteSessionProvider", p.Type())
		}
	}
	if store.TableWritePrepare {
		if _, ok := p.(TableWritePreparer); !ok {
			return fmt.Errorf("%s declares table_write_prepare but does not implement TableWritePreparer", p.Type())
		}
	}
	return nil
}

func validateComputeCapabilities(p EnginePlugin, compute *ComputeCapabilities) error {
	if compute == nil {
		return nil
	}
	if compute.Query != nil && compute.Query.Supported {
		if _, ok := p.(QueryRuntimeProvider); !ok {
			return fmt.Errorf("%s declares query support but does not implement QueryRuntimeProvider", p.Type())
		}
		if Contains(compute.Query.ResultKinds, "graph") {
			if _, ok := p.(GraphQueryProvider); !ok {
				return fmt.Errorf("%s declares graph query result kinds but does not implement GraphQueryProvider", p.Type())
			}
		}
	}
	if compute.Workflow != nil && compute.Workflow.Supported {
		if _, ok := p.(WorkflowRuntimeProvider); !ok {
			return fmt.Errorf("%s declares workflow support but does not implement WorkflowRuntimeProvider", p.Type())
		}
	}
	if compute.Script != nil && compute.Script.Supported {
		if _, ok := p.(ScriptRuntimeProvider); !ok {
			return fmt.Errorf("%s declares script support but does not implement ScriptRuntimeProvider", p.Type())
		}
	}
	return nil
}
