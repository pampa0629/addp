package format

import (
	"fmt"
	"sort"
	"strings"
	"sync"

	"github.com/addp/common/datatype"
)

type FormatIdentification struct {
	Extensions        []string `json:"extensions,omitempty"`
	MimeTypes         []string `json:"mime_types,omitempty"`
	ContentSignatures []string `json:"content_signatures,omitempty"`
}

type FormatDescriptor struct {
	ID             string               `json:"id"`
	Version        string               `json:"version,omitempty"`
	Priority       int                  `json:"priority,omitempty"`
	Format         FormatType           `json:"format"`
	I18nKey        string               `json:"i18n_key,omitempty"`
	DataType       datatype.DataType    `json:"data_type"`
	Layouts        []string             `json:"layouts,omitempty"`
	Identification FormatIdentification `json:"identification,omitempty"`
}

type FormatConflictDiagnostic struct {
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

type descriptorRegistry struct {
	mu          sync.RWMutex
	descriptors map[FormatType]FormatDescriptor
	conflicts   []FormatConflictDiagnostic
}

var globalDescriptorRegistry = newDescriptorRegistry()

func newDescriptorRegistry() *descriptorRegistry {
	return &descriptorRegistry{
		descriptors: make(map[FormatType]FormatDescriptor),
	}
}

func ListFormatDescriptors() []FormatDescriptor {
	return globalDescriptorRegistry.ListFormatDescriptors()
}

func (r *descriptorRegistry) ListFormatDescriptors() []FormatDescriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptors := make([]FormatDescriptor, 0, len(r.descriptors))
	for _, descriptor := range r.descriptors {
		descriptors = append(descriptors, cloneFormatDescriptor(descriptor))
	}
	sort.Slice(descriptors, func(i, j int) bool {
		return descriptors[i].Format < descriptors[j].Format
	})
	return descriptors
}

func GetFormatDescriptor(formatType FormatType) (FormatDescriptor, bool) {
	return globalDescriptorRegistry.GetFormatDescriptor(formatType)
}

func (r *descriptorRegistry) GetFormatDescriptor(formatType FormatType) (FormatDescriptor, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	descriptor, ok := r.descriptors[formatType]
	if !ok {
		return FormatDescriptor{}, false
	}
	return cloneFormatDescriptor(descriptor), true
}

func SupportsAccessIndex(formatType FormatType) bool {
	provider, err := GetTableInfoProvider(formatType)
	if err != nil {
		return false
	}
	accessProvider, ok := provider.(AccessIndexProvider)
	return ok && accessProvider.SupportsAccessIndex()
}

func RegisterFormatDescriptor(descriptor FormatDescriptor) error {
	return globalDescriptorRegistry.RegisterFormatDescriptor(descriptor)
}

func (r *descriptorRegistry) RegisterFormatDescriptor(descriptor FormatDescriptor) error {
	if err := ValidateLayouts(descriptor.Layouts); err != nil {
		return fmt.Errorf("format descriptor has invalid layouts: %w", err)
	}
	descriptor = normalizeFormatDescriptor(descriptor)
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
		r.conflicts = append(r.conflicts, formatConflictDiagnostic("format", string(descriptor.Format), existing, descriptor, overridden))
		shouldStore = overridden
	}

	for _, existing := range r.descriptors {
		if existing.Format == descriptor.Format {
			continue
		}
		recordFormatIdentificationConflicts(&r.conflicts, existing, descriptor)
	}

	if shouldStore {
		r.descriptors[descriptor.Format] = cloneFormatDescriptor(descriptor)
	}
	return nil
}

func (r *descriptorRegistry) ListFormatConflictDiagnostics() []FormatConflictDiagnostic {
	r.mu.RLock()
	defer r.mu.RUnlock()

	return append([]FormatConflictDiagnostic{}, r.conflicts...)
}

func normalizeFormatDescriptor(descriptor FormatDescriptor) FormatDescriptor {
	descriptor.ID = strings.TrimSpace(descriptor.ID)
	descriptor.Version = strings.TrimSpace(descriptor.Version)
	descriptor.I18nKey = strings.TrimSpace(descriptor.I18nKey)
	if value := strings.TrimSpace(string(descriptor.DataType)); value != "" {
		descriptor.DataType = datatype.ParseDataType(value)
	}
	descriptor.Layouts = NormalizeLayouts(descriptor.Layouts)
	descriptor.Identification.Extensions = normalizedFormatStrings(descriptor.Identification.Extensions, true)
	descriptor.Identification.MimeTypes = normalizedFormatStrings(descriptor.Identification.MimeTypes, false)
	descriptor.Identification.ContentSignatures = normalizedFormatStrings(descriptor.Identification.ContentSignatures, false)
	return descriptor
}

func normalizedFormatStrings(values []string, isExtension bool) []string {
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

func recordFormatIdentificationConflicts(conflicts *[]FormatConflictDiagnostic, existing, candidate FormatDescriptor) {
	for _, extension := range candidate.Identification.Extensions {
		if containsFormatString(existing.Identification.Extensions, extension) {
			*conflicts = append(*conflicts, formatConflictDiagnostic("extension", extension, existing, candidate, false))
		}
	}
	for _, mimeType := range candidate.Identification.MimeTypes {
		if containsFormatString(existing.Identification.MimeTypes, mimeType) {
			*conflicts = append(*conflicts, formatConflictDiagnostic("mime_type", mimeType, existing, candidate, false))
		}
	}
}

func formatConflictDiagnostic(kind, key string, existing, candidate FormatDescriptor, overridden bool) FormatConflictDiagnostic {
	return FormatConflictDiagnostic{
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

func containsFormatString(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneFormatDescriptor(descriptor FormatDescriptor) FormatDescriptor {
	descriptor.Layouts = append([]string(nil), descriptor.Layouts...)
	descriptor.Identification.Extensions = append([]string(nil), descriptor.Identification.Extensions...)
	descriptor.Identification.MimeTypes = append([]string(nil), descriptor.Identification.MimeTypes...)
	descriptor.Identification.ContentSignatures = append([]string(nil), descriptor.Identification.ContentSignatures...)
	return descriptor
}

func mustRegisterFormatDescriptor(descriptor FormatDescriptor) {
	if err := RegisterFormatDescriptor(descriptor); err != nil {
		panic(err)
	}
}
