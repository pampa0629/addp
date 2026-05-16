package format

import "fmt"

func RegisterTableProvider(provider TableProvider) error {
	return globalProviderRegistry.RegisterTableProvider(provider)
}

func (r *ProviderRegistry) RegisterTableProvider(provider TableProvider) error {
	if provider == nil {
		return fmt.Errorf("table provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "table provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableProviders[formatType] = provider
	r.tableInfoProviders[formatType] = provider
	r.tableSampleProviders[formatType] = provider
	return nil
}

func RegisterTableInfoProvider(provider TableInfoProvider) error {
	return globalProviderRegistry.RegisterTableInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterTableInfoProvider(provider TableInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("table info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "table info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableInfoProviders[formatType] = provider
	return nil
}

func RegisterTableSampleProvider(provider TableSampleProvider) error {
	return globalProviderRegistry.RegisterTableSampleProvider(provider)
}

func (r *ProviderRegistry) RegisterTableSampleProvider(provider TableSampleProvider) error {
	if provider == nil {
		return fmt.Errorf("table sample provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "table sample provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableSampleProviders[formatType] = provider
	return nil
}

func RegisterTableReaderProvider(provider TableReaderProvider) error {
	return globalProviderRegistry.RegisterTableReaderProvider(provider)
}

func (r *ProviderRegistry) RegisterTableReaderProvider(provider TableReaderProvider) error {
	if provider == nil {
		return fmt.Errorf("table reader provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "table reader provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableReaderProviders[formatType] = provider
	return nil
}

func RegisterTableWriterProvider(provider TableWriterProvider) error {
	return globalProviderRegistry.RegisterTableWriterProvider(provider)
}

func (r *ProviderRegistry) RegisterTableWriterProvider(provider TableWriterProvider) error {
	if provider == nil {
		return fmt.Errorf("table writer provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "table writer provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.tableWriterProviders[formatType] = provider
	return nil
}
