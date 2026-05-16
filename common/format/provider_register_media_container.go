package format

import "fmt"

func RegisterMediaProvider(provider MediaProvider) error {
	return globalProviderRegistry.RegisterMediaProvider(provider)
}

func (r *ProviderRegistry) RegisterMediaProvider(provider MediaProvider) error {
	return r.RegisterMediaInfoProvider(provider)
}

func RegisterMediaInfoProvider(provider MediaInfoProvider) error {
	return globalProviderRegistry.RegisterMediaInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterMediaInfoProvider(provider MediaInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("media info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "media info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.mediaInfoProviders[formatType] = provider
	return nil
}

func RegisterContainerInfoProvider(provider ContainerInfoProvider) error {
	return globalProviderRegistry.RegisterContainerInfoProvider(provider)
}

func (r *ProviderRegistry) RegisterContainerInfoProvider(provider ContainerInfoProvider) error {
	if provider == nil {
		return fmt.Errorf("container info provider cannot be nil")
	}
	formatType := provider.Format()
	if err := validateProviderFormat(formatType, "container info provider"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.containerInfoProviders[formatType] = provider
	return nil
}

func RegisterContainerChildResolver(resolver ContainerChildResolver) error {
	return globalProviderRegistry.RegisterContainerChildResolver(resolver)
}

func (r *ProviderRegistry) RegisterContainerChildResolver(resolver ContainerChildResolver) error {
	if resolver == nil {
		return fmt.Errorf("container child resolver cannot be nil")
	}
	formatType := resolver.Format()
	if err := validateProviderFormat(formatType, "container child resolver"); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	r.containerChildResolvers[formatType] = resolver
	return nil
}
