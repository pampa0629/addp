package format

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	formatregistry "github.com/addp/common/format/registry"
)

const (
	FormatDataTypeTable     = formatregistry.DataTypeTable
	FormatDataTypeDocument  = formatregistry.DataTypeDocument
	FormatDataTypeMedia     = formatregistry.DataTypeMedia
	FormatDataTypeContainer = formatregistry.DataTypeContainer
	FormatDataTypeGraph     = formatregistry.DataTypeGraph
	FormatDataTypeFile      = formatregistry.DataTypeFile
)

const (
	EngineFamilyTabular  = formatregistry.EngineFamilyTabular
	EngineFamilyObject   = formatregistry.EngineFamilyObject
	EngineFamilyFile     = formatregistry.EngineFamilyFile
	EngineFamilyDocument = formatregistry.EngineFamilyDocument
)

// Layout describes how a format can organize the content that forms a data item.
//
// Format capability uses layout as a declared possibility; data item detection
// uses the same values as the resolved item organization.
type Layout = string

const (
	FormatLayoutSingle Layout = formatregistry.LayoutSingle
	FormatLayoutMulti  Layout = formatregistry.LayoutMulti
	FormatLayoutWhole  Layout = formatregistry.LayoutWhole
)

const (
	FormatProviderTable     = formatregistry.ProviderTable
	FormatProviderDocument  = formatregistry.ProviderDocument
	FormatProviderMedia     = formatregistry.ProviderMedia
	FormatProviderContainer = formatregistry.ProviderContainer
	FormatProviderGraph     = formatregistry.ProviderGraph
	FormatProviderSpatial   = formatregistry.ProviderSpatial
)

// FormatCapability 声明一个格式在 ADDP 中可被哪些平台能力消费。
type FormatCapability struct {
	Format         FormatType
	I18nKey        string
	Extensions     []string
	DataType       string
	Layouts        []string
	ProviderHints  []string
	ContentReaders []string
	Spatial        bool
	TransferRead   bool
	TransferWrite  bool
	Parse          bool
	EngineFamilies []string
}

type formatCapabilityRegistry struct {
	mu           sync.RWMutex
	capabilities map[FormatType]FormatCapability
}

var globalFormatCapabilityRegistry = newFormatCapabilityRegistry()

func newFormatCapabilityRegistry() *formatCapabilityRegistry {
	return &formatCapabilityRegistry{
		capabilities: make(map[FormatType]FormatCapability),
	}
}

func RegisterFormatCapability(capability FormatCapability) error {
	return globalFormatCapabilityRegistry.Register(capability)
}

func (r *formatCapabilityRegistry) Register(capability FormatCapability) error {
	if capability.Format == "" {
		return fmt.Errorf("format capability must define format")
	}

	capability.I18nKey = strings.TrimSpace(capability.I18nKey)
	capability.DataType = strings.TrimSpace(capability.DataType)
	capability.Extensions = normalizedCapabilityStrings(capability.Extensions, true)
	capability.Layouts = normalizedCapabilityStrings(capability.Layouts, false)
	capability.ProviderHints = normalizedCapabilityStrings(capability.ProviderHints, false)
	capability.ContentReaders = normalizedCapabilityStrings(capability.ContentReaders, false)
	capability.EngineFamilies = normalizedCapabilityStrings(capability.EngineFamilies, false)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[capability.Format] = cloneFormatCapability(capability)
	return nil
}

func GetFormatCapability(formatType FormatType) (FormatCapability, bool) {
	return globalFormatCapabilityRegistry.Get(formatType)
}

func (r *formatCapabilityRegistry) Get(formatType FormatType) (FormatCapability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	capability, ok := r.capabilities[formatType]
	if !ok {
		return FormatCapability{}, false
	}
	return cloneFormatCapability(capability), true
}

func ListFormatCapabilities() []FormatCapability {
	return globalFormatCapabilityRegistry.List()
}

func (r *formatCapabilityRegistry) List() []FormatCapability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	capabilities := make([]FormatCapability, 0, len(r.capabilities))
	for _, capability := range r.capabilities {
		capabilities = append(capabilities, cloneFormatCapability(capability))
	}
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].Format < capabilities[j].Format
	})
	return capabilities
}

func ListTransferFormatsForEngineFamily(engineFamily string) []string {
	return formatregistry.ListTransferFormatsForEngineFamily(engineFamily)
}

func FormatCapabilityFromDescriptor(descriptor FormatDescriptor) FormatCapability {
	return FormatCapability{
		Format:         descriptor.Format,
		I18nKey:        descriptor.I18nKey,
		Extensions:     append([]string(nil), descriptor.Identification.Extensions...),
		DataType:       descriptor.DataType,
		Layouts:        append([]string(nil), descriptor.Layouts...),
		ProviderHints:  append([]string(nil), descriptor.ProviderHints...),
		ContentReaders: append([]string(nil), descriptor.ContentReaders...),
		Spatial:        descriptor.Spatial,
		TransferRead:   descriptor.TransferRead,
		TransferWrite:  descriptor.TransferWrite,
		Parse:          descriptor.Parse,
		EngineFamilies: append([]string(nil), descriptor.EngineFamilies...),
	}
}

func cloneFormatCapability(capability FormatCapability) FormatCapability {
	capability.Extensions = append([]string(nil), capability.Extensions...)
	capability.Layouts = append([]string(nil), capability.Layouts...)
	capability.ProviderHints = append([]string(nil), capability.ProviderHints...)
	capability.ContentReaders = append([]string(nil), capability.ContentReaders...)
	capability.EngineFamilies = append([]string(nil), capability.EngineFamilies...)
	return capability
}

func normalizedCapabilityStrings(values []string, keepDotPrefix bool) []string {
	if len(values) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if keepDotPrefix && !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}
