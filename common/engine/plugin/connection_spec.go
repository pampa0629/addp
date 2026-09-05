package plugin

import (
	"fmt"
	"sort"
	"strings"
)

const ConnectionSpecSchemaVersion = "engine.connection/v1"

const (
	ConnectionFieldText     = "text"
	ConnectionFieldPassword = "password"
	ConnectionFieldNumber   = "number"
	ConnectionFieldSelect   = "select"
	ConnectionFieldBoolean  = "boolean"
	ConnectionFieldTextarea = "textarea"
)

const ConnectionConstraintAllOrNone = "all_or_none"

type ConnectionFieldOption struct {
	Value    string `json:"value"`
	Label    string `json:"label,omitempty"`
	LabelKey string `json:"label_key,omitempty"`
}

type ConnectionFieldCondition struct {
	Field  string   `json:"field"`
	Values []string `json:"values"`
}

// ConnectionFieldSpec is the single source of truth for one connection_info field.
type ConnectionFieldSpec struct {
	Key            string                    `json:"key"`
	LabelKey       string                    `json:"label_key"`
	Input          string                    `json:"input"`
	Required       bool                      `json:"required"`
	Sensitive      bool                      `json:"sensitive"`
	Identity       bool                      `json:"identity"`
	Default        interface{}               `json:"default,omitempty"`
	Placeholder    string                    `json:"placeholder,omitempty"`
	PlaceholderKey string                    `json:"placeholder_key,omitempty"`
	HintKey        string                    `json:"hint_key,omitempty"`
	GroupKey       string                    `json:"group_key,omitempty"`
	Options        []ConnectionFieldOption   `json:"options,omitempty"`
	VisibleWhen    *ConnectionFieldCondition `json:"visible_when,omitempty"`
	Min            *int                      `json:"min,omitempty"`
	Max            *int                      `json:"max,omitempty"`
	Rows           int                       `json:"rows,omitempty"`
}

type ConnectionConstraintSpec struct {
	Kind       string                    `json:"kind"`
	Fields     []string                  `json:"fields"`
	MessageKey string                    `json:"message_key"`
	When       *ConnectionFieldCondition `json:"when,omitempty"`
}

// ConnectionSpec fully describes an engine registration connection form.
type ConnectionSpec struct {
	SchemaVersion string                     `json:"schema_version"`
	DefaultPort   int                        `json:"default_port,omitempty"`
	Fields        []ConnectionFieldSpec      `json:"fields"`
	Constraints   []ConnectionConstraintSpec `json:"constraints,omitempty"`
}

// EngineTypeDescriptor is the public, value-free description of one compiled engine type.
type EngineTypeDescriptor struct {
	Type           string                  `json:"type"`
	DisplayName    string                  `json:"display_name"`
	Origin         string                  `json:"origin"`
	ConnectionSpec ConnectionSpec          `json:"connection_spec"`
	Capabilities   EngineCapabilities      `json:"capabilities"`
	CatalogModel   *EngineCatalogModelSpec `json:"catalog_model,omitempty"`
}

type ConnectionSpecProvider interface {
	ConnectionSpec() ConnectionSpec
}

func NewConnectionSpec(fields ...ConnectionFieldSpec) ConnectionSpec {
	return ConnectionSpec{SchemaVersion: ConnectionSpecSchemaVersion, Fields: fields}
}

func Int(value int) *int { return &value }

func (s ConnectionSpec) Field(key string) (ConnectionFieldSpec, bool) {
	for _, field := range s.Fields {
		if field.Key == key {
			return field, true
		}
	}
	return ConnectionFieldSpec{}, false
}

