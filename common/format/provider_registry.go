package format

import "sync"

type ProviderRegistry struct {
	mu                      sync.RWMutex
	formatPlugins           map[FormatType]FormatPlugin
	tableProviders          map[FormatType]TableProvider
	formatInfoProviders     map[FormatType]FormatInfoProvider
	tableInfoProviders      map[FormatType]TableInfoProvider
	tableSampleProviders    map[FormatType]TableSampleProvider
	documentProviders       map[FormatType]DocumentProvider
	documentInfoProviders   map[FormatType]DocumentInfoProvider
	documentTextReaders     map[FormatType]DocumentTextReader
	mediaInfoProviders      map[FormatType]MediaInfoProvider
	containerInfoProviders  map[FormatType]ContainerInfoProvider
	containerChildResolvers map[FormatType]ContainerChildResolver
}

var globalProviderRegistry = NewProviderRegistry()

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		formatPlugins:           make(map[FormatType]FormatPlugin),
		tableProviders:          make(map[FormatType]TableProvider),
		formatInfoProviders:     make(map[FormatType]FormatInfoProvider),
		tableInfoProviders:      make(map[FormatType]TableInfoProvider),
		tableSampleProviders:    make(map[FormatType]TableSampleProvider),
		documentProviders:       make(map[FormatType]DocumentProvider),
		documentInfoProviders:   make(map[FormatType]DocumentInfoProvider),
		documentTextReaders:     make(map[FormatType]DocumentTextReader),
		mediaInfoProviders:      make(map[FormatType]MediaInfoProvider),
		containerInfoProviders:  make(map[FormatType]ContainerInfoProvider),
		containerChildResolvers: make(map[FormatType]ContainerChildResolver),
	}
}
