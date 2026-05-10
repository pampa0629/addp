package capability

import (
	"fmt"
	"sort"
	"strings"
	"sync"
)

type Format string

const (
	FormatTable     Format = "table"
	FormatDocument  Format = "document"
	FormatCSV       Format = "csv"
	FormatJSON      Format = "json"
	FormatMarkdown  Format = "markdown"
	FormatParquet   Format = "parquet"
	FormatShapefile Format = "shapefile"
)

const (
	DataTypeTable     = "table"
	DataTypeDocument  = "document"
	DataTypeMedia     = "media"
	DataTypeContainer = "container"
	DataTypeGraph     = "graph"
	DataTypeFile      = "file"
)

const (
	EngineFamilyTabular  = "tabular"
	EngineFamilyObject   = "object"
	EngineFamilyFile     = "file"
	EngineFamilyDocument = "document"
)

const (
	LayoutSingle = "single"
	LayoutMulti  = "multi"
	LayoutWhole  = "whole"
)

const (
	ProviderTable     = "table"
	ProviderDocument  = "document"
	ProviderMedia     = "media"
	ProviderContainer = "container"
	ProviderGraph     = "graph"
	ProviderSpatial   = "spatial"
)

// Capability 声明一个格式在 ADDP 中可被哪些平台能力消费。
//
// 它不是文件探测规则，也不等同于 Provider 注册表；Provider 只说明代码里是否有实现，
// Capability 则用于能力声明、Transfer、Preview 等模块统一理解格式的产品语义。
type Capability struct {
	Format         Format
	I18nKey        string
	Extensions     []string
	DataType       string
	Layouts        []string
	ProviderHints  []string
	Spatial        bool
	TransferRead   bool
	TransferWrite  bool
	Preview        bool
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
	mustRegister(Capability{
		Format:         FormatTable,
		I18nKey:        "format.table",
		DataType:       DataTypeTable,
		Layouts:        []string{LayoutWhole},
		ProviderHints:  []string{ProviderTable},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		EngineFamilies: []string{EngineFamilyTabular},
	})
	mustRegister(Capability{
		Format:         FormatDocument,
		I18nKey:        "format.document",
		DataType:       DataTypeDocument,
		Layouts:        []string{LayoutWhole},
		ProviderHints:  []string{ProviderDocument},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		EngineFamilies: []string{EngineFamilyDocument},
	})
	mustRegister(Capability{
		Format:         FormatCSV,
		I18nKey:        "format.csv",
		Extensions:     []string{".csv"},
		DataType:       DataTypeTable,
		Layouts:        []string{LayoutSingle},
		ProviderHints:  []string{ProviderTable},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	})
	mustRegister(Capability{
		Format:         FormatJSON,
		I18nKey:        "format.json",
		Extensions:     []string{".json", ".geojson"},
		DataType:       DataTypeDocument,
		Layouts:        []string{LayoutSingle},
		ProviderHints:  []string{ProviderDocument, ProviderTable, ProviderSpatial},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	})
	mustRegister(Capability{
		Format:         FormatMarkdown,
		I18nKey:        "format.markdown",
		Extensions:     []string{".md", ".markdown"},
		DataType:       DataTypeDocument,
		Layouts:        []string{LayoutSingle},
		ProviderHints:  []string{ProviderDocument},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		Parse:          false,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	})
	mustRegister(Capability{
		Format:         FormatParquet,
		I18nKey:        "format.parquet",
		Extensions:     []string{".parquet"},
		DataType:       DataTypeTable,
		Layouts:        []string{LayoutSingle, LayoutWhole},
		ProviderHints:  []string{ProviderTable},
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	})
	mustRegister(Capability{
		Format:         FormatShapefile,
		I18nKey:        "format.shapefile",
		Extensions:     []string{".shp", ".shx", ".dbf", ".prj"},
		DataType:       DataTypeTable,
		Layouts:        []string{LayoutMulti},
		ProviderHints:  []string{ProviderTable, ProviderSpatial},
		Spatial:        true,
		TransferRead:   true,
		TransferWrite:  true,
		Preview:        true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	})
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
