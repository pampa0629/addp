package format

import "fmt"

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

func RegisterBinaryContentReader(reader BinaryContentReader) error {
	return globalProviderRegistry.RegisterBinaryContentReader(reader)
}

func (r *ProviderRegistry) RegisterBinaryContentReader(reader BinaryContentReader) error {
	if reader == nil {
		return fmt.Errorf("binary content reader cannot be nil")
	}
	formatType := reader.Format()
	if err := validateBinaryContentReaderFormat(formatType); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.binaryContentReaders[formatType] = reader
	return nil
}

func validateBinaryContentReaderFormat(formatType FormatType) error {
	if formatType == "" {
		return fmt.Errorf("binary content reader must define format")
	}
	return nil
}
