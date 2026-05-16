package format

import "fmt"

func RegisterDocumentProvider(provider DocumentProvider) error {
	return globalProviderRegistry.RegisterDocumentProvider(provider)
}

func (r *ProviderRegistry) RegisterDocumentProvider(provider DocumentProvider) error {
	if provider == nil {
		return fmt.Errorf("document provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "document provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentProviders[formatType] = provider
	r.documentInfoProviders[formatType] = provider
	r.documentTextReaders[formatType] = provider
	return nil
}

func RegisterDocumentInfoProvider(provider DocumentInfoProvider) error {
	return globalProviderRegistry.RegisterDocumentInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterDocumentInfoProvider(provider DocumentInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("document info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "document info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentInfoProviders[formatType] = provider
	return nil
}

func RegisterDocumentTextReader(reader DocumentTextReader) error {
	return globalProviderRegistry.RegisterDocumentTextReader(reader)
}

func (r *ProviderRegistry) RegisterDocumentTextReader(reader DocumentTextReader) error {
	if reader == nil {
		return fmt.Errorf("document text reader cannot be nil")
	}
	formatType := reader.Format()
	if err := validateProviderFormat(formatType, "document text reader"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.documentTextReaders[formatType] = reader
	return nil
}
