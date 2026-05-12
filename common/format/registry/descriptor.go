package registry

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
	FormatDOCX      Format = "docx"
	FormatExcel     Format = "excel"
	FormatGIF       Format = "gif"
	FormatImage     Format = "image"
	FormatJPEG      Format = "jpeg"
	FormatJSON      Format = "json"
	FormatMarkdown  Format = "markdown"
	FormatORC       Format = "orc"
	FormatParquet   Format = "parquet"
	FormatPDF       Format = "pdf"
	FormatPNG       Format = "png"
	FormatPPTX      Format = "pptx"
	FormatShapefile Format = "shapefile"
	FormatSQLite    Format = "sqlite"
	FormatText      Format = "text"
	FormatTIFF      Format = "tiff"
	FormatTSV       Format = "tsv"
	FormatWPS       Format = "wps"
	FormatAvro      Format = "avro"
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

const (
	ContentReaderTableSample          = "table_sample"
	ContentReaderComponentTableSample = "component_table_sample"
	ContentReaderScopeTableSample     = "scope_table_sample"
	ContentReaderDocumentText         = "document_text"
	ContentReaderRawContent           = "raw_content"
	ContentReaderRangeContent         = "range_content"
	ContentReaderMediaThumbnail       = "media_thumbnail"
	ContentReaderContainerEntry       = "container_entry"
	ContentReaderGraphSample          = "graph_sample"
)

type Identification struct {
	Extensions        []string `json:"extensions,omitempty"`
	MimeTypes         []string `json:"mime_types,omitempty"`
	ContentSignatures []string `json:"content_signatures,omitempty"`
}

type ProviderDescriptor struct {
	FormatInfo     bool `json:"format_info,omitempty"`
	TableInfo      bool `json:"table_info,omitempty"`
	TableSample    bool `json:"table_sample,omitempty"`
	Table          bool `json:"table,omitempty"`
	ComponentTable bool `json:"component_table,omitempty"`
	ScopeTable     bool `json:"scope_table,omitempty"`
	ContentIndex   bool `json:"content_index,omitempty"`
	DocumentInfo   bool `json:"document_info,omitempty"`
	MediaInfo      bool `json:"media_info,omitempty"`
	ContainerInfo  bool `json:"container_info,omitempty"`
	Metadata       bool `json:"metadata,omitempty"` // legacy: FileMetadataExtractor 兼容状态
}

type Descriptor struct {
	ID             string             `json:"id"`
	Version        string             `json:"version,omitempty"`
	Priority       int                `json:"priority,omitempty"`
	Format         Format             `json:"format"`
	I18nKey        string             `json:"i18n_key,omitempty"`
	DataType       string             `json:"data_type"`
	Layouts        []string           `json:"layouts,omitempty"`
	ProviderHints  []string           `json:"provider_hints,omitempty"`
	Identification Identification     `json:"identification,omitempty"`
	Providers      ProviderDescriptor `json:"providers,omitempty"`
	ContentReaders []string           `json:"content_readers,omitempty"`
	TransferRead   bool               `json:"transfer_read,omitempty"`
	TransferWrite  bool               `json:"transfer_write,omitempty"`
	Parse          bool               `json:"parse,omitempty"`
	Spatial        bool               `json:"spatial,omitempty"`
	EngineFamilies []string           `json:"engine_families,omitempty"`
}

type ConflictDiagnostic struct {
	Kind              string `json:"kind"`
	Key               string `json:"key"`
	ExistingPluginID  string `json:"existing_plugin_id"`
	ExistingVersion   string `json:"existing_version,omitempty"`
	ExistingPriority  int    `json:"existing_priority,omitempty"`
	CandidatePluginID string `json:"candidate_plugin_id"`
	CandidateVersion  string `json:"candidate_version,omitempty"`
	CandidatePriority int    `json:"candidate_priority,omitempty"`
	Overridden        bool   `json:"overridden"`
}

type Registry struct {
	mu          sync.RWMutex
	descriptors map[Format]Descriptor
	conflicts   []ConflictDiagnostic
}

