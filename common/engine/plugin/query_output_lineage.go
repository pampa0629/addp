package plugin

import (
	"fmt"
	"strings"

	"github.com/addp/common/datatype"
)

const (
	QueryOutputTransformationDirect  = "direct"
	QueryOutputTransformationDerived = "derived"
)

// QueryOutputLineage is the complete provider-owned field value flow from the
// QueryReadSet leaves to QueryResult rows. It contains no Security decisions.
type QueryOutputLineage struct {
	Sources []QueryOutputSource `json:"sources"`
}

type QueryOutputSource struct {
	Path           EngineCatalogPath    `json:"path"`
	Fields         []datatype.FieldInfo `json:"fields"`
	IdentityOutput bool                 `json:"identity_output"`
	OpaqueOutput   bool                 `json:"opaque_output"`
	Bindings       []QueryOutputBinding `json:"bindings,omitempty"`
}

type QueryOutputBinding struct {
	SourcePath     []string `json:"source_path"`
	OutputPath     []string `json:"output_path,omitempty"`
	Transformation string   `json:"transformation"`
}

func ValidateQueryOutputLineage(readSet *QueryReadSet, lineage *QueryOutputLineage) error {
	if readSet == nil || lineage == nil || len(lineage.Sources) != len(readSet.Paths) {
		return fmt.Errorf("%w: every read source requires exactly one lineage source", ErrQueryOutputLineageUnresolved)
	}
	byPath := make(map[string]struct{}, len(readSet.Paths))
	for _, path := range readSet.Paths {
		byPath[queryReadPathKey(path)] = struct{}{}
	}
	seen := make(map[string]struct{}, len(lineage.Sources))
	for sourceIndex, source := range lineage.Sources {
		key := queryReadPathKey(source.Path)
		if _, exists := byPath[key]; !exists {
			return fmt.Errorf("%w: lineage source %d is outside the read set", ErrQueryOutputLineageUnresolved, sourceIndex)
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("%w: duplicate lineage source", ErrQueryOutputLineageUnresolved)
		}
		seen[key] = struct{}{}
		if len(source.Fields) == 0 {
			return fmt.Errorf("%w: lineage source fields are required", ErrQueryOutputLineageUnresolved)
		}
		fieldPaths := make(map[string]struct{}, len(source.Fields))
		for _, field := range source.Fields {
			path := field.Path
			if len(path) == 0 {
				path = []string{field.Name}
			}
			if !validQueryComponentPath(path) {
				return fmt.Errorf("%w: invalid lineage source field path", ErrQueryOutputLineageUnresolved)
			}
			fieldPaths[strings.Join(path, "\x00")] = struct{}{}
		}
		if source.OpaqueOutput && (source.IdentityOutput || len(source.Bindings) > 0) {
			return fmt.Errorf("%w: opaque lineage source cannot declare mappings", ErrQueryOutputLineageUnresolved)
		}
		for bindingIndex, binding := range source.Bindings {
			if len(binding.SourcePath) == 0 || !validQueryComponentPath(binding.SourcePath) {
				return fmt.Errorf("%w: invalid source path at binding %d", ErrQueryOutputLineageUnresolved, bindingIndex)
			}
			if _, exists := fieldPaths[strings.Join(binding.SourcePath, "\x00")]; !exists {
				return fmt.Errorf("%w: binding source path is absent from current fields", ErrQueryOutputLineageUnresolved)
			}
			switch binding.Transformation {
			case QueryOutputTransformationDirect:
				if len(binding.OutputPath) == 0 || !validQueryComponentPath(binding.OutputPath) {
					return fmt.Errorf("%w: direct binding requires an output path", ErrQueryOutputLineageUnresolved)
				}
			case QueryOutputTransformationDerived:
			default:
				return fmt.Errorf("%w: invalid output transformation", ErrQueryOutputLineageUnresolved)
			}
		}
	}
	return nil
}

func validQueryComponentPath(path []string) bool {
	for _, segment := range path {
		if strings.TrimSpace(segment) == "" {
			return false
		}
	}
	return true
}

func (l *QueryOutputLineage) Clone() *QueryOutputLineage {
	if l == nil {
		return nil
	}
	cloned := &QueryOutputLineage{Sources: make([]QueryOutputSource, len(l.Sources))}
	for sourceIndex, source := range l.Sources {
		clonedSource := source
		clonedSource.Path = cloneEngineCatalogPath(source.Path)
		clonedSource.Fields = append([]datatype.FieldInfo(nil), source.Fields...)
		for fieldIndex := range clonedSource.Fields {
			clonedSource.Fields[fieldIndex].Path = append([]string(nil), source.Fields[fieldIndex].Path...)
		}
		clonedSource.Bindings = make([]QueryOutputBinding, len(source.Bindings))
		for bindingIndex, binding := range source.Bindings {
			clonedSource.Bindings[bindingIndex] = QueryOutputBinding{
				SourcePath:     append([]string(nil), binding.SourcePath...),
				OutputPath:     append([]string(nil), binding.OutputPath...),
				Transformation: binding.Transformation,
			}
		}
		cloned.Sources[sourceIndex] = clonedSource
	}
	return cloned
}
