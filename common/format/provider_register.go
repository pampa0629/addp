package format

import "fmt"

func RegisterFormatPlugin(plugin FormatPlugin) error {
	return globalProviderRegistry.RegisterFormatPlugin(plugin)
}

func (r *ProviderRegistry) RegisterFormatPlugin(plugin FormatPlugin) error {
	if plugin == nil {
		return fmt.Errorf("format plugin cannot be nil")
	}
	formatType := plugin.Format()
	if err := validateProviderFormat(formatType, "format plugin"); err != nil {
		return err
	}
	descriptor := plugin.Descriptor()
	if descriptor.Format == "" {
		descriptor.Format = formatType
	}
	if descriptor.Format != formatType {
		return fmt.Errorf("format plugin descriptor format %s does not match plugin format %s", descriptor.Format, formatType)
	}
	if descriptor.ID != "" && descriptor.DataType != "" && shouldRegisterPluginDescriptor(formatType) {
		if err := RegisterFormatDescriptor(descriptor); err != nil {
			return err
		}
	}

	r.mu.Lock()
	r.formatPlugins[formatType] = plugin
	r.mu.Unlock()

	return r.registerPluginImplementedProviders(plugin)
}

func shouldRegisterPluginDescriptor(formatType FormatType) bool {
	_, ok := GetFormatDescriptor(formatType)
	return !ok
}

func (r *ProviderRegistry) registerPluginImplementedProviders(plugin FormatPlugin) error {
	if provider, ok := plugin.(FormatInfoProvider); ok {
		if err := r.RegisterFormatInfoProvider(provider); err != nil {
			return err
		}
	}
	if provider, ok := plugin.(TableProvider); ok {
		if err := r.RegisterTableProvider(provider); err != nil {
			return err
		}
	} else {
		if provider, ok := plugin.(TableInfoProvider); ok {
			if err := r.RegisterTableInfoProvider(provider); err != nil {
				return err
			}
		}
		if reader, ok := plugin.(TableSampleProvider); ok {
			if err := r.RegisterTableSampleProvider(reader); err != nil {
				return err
			}
		}
	}
	if reader, ok := plugin.(TableReaderProvider); ok {
		if err := r.RegisterTableReaderProvider(reader); err != nil {
			return err
		}
	}
	if writer, ok := plugin.(TableWriterProvider); ok {
		if err := r.RegisterTableWriterProvider(writer); err != nil {
			return err
		}
	}
	if writer, ok := plugin.(ComponentTableWriterProvider); ok {
		if err := r.RegisterComponentTableWriterProvider(writer); err != nil {
			return err
		}
	}
	if provider, ok := plugin.(DocumentProvider); ok {
		if err := r.RegisterDocumentProvider(provider); err != nil {
			return err
		}
	} else {
		if provider, ok := plugin.(DocumentInfoProvider); ok {
			if err := r.RegisterDocumentInfoProvider(provider); err != nil {
				return err
			}
		}
		if reader, ok := plugin.(DocumentTextReader); ok {
			if err := r.RegisterDocumentTextReader(reader); err != nil {
				return err
			}
		}
	}
	if provider, ok := plugin.(MediaInfoProvider); ok {
		if err := r.RegisterMediaInfoProvider(provider); err != nil {
			return err
		}
	}
	if provider, ok := plugin.(ContainerInfoProvider); ok {
		if err := r.RegisterContainerInfoProvider(provider); err != nil {
			return err
		}
	}
	if resolver, ok := plugin.(ContainerChildResolver); ok {
		if err := r.RegisterContainerChildResolver(resolver); err != nil {
			return err
		}
	}
	return nil
}

func RegisterFormatInfoProvider(provider FormatInfoProvider) error {
	return globalProviderRegistry.RegisterFormatInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterFormatInfoProvider(provider FormatInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("format info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "format info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.formatInfoProviders[formatType] = provider
	return nil
}

func validateProviderFormat(formatType FormatType, label string) error {
	if formatType == "" || formatType == FormatUnknown {
		return fmt.Errorf("%s must define format", label)
	}
	return nil
}