var builtinDescriptors = []Descriptor{
	{
		ID:            "builtin-table",
		Format:        FormatTable,
		I18nKey:       "format.table",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutWhole},
		ProviderHints: []string{ProviderTable},
		Providers: ProviderDescriptor{
			TableInfo:   true,
			TableSample: true,
			Table:       true,
		},
		ContentReaders: []string{ContentReaderTableSample},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyTabular},
	},
	{
		ID:            "builtin-document",
		Format:        FormatDocument,
		I18nKey:       "format.document",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutWhole},
		ProviderHints: []string{ProviderDocument},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
		},
		ContentReaders: []string{ContentReaderDocumentText, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyDocument},
	},
	{
		ID:            "builtin-csv",
		Format:        FormatCSV,
		I18nKey:       "format.csv",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderTable},
		Identification: Identification{
			Extensions: []string{".csv"},
			MimeTypes:  []string{"text/csv"},
		},
		Providers: ProviderDescriptor{
			FormatInfo:   true,
			TableInfo:    true,
			TableSample:  true,
			Table:        true,
			ContentIndex: true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-tsv",
		Format:        FormatTSV,
		I18nKey:       "format.tsv",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderTable},
		Identification: Identification{
			Extensions: []string{".tsv"},
			MimeTypes:  []string{"text/tab-separated-values"},
		},
		Providers: ProviderDescriptor{
			FormatInfo:   true,
			TableInfo:    true,
			TableSample:  true,
			Table:        true,
			ContentIndex: true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-docx",
		Format:        FormatDOCX,
		I18nKey:       "format.docx",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".docx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.wordprocessingml.document"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-excel",
		Format:        FormatExcel,
		I18nKey:       "format.excel",
		DataType:      DataTypeContainer,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderContainer, ProviderTable},
		Identification: Identification{
			Extensions: []string{".xlsx", ".xls", ".xlsm"},
			MimeTypes: []string{
				"application/vnd.openxmlformats-officedocument.spreadsheetml.sheet",
				"application/vnd.ms-excel",
				"application/vnd.ms-excel.sheet.macroenabled.12",
			},
		},
		Providers: ProviderDescriptor{
			ContainerInfo: true,
			TableInfo:     true,
			TableSample:   true,
			Table:         true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderRawContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-json",
		Format:        FormatJSON,
		I18nKey:       "format.json",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument, ProviderTable, ProviderSpatial},
		Identification: Identification{
			Extensions: []string{".json", ".geojson"},
			MimeTypes:  []string{"application/json", "application/geo+json", "application/vnd.geo+json"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
			TableInfo:    true,
			TableSample:  true,
			Table:        true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-markdown",
		Format:        FormatMarkdown,
		I18nKey:       "format.markdown",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".md", ".markdown"},
			MimeTypes:  []string{"text/markdown", "text/x-markdown"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
		},
		ContentReaders: []string{ContentReaderDocumentText, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-pdf",
		Format:        FormatPDF,
		I18nKey:       "format.pdf",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".pdf"},
			MimeTypes:  []string{"application/pdf"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
			Metadata:     true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-pptx",
		Format:        FormatPPTX,
		I18nKey:       "format.pptx",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".pptx"},
			MimeTypes:  []string{"application/vnd.openxmlformats-officedocument.presentationml.presentation"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-parquet",
		Format:        FormatParquet,
		I18nKey:       "format.parquet",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutSingle, LayoutWhole},
		ProviderHints: []string{ProviderTable},
		Identification: Identification{
			Extensions: []string{".parquet"},
			MimeTypes:  []string{"application/parquet", "application/x-parquet", "application/vnd.apache.parquet"},
		},
		Providers: ProviderDescriptor{
			TableInfo:   true,
			TableSample: true,
			Table:       true,
			ScopeTable:  true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderScopeTableSample, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-orc",
		Format:        FormatORC,
		I18nKey:       "format.orc",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutSingle, LayoutWhole},
		ProviderHints: []string{ProviderTable},
		Identification: Identification{
			Extensions: []string{".orc"},
			MimeTypes:  []string{"application/orc", "application/vnd.apache.orc"},
		},
		ContentReaders: []string{ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-avro",
		Format:        FormatAvro,
		I18nKey:       "format.avro",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutSingle, LayoutWhole},
		ProviderHints: []string{ProviderTable},
		Identification: Identification{
			Extensions: []string{".avro"},
			MimeTypes:  []string{"application/avro", "application/x-avro-binary"},
		},
		ContentReaders: []string{ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-image",
		Format:        FormatImage,
		I18nKey:       "format.image",
		DataType:      DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		Providers: ProviderDescriptor{
			MediaInfo: true,
			Metadata:  true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-jpeg",
		Format:        FormatJPEG,
		I18nKey:       "format.jpeg",
		DataType:      DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		Identification: Identification{
			Extensions: []string{".jpg", ".jpeg"},
			MimeTypes:  []string{"image/jpeg"},
		},
		Providers: ProviderDescriptor{
			MediaInfo: true,
			Metadata:  true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-png",
		Format:        FormatPNG,
		I18nKey:       "format.png",
		DataType:      DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		Identification: Identification{
			Extensions: []string{".png"},
			MimeTypes:  []string{"image/png"},
		},
		Providers: ProviderDescriptor{
			MediaInfo: true,
			Metadata:  true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-gif",
		Format:        FormatGIF,
		I18nKey:       "format.gif",
		DataType:      DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		Identification: Identification{
			Extensions: []string{".gif"},
			MimeTypes:  []string{"image/gif"},
		},
		Providers: ProviderDescriptor{
			MediaInfo: true,
			Metadata:  true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-tiff",
		Format:        FormatTIFF,
		I18nKey:       "format.tiff",
		DataType:      DataTypeMedia,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderMedia},
		Identification: Identification{
			Extensions: []string{".tif", ".tiff"},
			MimeTypes:  []string{"image/tiff"},
		},
		Providers: ProviderDescriptor{
			MediaInfo: true,
			Metadata:  true,
		},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-shapefile",
		Format:        FormatShapefile,
		I18nKey:       "format.shapefile",
		DataType:      DataTypeTable,
		Layouts:       []string{LayoutMulti},
		ProviderHints: []string{ProviderTable, ProviderSpatial},
		Identification: Identification{
			Extensions: []string{".shp", ".shx", ".dbf", ".prj"},
			MimeTypes:  []string{"application/x-shapefile", "application/x-esri-shapefile"},
		},
		Providers: ProviderDescriptor{
			FormatInfo:     true,
			TableInfo:      true,
			TableSample:    true,
			Table:          true,
			ComponentTable: true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderComponentTableSample, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		Parse:          true,
		Spatial:        true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-sqlite",
		Format:        FormatSQLite,
		I18nKey:       "format.sqlite",
		DataType:      DataTypeContainer,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderContainer, ProviderTable},
		Identification: Identification{
			Extensions: []string{".sqlite", ".sqlite3", ".db"},
			MimeTypes:  []string{"application/x-sqlite3", "application/vnd.sqlite3", "application/sqlite"},
		},
		Providers: ProviderDescriptor{
			ContainerInfo: true,
			TableInfo:     true,
			TableSample:   true,
			Table:         true,
		},
		ContentReaders: []string{ContentReaderTableSample, ContentReaderRawContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile},
	},
	{
		ID:            "builtin-text",
		Format:        FormatText,
		I18nKey:       "format.text",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".txt"},
			MimeTypes:  []string{"text/plain"},
		},
		Providers: ProviderDescriptor{
			DocumentInfo: true,
		},
		ContentReaders: []string{ContentReaderDocumentText, ContentReaderRawContent},
		TransferRead:   true,
		TransferWrite:  true,
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
	{
		ID:            "builtin-wps",
		Format:        FormatWPS,
		I18nKey:       "format.wps",
		DataType:      DataTypeDocument,
		Layouts:       []string{LayoutSingle},
		ProviderHints: []string{ProviderDocument},
		Identification: Identification{
			Extensions: []string{".wps"},
			MimeTypes:  []string{"application/vnd.ms-works", "application/wps-office.doc", "application/x-wps", "application/kswps"},
		},
		Providers:      ProviderDescriptor{},
		ContentReaders: []string{ContentReaderRawContent, ContentReaderRangeContent},
		EngineFamilies: []string{EngineFamilyObject, EngineFamilyFile, EngineFamilyDocument},
	},
}

var globalRegistry = newRegistry()

func init() {
	for _, descriptor := range builtinDescriptors {
		mustRegisterDescriptor(descriptor)
	}
}

func newRegistry() *Registry {
	return &Registry{
		descriptors: make(map[Format]Descriptor),
	}
}

func mustRegisterDescriptor(descriptor Descriptor) {
	if err := RegisterDescriptor(descriptor); err != nil {
		panic(err)
	}
}

func RegisterDescriptor(descriptor Descriptor) error {
	return globalRegistry.RegisterDescriptor(descriptor)
}

func (r *Registry) RegisterDescriptor(descriptor Descriptor) error {
	descriptor = normalizeDescriptor(descriptor)
	if descriptor.ID == "" {
		return fmt.Errorf("format descriptor requires id")
	}
	if descriptor.Format == "" {
		return fmt.Errorf("format descriptor %s requires format", descriptor.ID)
	}
	if descriptor.DataType == "" {
		return fmt.Errorf("format descriptor %s requires data_type", descriptor.ID)
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	shouldStore := true
	if existing, exists := r.descriptors[descriptor.Format]; exists {
		overridden := descriptor.Priority >= existing.Priority
		r.conflicts = append(r.conflicts, conflictDiagnostic("format", string(descriptor.Format), existing, descriptor, overridden))
		shouldStore = overridden
	}

	for _, existing := range r.descriptors {
		if existing.Format == descriptor.Format {
			continue
		}
		recordIdentificationConflicts(&r.conflicts, existing, descriptor)
	}

	if shouldStore {
		r.descriptors[descriptor.Format] = cloneDescriptor(descriptor)
	}
	return nil
}

func ListDescriptors() []Descriptor {
	return globalRegistry.ListDescriptors()
}

func (r *Registry) ListDescriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptors := make([]Descriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		descriptors = append(descriptors, cloneDescriptor(descriptor))
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Format < descriptors[j].Format
	})
	return descriptors
}

func GetDescriptor(format Format) (Descriptor, bool) {
	return globalRegistry.GetDescriptor(format)
}

func (r *Registry) GetDescriptor(format Format) (Descriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptor, ok := r.descriptors[format]
	if !ok {
		return Descriptor{}, false
	}
	return cloneDescriptor(descriptor), true
}

func ListConflictDiagnostics() []ConflictDiagnostic {
	return globalRegistry.ListConflictDiagnostics()
}

func (r *Registry) ListConflictDiagnostics() []ConflictDiagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]ConflictDiagnostic{}, r.conflicts...)
}

