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
	if err := validatePluginFormat(formatType); err != nil {
		return err
	}
	if descriptorProvider, ok := plugin.(FormatDescriptorProvider); ok {
		descriptor := descriptorProvider.Descriptor()
		if descriptor.Format != formatType {
			return fmt.Errorf("format plugin descriptor format %s does not match plugin format %s", descriptor.Format, formatType)
		}
		if descriptor.ID != "" && descriptor.DataType != "" && shouldRegisterPluginDescriptor(formatType) {
			if err := RegisterFormatDescriptor(descriptor); err != nil {
				return err
			}
		}
	}

	r.mu.Lock()
	r.formatPlugins[formatType] = plugin
	r.mu.Unlock()

	return validatePluginImplementedCapabilities(plugin)
}

func shouldRegisterPluginDescriptor(formatType FormatType) bool {
	_, ok := GetFormatDescriptor(formatType)
	return !ok
}

func validatePluginFormat(formatType FormatType) error {
	if formatType == "" {
		return fmt.Errorf("format plugin must define format")
	}
	return nil
}

func validatePluginImplementedCapabilities(plugin FormatPlugin) error {
	if provider, ok := plugin.(RelatedRefSpecProvider); ok {
		if err := ValidateRelatedRefSpecs(provider.RelatedRefSpecs()); err != nil {
			return fmt.Errorf("format plugin %s has invalid related ref specs: %w", plugin.Format(), err)
		}
	}
	return nil
}
