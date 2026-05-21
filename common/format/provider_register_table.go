package format

import "fmt"

func RegisterMultiTableInfoProvider(provider MultiTableInfoProvider) error {
	return globalProviderRegistry.RegisterMultiTableInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterMultiTableInfoProvider(provider MultiTableInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("multi table info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "multi table info provider"); err != nil {
		return err
	}
	if err := validateRelatedRefSpecProvider(provider, "multi table info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.multiTableInfoProviders[formatType] = provider
	return nil
}

func RegisterMultiTableSampleReader(reader MultiTableSampleReader) error {
	return globalProviderRegistry.RegisterMultiTableSampleReader(reader)
}

func (r *ProviderRegistry) RegisterMultiTableSampleReader(reader MultiTableSampleReader) error {
	if reader == nil {
		return fmt.Errorf("multi table sample reader cannot be nil")
	}
	formatType := reader.Format()
	if err := validateProviderFormat(formatType, "multi table sample reader"); err != nil {
		return err
	}
	if err := validateRelatedRefSpecProvider(reader, "multi table sample reader"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.multiTableSampleReaders[formatType] = reader
	return nil
}

func RegisterScopeTableInfoProvider(provider ScopeTableInfoProvider) error {
	return globalProviderRegistry.RegisterScopeTableInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterScopeTableInfoProvider(provider ScopeTableInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("scope table info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "scope table info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopeTableInfoProviders[formatType] = provider
	return nil
}

func RegisterScopeTableSampleReader(reader ScopeTableSampleReader) error {
	return globalProviderRegistry.RegisterScopeTableSampleReader(reader)
}

func (r *ProviderRegistry) RegisterScopeTableSampleReader(reader ScopeTableSampleReader) error {
	if reader == nil {
		return fmt.Errorf("scope table sample reader cannot be nil")
	}
	formatType := reader.Format()
	if err := validateProviderFormat(formatType, "scope table sample reader"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopeTableSampleReaders[formatType] = reader
	return nil
}

func RegisterScopeTableReaderProvider(provider ScopeTableReaderProvider) error {
	return globalProviderRegistry.RegisterScopeTableReaderProvider(provider)
}

func (r *ProviderRegistry) RegisterScopeTableReaderProvider(provider ScopeTableReaderProvider) error {
	if provider == nil {
		return fmt.Errorf("scope table reader provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "scope table reader provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.scopeTableReaders[formatType] = provider
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

func RegisterTableSampleReader(provider TableSampleReader) error {
	return globalProviderRegistry.RegisterTableSampleReader(provider)
}

func (r *ProviderRegistry) RegisterTableSampleReader(provider TableSampleReader) error {
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

func RegisterMultiTableReaderProvider(provider MultiTableReaderProvider) error {
	return globalProviderRegistry.RegisterMultiTableReaderProvider(provider)
}

func (r *ProviderRegistry) RegisterMultiTableReaderProvider(provider MultiTableReaderProvider) error {
	if provider == nil {
		return fmt.Errorf("multi table reader provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "multi table reader provider"); err != nil {
		return err
	}
	if err := validateRelatedRefSpecProvider(provider, "multi table reader provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.multiTableReaders[formatType] = provider
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

func RegisterMultiTableWriterProvider(provider MultiTableWriterProvider) error {
	return globalProviderRegistry.RegisterMultiTableWriterProvider(provider)
}

func (r *ProviderRegistry) RegisterMultiTableWriterProvider(provider MultiTableWriterProvider) error {
	if provider == nil {
		return fmt.Errorf("multi table writer provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "multi table writer provider"); err != nil {
		return err
	}
	if err := validateRelatedRefSpecProvider(provider, "multi table writer provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.multiTableWriters[formatType] = provider
	return nil
}

func validateRelatedRefSpecProvider(provider RelatedRefSpecProvider, label string) error {
	if provider == nil {
		return fmt.Errorf("%s cannot be nil", label)
	}
	if err := ValidateRelatedRefSpecs(provider.RelatedRefSpecs()); err != nil {
		return fmt.Errorf("%s %s has invalid related ref specs: %w", label, provider.Format(), err)
	}
	return nil
}
