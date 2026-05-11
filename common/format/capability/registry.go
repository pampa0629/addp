package capability

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	formatregistry "github.com/addp/common/format/registry"
)

type Format string

const (
	FormatTable     Format = Format(formatregistry.FormatTable)
	FormatDocument  Format = Format(formatregistry.FormatDocument)
	FormatCSV       Format = Format(formatregistry.FormatCSV)
	FormatDOCX      Format = Format(formatregistry.FormatDOCX)
	FormatExcel     Format = Format(formatregistry.FormatExcel)
	FormatGIF       Format = Format(formatregistry.FormatGIF)
	FormatImage     Format = Format(formatregistry.FormatImage)
	FormatJPEG      Format = Format(formatregistry.FormatJPEG)
	FormatJSON      Format = Format(formatregistry.FormatJSON)
	FormatMarkdown  Format = Format(formatregistry.FormatMarkdown)
	FormatParquet   Format = Format(formatregistry.FormatParquet)
	FormatPDF       Format = Format(formatregistry.FormatPDF)
	FormatPNG       Format = Format(formatregistry.FormatPNG)
	FormatPPTX      Format = Format(formatregistry.FormatPPTX)
	FormatShapefile Format = Format(formatregistry.FormatShapefile)
	FormatSQLite    Format = Format(formatregistry.FormatSQLite)
	FormatText      Format = Format(formatregistry.FormatText)
	FormatTIFF      Format = Format(formatregistry.FormatTIFF)
	FormatWPS       Format = Format(formatregistry.FormatWPS)
)

const (
	DataTypeTable     = formatregistry.DataTypeTable
	DataTypeDocument  = formatregistry.DataTypeDocument
	DataTypeMedia     = formatregistry.DataTypeMedia
	DataTypeContainer = formatregistry.DataTypeContainer
	DataTypeGraph     = formatregistry.DataTypeGraph
	DataTypeFile      = formatregistry.DataTypeFile
)

const (
	EngineFamilyTabular  = formatregistry.EngineFamilyTabular
	EngineFamilyObject   = formatregistry.EngineFamilyObject
	EngineFamilyFile     = formatregistry.EngineFamilyFile
	EngineFamilyDocument = formatregistry.EngineFamilyDocument
)

const (
	LayoutSingle = formatregistry.LayoutSingle
	LayoutMulti  = formatregistry.LayoutMulti
	LayoutWhole  = formatregistry.LayoutWhole
)

const (
	ProviderTable     = formatregistry.ProviderTable
	ProviderDocument  = formatregistry.ProviderDocument
	ProviderMedia     = formatregistry.ProviderMedia
	ProviderContainer = formatregistry.ProviderContainer
	ProviderGraph     = formatregistry.ProviderGraph
	ProviderSpatial   = formatregistry.ProviderSpatial
)

// Capability 声明一个格式在 ADDP 中可被哪些平台能力消费。
//
// 它不是文件探测规则，也不等同于 Provider 注册表；Provider 只说明代码里是否有实现，
// Capability 则用于能力声明、Transfer 等模块统一理解格式的产品语义。
type Capability struct {
	Format         Format
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

type Registry struct {
	mu           sync.RWMutex
	capabilities map[Format]Capability
}

var globalRegistry = newRegistry()

func newRegistry() *Registry {
	return &Registry{
		capabilities: make(map[Format]Capability),
	}
}

func init() {
	for _, descriptor := range formatregistry.ListDescriptors() {
		mustRegister(CapabilityFromDescriptor(descriptor))
	}
}

func CapabilityFromDescriptor(descriptor formatregistry.Descriptor) Capability {
	return Capability{
		Format:         Format(descriptor.Format),
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

func mustRegister(capability Capability) {
	if err := Register(capability); err != nil {
		panic(err)
	}
}

func Register(capability Capability) error {
	return globalRegistry.Register(capability)
}

func (r *Registry) Register(capability Capability) error {
	if capability.Format == "" {
		return fmt.Errorf("format capability must define format")
	}

	capability.I18nKey = strings.TrimSpace(capability.I18nKey)
	capability.DataType = strings.TrimSpace(capability.DataType)
	capability.Extensions = normalizedStrings(capability.Extensions, true)
	capability.Layouts = normalizedStrings(capability.Layouts, false)
	capability.ProviderHints = normalizedStrings(capability.ProviderHints, false)
	capability.ContentReaders = normalizedStrings(capability.ContentReaders, false)
	capability.EngineFamilies = normalizedStrings(capability.EngineFamilies, false)

	r.mu.Lock()
	defer r.mu.Unlock()
	r.capabilities[capability.Format] = clone(capability)
	return nil
}

func Get(format Format) (Capability, bool) {
	return globalRegistry.Get(format)
}

func (r *Registry) Get(format Format) (Capability, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	capability, ok := r.capabilities[format]
	if !ok {
		return Capability{}, false
	}
	return clone(capability), true
}

func List() []Capability {
	return globalRegistry.List()
}

func (r *Registry) List() []Capability {
	r.mu.RLock()
	defer r.mu.RUnlock()

	capabilities := make([]Capability, 0, len(r.capabilities))
	for _, capability := range r.capabilities {
		capabilities = append(capabilities, clone(capability))
	}
	sort.Slice(capabilities, func(i, j int) bool {
		return capabilities[i].Format < capabilities[j].Format
	})
	return capabilities
}

func ListTransferFormatsForEngineFamily(engineFamily string) []string {
	return globalRegistry.ListTransferFormatsForEngineFamily(engineFamily)
}

func (r *Registry) ListTransferFormatsForEngineFamily(engineFamily string) []string {
	engineFamily = strings.ToLower(strings.TrimSpace(engineFamily))
	if engineFamily == "" {
		return nil
	}

	r.mu.RLock()
	defer r.mu.RUnlock()

	formats := make([]string, 0)
	for _, capability := range r.capabilities {
		if !capability.TransferRead && !capability.TransferWrite {
			continue
		}
		if containsString(capability.EngineFamilies, engineFamily) {
			formats = append(formats, string(capability.Format))
		}
	}
	sort.Strings(formats)
	return formats
}

func clone(capability Capability) Capability {
	capability.Extensions = append([]string(nil), capability.Extensions...)
	capability.Layouts = append([]string(nil), capability.Layouts...)
	capability.ProviderHints = append([]string(nil), capability.ProviderHints...)
	capability.ContentReaders = append([]string(nil), capability.ContentReaders...)
	capability.EngineFamilies = append([]string(nil), capability.EngineFamilies...)
	return capability
}

func normalizedStrings(values []string, keepDotPrefix bool) []string {
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

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