func (s ConnectionSpec) DefaultPortValue() int {
	if s.DefaultPort > 0 {
		return s.DefaultPort
	}
	field, ok := s.Field("port")
	if !ok || field.Default == nil {
		return 0
	}
	switch value := field.Default.(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

func (s ConnectionSpec) RequiredFields() []string {
	return s.fieldsMatching(func(field ConnectionFieldSpec) bool { return field.Required })
}

func (s ConnectionSpec) UnconditionalRequiredFields() []string {
	return s.fieldsMatching(func(field ConnectionFieldSpec) bool { return field.Required && field.VisibleWhen == nil })
}

func (s ConnectionSpec) SensitiveFields() []string {
	return s.fieldsMatching(func(field ConnectionFieldSpec) bool { return field.Sensitive })
}

func (s ConnectionSpec) IdentityFields() []string {
	return s.fieldsMatching(func(field ConnectionFieldSpec) bool { return field.Identity })
}

func (s ConnectionSpec) fieldsMatching(match func(ConnectionFieldSpec) bool) []string {
	fields := make([]string, 0, len(s.Fields))
	for _, field := range s.Fields {
		if match(field) {
			fields = append(fields, field.Key)
		}
	}
	return fields
}

func (s ConnectionSpec) Validate() error {
	if s.SchemaVersion != ConnectionSpecSchemaVersion {
		return fmt.Errorf("connection spec schema_version must be %s", ConnectionSpecSchemaVersion)
	}
	if s.DefaultPort < 0 || s.DefaultPort > 65535 {
		return fmt.Errorf("connection spec default_port must be between 0 and 65535")
	}
	seen := make(map[string]struct{}, len(s.Fields))
	for _, field := range s.Fields {
		if strings.TrimSpace(field.Key) == "" {
			return fmt.Errorf("connection spec contains empty field key")
		}
		if _, exists := seen[field.Key]; exists {
			return fmt.Errorf("connection spec contains duplicate field %s", field.Key)
		}
		seen[field.Key] = struct{}{}
		if field.LabelKey == "" {
			return fmt.Errorf("connection field %s is missing label_key", field.Key)
		}
		switch field.Input {
		case ConnectionFieldText, ConnectionFieldPassword, ConnectionFieldNumber, ConnectionFieldSelect, ConnectionFieldBoolean, ConnectionFieldTextarea:
		default:
			return fmt.Errorf("connection field %s has unsupported input %s", field.Key, field.Input)
		}
		if field.Sensitive && field.Input != ConnectionFieldPassword && field.Input != ConnectionFieldTextarea && field.Input != ConnectionFieldText {
			return fmt.Errorf("sensitive connection field %s has invalid input %s", field.Key, field.Input)
		}
		if field.VisibleWhen != nil {
			if _, ok := seen[field.VisibleWhen.Field]; !ok {
				return fmt.Errorf("connection field %s depends on unknown or later field %s", field.Key, field.VisibleWhen.Field)
			}
		}
	}
	for _, constraint := range s.Constraints {
		if constraint.Kind != ConnectionConstraintAllOrNone {
			return fmt.Errorf("unsupported connection constraint %s", constraint.Kind)
		}
		if len(constraint.Fields) < 2 || constraint.MessageKey == "" {
			return fmt.Errorf("invalid %s connection constraint", constraint.Kind)
		}
		for _, field := range constraint.Fields {
			if _, ok := seen[field]; !ok {
				return fmt.Errorf("connection constraint references unknown field %s", field)
			}
		}
		if constraint.When != nil {
			if _, ok := seen[constraint.When.Field]; !ok {
				return fmt.Errorf("connection constraint depends on unknown field %s", constraint.When.Field)
			}
		}
	}
	return nil
}

func DescribeEngineType(enginePlugin EnginePlugin) (EngineTypeDescriptor, error) {
	provider, ok := enginePlugin.(ConnectionSpecProvider)
	if !ok {
		return EngineTypeDescriptor{}, fmt.Errorf("engine plugin %s did not implement ConnectionSpecProvider", enginePlugin.Type())
	}
	spec := provider.ConnectionSpec()
	if err := spec.Validate(); err != nil {
		return EngineTypeDescriptor{}, fmt.Errorf("engine plugin %s has invalid connection spec: %w", enginePlugin.Type(), err)
	}
	descriptor := EngineTypeDescriptor{
		Type: enginePlugin.Type(), DisplayName: enginePlugin.DisplayName(), Origin: enginePlugin.EngineOrigin(),
		ConnectionSpec: spec, Capabilities: enginePlugin.Capabilities(),
	}
	if catalogProvider, ok := enginePlugin.(EngineCatalogModelProvider); ok {
		model := catalogProvider.EngineCatalogModel()
		descriptor.CatalogModel = &model
	}
	return descriptor, nil
}

func ListEngineTypeDescriptors(origin string) ([]EngineTypeDescriptor, error) {
	descriptors := make([]EngineTypeDescriptor, 0)
	for _, enginePlugin := range GetAll() {
		if origin != "" && enginePlugin.EngineOrigin() != origin {
			continue
		}
		descriptor, err := DescribeEngineType(enginePlugin)
		if err != nil {
			return nil, err
		}
		descriptors = append(descriptors, descriptor)
	}
	sort.Slice(descriptors, func(i, j int) bool { return descriptors[i].Type < descriptors[j].Type })
	return descriptors, nil
}