func normalizeDescriptor(descriptor Descriptor) Descriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Version = strings.TrimSpace(descriptor.Version)
	descriptor.I18nKey = strings.TrimSpace(descriptor.I18nKey)
	descriptor.DataType = strings.TrimSpace(descriptor.DataType)
	descriptor.Layouts = normalizedStrings(descriptor.Layouts, false)
	descriptor.ProviderHints = normalizedStrings(descriptor.ProviderHints, false)
	descriptor.ContentReaders = normalizedStrings(descriptor.ContentReaders, false)
	descriptor.EngineFamilies = normalizedStrings(descriptor.EngineFamilies, false)
	descriptor.Identification.Extensions = normalizedStrings(descriptor.Identification.Extensions, true)
	descriptor.Identification.MimeTypes = normalizedStrings(descriptor.Identification.MimeTypes, false)
	descriptor.Identification.ContentSignatures = normalizedStrings(descriptor.Identification.ContentSignatures, false)
	return descriptor
}

func normalizedStrings(values []string, isExtension bool) []string {
	if len(values) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(values))
	normalized := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.ToLower(strings.TrimSpace(value))
		if value == "" {
			continue
		}
		if isExtension && !strings.HasPrefix(value, ".") {
			value = "." + value
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		normalized = append(normalized, value)
	}
	return normalized
}

