package skp

import (
	"context"
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"strings"
)

type vffComponentDoublePath struct {
	field string
	types []string
}
type vffComponentDoubleValue struct {
	component                                  int
	path                                       vffComponentDoublePath
	field                                      vffSKCFieldCandidate
	types                                      []vffSKCDeclarationWitness
	bits                                       uint64
	fieldOffset, dictionaryOffset, valueOffset int
	valueSHA256                                [32]byte
	metadata                                   vffComponentFieldMetadata
}
type vffComponentDoubleFacts struct {
	schema *vffComponentSchemaFacts
	values []vffComponentDoubleValue
	opaque []vffOpaqueRecord
}

// This consumes only the already-bound type edges. It does not add floating
// point semantics to the general scalar/facet evaluator or infer units.
func (f *vffComponentSimpleField) doubleTypes(names []string) ([]vffSKCDeclarationWitness, error) {
	b := f.checker.b
	if len(f.field.value.facets) != 0 {
		return nil, fmt.Errorf("double field facets remain unproved")
	}
	if strings.Trim(f.node.text, " \t\r\n") != "" {
		return nil, fmt.Errorf("double field text remains unproved")
	}
	for _, child := range f.node.children {
		if child.name.Space != vffXSDNS || child.name.Local != "annotation" {
			return nil, fmt.Errorf("double field content remains unproved")
		}
	}
	for name := range f.node.attrs {
		if !strings.Contains(" name type id minOccurs maxOccurs nillable form ", " "+name+" ") {
			return nil, fmt.Errorf("double field declaration constraint remains unproved")
		}
	}
	node := f.typeNode
	var declarations []vffSKCDeclarationWitness
	for i, name := range names {
		if err := b.tick(i + 1); err != nil {
			return nil, err
		}
		if node == nil || node.name.Space != vffXSDNS || node.name.Local != "simpleType" || node.attrs["name"] != name {
			return nil, fmt.Errorf("double dictionary path differs from its named type chain")
		}
		for parent := node; parent != nil; parent = parent.parent {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if parent.name.Local == "schema" && parent.attrs["finalDefault"] != "" {
				return nil, fmt.Errorf("double schema derivation default remains unproved")
			}
		}
		declarations = append(declarations, b.witness(node))
		for _, kind := range []string{"simpleType", "restriction"} {
			if strings.Trim(node.text, " \t\r\n") != "" {
				return nil, fmt.Errorf("double type has unproved text")
			}
			allowed := " name id "
			if kind == "restriction" {
				allowed = " base id "
			}
			for key := range node.attrs {
				if !strings.Contains(allowed, " "+key+" ") {
					return nil, fmt.Errorf("double type constraint remains unproved")
				}
			}
			var child *vffXMLNode
			annotations := 0
			for _, candidate := range node.children {
				if err := b.tick(0); err != nil {
					return nil, err
				}
				if candidate.name.Space != vffXSDNS {
					return nil, fmt.Errorf("double type content namespace remains unproved")
				}
				if candidate.name.Local == "annotation" {
					annotations++
					if annotations == 1 {
						continue
					}
				}
				if kind != "simpleType" || candidate.name.Local != "restriction" || child != nil {
					return nil, fmt.Errorf("double requires only named restrictions without facets")
				}
				child = candidate
			}
			if kind == "simpleType" {
				if child == nil {
					return nil, fmt.Errorf("double type restriction is missing")
				}
				node = child
			}
		}
		ref, exists := f.checker.refs[vffSKCTypeReferenceKey{b.witness(node), "base"}]
		if !exists {
			return nil, fmt.Errorf("double restriction base is not bound")
		}
		if ref.name.Space == vffXSDNS && ref.name.Local == "double" {
			if i != len(names)-1 {
				return nil, fmt.Errorf("double dictionary path extends beyond its named types")
			}
			return declarations, nil
		}
		if ref.name.Space != b.namespace || ref.target == nil || ref.target.kind != "simpleType" {
			return nil, fmt.Errorf("double restriction does not reach the supported builtin")
		}
		node = b.nodes[vffSKCNodeKey{ref.target.member, ref.target.begin}]
	}
	return nil, fmt.Errorf("double dictionary path omits a named base type")
}

