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
	if err := validateProviderCapabilities(p, caps); err != nil {
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
	if storage.Facts != nil && storage.Facts.Supported {
		if _, ok := p.(CatalogFactsProvider); !ok {
			if _, ok := p.(DynamicSchemaSamplingProvider); !ok {
				return fmt.Errorf("%s declares facts support but does not implement CatalogFactsProvider or DynamicSchemaSamplingProvider", p.Type())
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
	if store.TableReadSpatialTransform && !implementsNativeTableReader(p) {
		return fmt.Errorf("%s declares table_read_spatial_transform but does not implement native table read provider", p.Type())
	}
	if store.TableSpatialEncoding != nil {
		if err := validateTableSpatialEncodingCapability(p, store.TableSpatialEncoding); err != nil {
			return err
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

func validateTableSpatialEncodingCapability(p EnginePlugin, spatial *NativeTableSpatialEncodingCapability) error {
	if spatial == nil {
		return nil
	}
	if (len(spatial.GeometryReadEncodings) > 0 || spatial.ReadTransform || spatial.NativeSpatialFunctions) && !implementsNativeTableReader(p) {
		return fmt.Errorf("%s declares table_spatial_encoding read capability but does not implement native table read provider", p.Type())
	}
	if (len(spatial.GeometryWriteEncodings) > 0 || spatial.WriteTransform) && !implementsNativeTableWriter(p) {
		return fmt.Errorf("%s declares table_spatial_encoding write capability but does not implement native table write provider", p.Type())
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

func validateProviderCapabilities(p EnginePlugin, caps EngineCapabilities) error {
	if _, ok := p.(CatalogModelProvider); ok {
		if caps.Storage == nil || caps.Storage.CatalogModel == nil {
			return fmt.Errorf("%s implements CatalogModelProvider but does not declare storage.catalog_model", p.Type())
		}
	}
	if _, ok := p.(CatalogProvider); ok {
		if caps.Storage == nil || caps.Storage.Catalog == nil || !caps.Storage.Catalog.Supported {
			return fmt.Errorf("%s implements CatalogProvider but does not declare catalog support", p.Type())
		}
	}
	if _, ok := p.(CatalogFactsProvider); ok {
		if caps.Storage == nil || caps.Storage.Facts == nil || !caps.Storage.Facts.Supported {
			return fmt.Errorf("%s implements CatalogFactsProvider but does not declare facts support", p.Type())
		}
	}
	if _, ok := p.(DynamicSchemaSamplingProvider); ok {
		if caps.Storage == nil || caps.Storage.Facts == nil || !caps.Storage.Facts.Supported || !caps.Storage.Facts.Sampling {
			return fmt.Errorf("%s implements DynamicSchemaSamplingProvider but does not declare facts sampling support", p.Type())
		}
	}
	if err := validateStoreProviderCapabilities(p, caps.Storage); err != nil {
		return err
	}
	if _, ok := p.(QueryRuntimeProvider); ok {
		if caps.Compute == nil || caps.Compute.Query == nil || !caps.Compute.Query.Supported {
			return fmt.Errorf("%s implements QueryRuntimeProvider but does not declare query support", p.Type())
		}
	}
	if _, ok := p.(GraphQueryProvider); ok {
		if caps.Compute == nil || caps.Compute.Query == nil || !caps.Compute.Query.Supported || !Contains(caps.Compute.Query.ResultKinds, "graph") {
			return fmt.Errorf("%s implements GraphQueryProvider but does not declare graph query result kind", p.Type())
		}
	}
	if _, ok := p.(WorkflowRuntimeProvider); ok {
		if caps.Compute == nil || caps.Compute.Workflow == nil || !caps.Compute.Workflow.Supported {
			return fmt.Errorf("%s implements WorkflowRuntimeProvider but does not declare workflow support", p.Type())
		}
	}
	if _, ok := p.(ScriptRuntimeProvider); ok {
		if caps.Compute == nil || caps.Compute.Script == nil || !caps.Compute.Script.Supported {
			return fmt.Errorf("%s implements ScriptRuntimeProvider but does not declare script support", p.Type())
		}
	}
	return nil
}

func validateStoreProviderCapabilities(p EnginePlugin, storage *StorageCapabilities) error {
	if _, ok := p.(ContentReadableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.StreamRead }) {
		return fmt.Errorf("%s implements ContentReadableProvider but does not declare stream_read", p.Type())
	}
	if _, ok := p.(ContentWritableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.StreamWrite }) {
		return fmt.Errorf("%s implements ContentWritableProvider but does not declare stream_write", p.Type())
	}
	if _, ok := p.(RangeReadableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.RangeRead }) {
		return fmt.Errorf("%s implements RangeReadableProvider but does not declare range_read", p.Type())
	}
	if _, ok := p.(RangeWritableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.RangeWrite }) {
		return fmt.Errorf("%s implements RangeWritableProvider but does not declare range_write", p.Type())
	}
	if _, ok := p.(ResourceDeleteProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.Delete }) {
		return fmt.Errorf("%s implements ResourceDeleteProvider but does not declare delete", p.Type())
	}
	if _, ok := p.(BatchReadableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.BatchRead }) {
		return fmt.Errorf("%s implements BatchReadableProvider but does not declare batch_read", p.Type())
	}
	if _, ok := p.(TableReadSessionProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.TableReadSession }) {
		return fmt.Errorf("%s implements TableReadSessionProvider but does not declare table_read_session", p.Type())
	}
	if _, ok := p.(BatchWritableProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.BatchWrite }) {
		return fmt.Errorf("%s implements BatchWritableProvider but does not declare batch_write", p.Type())
	}
	if _, ok := p.(TableWriteSessionProvider); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.TableWriteSession }) {
		return fmt.Errorf("%s implements TableWriteSessionProvider but does not declare table_write_session", p.Type())
	}
	if _, ok := p.(TableWritePreparer); ok && !declaresStoreCapability(storage, func(store *StoreCapability) bool { return store.TableWritePrepare }) {
		return fmt.Errorf("%s implements TableWritePreparer but does not declare table_write_prepare", p.Type())
	}
	return nil
}

func implementsNativeTableReader(p EnginePlugin) bool {
	if _, ok := p.(BatchReadableProvider); ok {
		return true
	}
	if _, ok := p.(TableReadSessionProvider); ok {
		return true
	}
	return false
}

func implementsNativeTableWriter(p EnginePlugin) bool {
	if _, ok := p.(BatchWritableProvider); ok {
		return true
	}
	if _, ok := p.(TableWriteSessionProvider); ok {
		return true
	}
	return false
}

func declaresStoreCapability(storage *StorageCapabilities, hasCapability func(*StoreCapability) bool) bool {
	return storage != nil && storage.Store != nil && hasCapability(storage.Store)
}