func recordIdentificationConflicts(conflicts *[]ConflictDiagnostic, existing, candidate Descriptor) {
	for _, extension := range candidate.Identification.Extensions {
		if containsString(existing.Identification.Extensions, extension) {
			*conflicts = append(*conflicts, conflictDiagnostic("extension", extension, existing, candidate, false))
		}
	}
	for _, mimeType := range candidate.Identification.MimeTypes {
		if containsString(existing.Identification.MimeTypes, mimeType) {
			*conflicts = append(*conflicts, conflictDiagnostic("mime_type", mimeType, existing, candidate, false))
		}
	}
}

func conflictDiagnostic(kind, key string, existing, candidate Descriptor, overridden bool) ConflictDiagnostic {
	return ConflictDiagnostic{
		Kind:              kind,
		Key:               key,
		ExistingPluginID:  existing.ID,
		ExistingVersion:   existing.Version,
		ExistingPriority:  existing.Priority,
		CandidatePluginID: candidate.ID,
		CandidateVersion:  candidate.Version,
		CandidatePriority: candidate.Priority,
		Overridden:        overridden,
	}
}

func containsString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneDescriptor(descriptor Descriptor) Descriptor {
	descriptor.Layouts = append([]string(nil), descriptor.Layouts...)
	descriptor.ProviderHints = append([]string(nil), descriptor.ProviderHints...)
	descriptor.ContentReaders = append([]string(nil), descriptor.ContentReaders...)
	descriptor.EngineFamilies = append([]string(nil), descriptor.EngineFamilies...)
	descriptor.Identification.Extensions = append([]string(nil), descriptor.Identification.Extensions...)
	descriptor.Identification.MimeTypes = append([]string(nil), descriptor.Identification.MimeTypes...)
	descriptor.Identification.ContentSignatures = append([]string(nil), descriptor.Identification.ContentSignatures...)
	return descriptor
}