// Explicit selected leaf values only, not a complete classification instance.
func readVFFComponentDoubleValues(ctx context.Context, c *vffContainer, schemaName, packageMember, componentType string, paths []vffComponentDoublePath, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentDoubleFacts, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("double values require explicit paths")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !vffSKCBindingName(path.field) || len(path.types) == 0 || seen[path.field] {
			return nil, fmt.Errorf("double values require unique explicit field and type paths")
		}
		seen[path.field] = true
		for _, name := range path.types {
			if !vffSKCBindingName(name) {
				return nil, fmt.Errorf("double dictionary type name is invalid")
			}
		}
	}
	scope, err := readVFFComponentFieldScope(ctx, c, schemaName, packageMember, componentType, componentLimits, limits)
	if err != nil {
		return nil, err
	}
	type plan struct {
		path  vffComponentDoublePath
		field vffComponentSimpleField
		types []vffSKCDeclarationWitness
	}
	var plans []plan
	for _, path := range paths {
		field, err := scope.simpleField(vffComponentFieldPath{path.field, path.types[0]})
		if err != nil {
			return nil, err
		}
		types, err := field.doubleTypes(path.types)
		if err != nil {
			return nil, err
		}
		path.types = append([]string(nil), path.types...)
		plans = append(plans, plan{path, field, types})
	}
	r := &vffComponentClassificationReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: componentLimits.records, maxDepth: componentLimits.depth}, result: &vffComponentClassificationFacts{}}
	result := &vffComponentDoubleFacts{schema: scope.schema}
	for _, component := range scope.selected {
		dictionaries, err := r.nestedDictionaries(scope.schema.components.bindings[component].nestedDictionary, 13)
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			field, exists := dictionaries[p.path.field]
			if !exists {
				return nil, fmt.Errorf("double field dictionary is missing")
			}
			leaf := field
			depth := 20
			for _, name := range p.path.types {
				for _, entry := range leaf.entries {
					r.keep([]vffClassificationRecord{entry})
				}
				children, err := r.nestedDictionaries(leaf.nested, depth-1)
				if err != nil {
					return nil, err
				}
				leaf, exists = children[name]
				if !exists {
					return nil, fmt.Errorf("double type dictionary is missing")
				}
				depth += 6
			}
			metadata, err := r.fieldMetadata(scope.schema, componentType, p.path.field, field, leaf, "double", 20, depth)
			if err != nil {
				return nil, err
			}
			if _, exists := leaf.entries["options"]; exists {
				return nil, fmt.Errorf("double field options remain unproved")
			}
			entry, exists := leaf.entries["value"]
			if !exists {
				return nil, fmt.Errorf("double field value is missing")
			}
			nodes, err := r.recordsAt(entry.data, depth, entry.offset)
			if err != nil {
				return nil, err
			}
			if len(nodes) != 1 || nodes[0].tag != "A938" || len(nodes[0].data) != 8 {
				return nil, fmt.Errorf("double field requires exactly one eight-byte A938 value")
			}
			bits := binary.LittleEndian.Uint64(nodes[0].data)
			if bits>>52&0x7ff == 0x7ff {
				return nil, fmt.Errorf("double field requires a finite value")
			}
			for key, other := range leaf.entries {
				if key != "value" && key != "attribute_type" && key != "is_hidden" {
					r.keep([]vffClassificationRecord{other})
				}
			}
			result.values = append(result.values, vffComponentDoubleValue{component: component, path: p.path, field: p.field.field, types: p.types, bits: bits, fieldOffset: field.offset, dictionaryOffset: leaf.offset, valueOffset: entry.offset - 6, valueSHA256: sha256.Sum256(entry.data), metadata: metadata})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.opaque = r.result.opaque
	sort.Slice(result.opaque, func(i, j int) bool { return result.opaque[i].offset < result.opaque[j].offset })
	return result, nil
}
