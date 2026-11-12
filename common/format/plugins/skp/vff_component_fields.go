package skp

import (
	"context"
	"crypto/sha256"
	"encoding/xml"
	"fmt"
	"sort"
)

type vffComponentFieldPath struct{ field, valueType string }
type vffComponentFieldValue struct {
	component                                  int
	path                                       vffComponentFieldPath
	field                                      vffSKCFieldCandidate
	typeDeclaration                            vffSKCDeclarationWitness
	primitive                                  xml.Name
	facets                                     []vffSKCDeclarationWitness
	raw                                        string
	value                                      vffSKCValue
	fieldOffset, dictionaryOffset, valueOffset int
	valueSHA256                                [32]byte
	metadata                                   vffComponentFieldMetadata
}
type vffComponentFieldFacts struct {
	schema *vffComponentSchemaFacts
	values []vffComponentFieldValue
	opaque []vffOpaqueRecord
}

func (r *vffComponentClassificationReader) nestedDictionaries(parent vffClassificationRecord, depth int) (map[string]vffClassificationDictionary, error) {
	if parent.tag != "B036" || len(parent.data) == 0 {
		return nil, fmt.Errorf("selected classification path has no nested dictionaries")
	}
	var err error
	for i, tag := range []string{"DC05", "DD05", "B136", "B236"} {
		parent, err = r.level(parent, tag, depth+i)
		if err != nil {
			return nil, err
		}
	}
	dictionaries, _, err := r.dictionaries(parent, depth+4)
	return dictionaries, err
}

type vffComponentFieldScope struct {
	schema   *vffComponentSchemaFacts
	b        *vffSKCBinder
	selected []int
	fields   []vffSKCFieldCandidate
}

func readVFFComponentFieldScope(ctx context.Context, c *vffContainer, schemaName, packageMember, componentType string, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentFieldScope, error) {
	if !vffSKCBindingName(componentType) {
		return nil, fmt.Errorf("component field scope requires an explicit type")
	}
	schema, err := readVFFComponentSchemaBindings(ctx, c, schemaName, packageMember, componentLimits, limits)
	if err != nil {
		return nil, err
	}
	var selected []int
	var declaration *vffSKCDeclarationWitness
	for _, binding := range schema.bindings {
		if schema.components.bindings[binding.component].schemaType == componentType {
			selected = append(selected, binding.component)
			declaration = &binding.declaration
		}
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no components match the selected classification type")
	}
	b := schema.graph.binders[schema.document.rootSchema]
	typeNode := b.nodes[vffSKCNodeKey{declaration.member, declaration.begin}]
	b.issues = nil
	fields, err := b.expand(typeNode, nil, 1)
	if err != nil {
		return nil, err
	}
	if len(b.issues) != 0 {
		return nil, fmt.Errorf("component inherited field scope is not fully resolved")
	}
	return &vffComponentFieldScope{schema, b, selected, fields}, nil
}

func (s *vffComponentFieldScope) element(name string) (*vffSKCFieldCandidate, error) {
	var found *vffSKCFieldCandidate
	for i := range s.fields {
		if err := s.b.tick(0); err != nil {
			return nil, err
		}
		if s.fields[i].declaration.name == name {
			if found != nil || s.fields[i].declaration.kind != "element" {
				return nil, fmt.Errorf("selected classification field is not a unique element")
			}
			found = &s.fields[i]
		}
	}
	if found == nil {
		return nil, fmt.Errorf("selected classification field declaration is missing")
	}
	return found, nil
}

type vffComponentSimpleField struct {
	path           vffComponentFieldPath
	field          vffSKCFieldCandidate
	declaration    vffSKCDeclarationWitness
	node, typeNode *vffXMLNode
	checker        *vffSKCRestrictionChecker
}
type vffComponentStringField struct {
	vffComponentSimpleField
	profile *vffSKCRestrictionProfile
}

func (s *vffComponentFieldScope) simpleField(path vffComponentFieldPath) (vffComponentSimpleField, error) {
	b := s.b
	found, err := s.element(path.field)
	if err != nil {
		return vffComponentSimpleField{}, err
	}
	node := b.nodes[vffSKCNodeKey{found.declaration.member, found.declaration.begin}]
	name, err := b.qname(node, node.attrs["type"])
	if err != nil {
		return vffComponentSimpleField{}, err
	}
	if name.Space != b.namespace || name.Local != path.valueType {
		return vffComponentSimpleField{}, fmt.Errorf("dictionary value type differs from the exact field type")
	}
	target, err := b.resolveDeclaration(node, name, "type")
	if err != nil {
		return vffComponentSimpleField{}, err
	}
	if target == nil || target.node.name.Local != "simpleType" {
		return vffComponentSimpleField{}, fmt.Errorf("classification field requires a unique named simple type")
	}
	found.value, err = b.readValueType(node)
	if err != nil {
		return vffComponentSimpleField{}, err
	}
	if len(found.value.issues) != 0 {
		return vffComponentSimpleField{}, fmt.Errorf("selected classification field value type is unproved")
	}
	refs, err := b.restrictionTypeReferences(found.value)
	if err != nil {
		return vffComponentSimpleField{}, err
	}
	checker := &vffSKCRestrictionChecker{b: b, refs: refs, cache: map[*vffXMLNode]*vffSKCRestrictionProfile{}, active: map[*vffXMLNode]bool{}}
	return vffComponentSimpleField{path, *found, b.witness(target.node), node, target.node, checker}, nil
}

func (s *vffComponentFieldScope) stringField(path vffComponentFieldPath) (vffComponentStringField, error) {
	field, err := s.simpleField(path)
	if err != nil {
		return vffComponentStringField{}, err
	}
	profile, err := field.checker.profile(field.typeNode, 1)
	if err != nil {
		return vffComponentStringField{}, err
	}
	if !profile.known || profile.item != nil || profile.primitive.Space != vffXSDNS || profile.primitive.Local != "string" && profile.primitive.Local != "normalizedString" && profile.primitive.Local != "token" {
		return vffComponentStringField{}, fmt.Errorf("classification field is not a proved scalar string type")
	}
	return vffComponentStringField{field, profile}, nil
}

// One typed/fixed value evaluation shared by plain and enumeration leaves.
func (p *vffComponentStringField) checkedValue(raw string) (vffSKCValue, error) {
	value, known, err := p.checker.evaluate(p.profile, p.node, raw)
	if err != nil {
		return vffSKCValue{}, err
	}
	if !known {
		return vffSKCValue{}, fmt.Errorf("component field string value remains unproved")
	}
	if fixed, declared := p.node.attrs["fixed"]; declared {
		constraint, known, err := p.checker.evaluate(p.profile, p.node, fixed)
		if err != nil {
			return vffSKCValue{}, err
		}
		if !known {
			return vffSKCValue{}, fmt.Errorf("component field fixed value remains unproved")
		}
		equal, err := p.checker.b.equalValue(value, constraint)
		if err != nil {
			return vffSKCValue{}, err
		}
		if !equal {
			return vffSKCValue{}, fmt.Errorf("component field value differs from its fixed declaration")
		}
	}
	return value, nil
}

// Verify only explicit string-valued dictionary paths against their exact
// declarations. Template metadata and a complete XML instance remain unproved.
func readVFFComponentFieldValues(ctx context.Context, c *vffContainer, schemaName, packageMember, componentType string, paths []vffComponentFieldPath, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentFieldFacts, error) {
	if len(paths) == 0 {
		return nil, fmt.Errorf("component field values require an explicit type and paths")
	}
	seen := map[string]bool{}
	for _, path := range paths {
		if !vffSKCBindingName(path.field) || !vffSKCBindingName(path.valueType) || seen[path.field] {
			return nil, fmt.Errorf("component field paths must be unique explicit names")
		}
		seen[path.field] = true
	}
	scope, err := readVFFComponentFieldScope(ctx, c, schemaName, packageMember, componentType, componentLimits, limits)
	if err != nil {
		return nil, err
	}
	schema, b, selected := scope.schema, scope.b, scope.selected
	var plans []vffComponentStringField
	for _, path := range paths {
		p, err := scope.stringField(path)
		if err != nil {
			return nil, err
		}
		if len(p.profile.levels) != 0 {
			return nil, fmt.Errorf("enumeration field metadata remains unproved")
		}
		plans = append(plans, p)
	}
	r := &vffComponentClassificationReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: componentLimits.records, maxDepth: componentLimits.depth}, result: &vffComponentClassificationFacts{}}
	result := &vffComponentFieldFacts{schema: schema}
	for _, component := range selected {
		dictionaries, err := r.nestedDictionaries(schema.components.bindings[component].nestedDictionary, 13)
		if err != nil {
			return nil, err
		}
		for _, p := range plans {
			field, exists := dictionaries[p.path.field]
			if !exists {
				return nil, fmt.Errorf("selected component field dictionary is missing")
			}
			for _, entry := range field.entries {
				r.keep([]vffClassificationRecord{entry})
			}
			leaves, err := r.nestedDictionaries(field.nested, 19)
			if err != nil {
				return nil, err
			}
			leaf, exists := leaves[p.path.valueType]
			if !exists {
				return nil, fmt.Errorf("selected component value type dictionary is missing")
			}
			entry, exists := leaf.entries["value"]
			if !exists {
				return nil, fmt.Errorf("selected component value is missing")
			}
			for key, other := range leaf.entries {
				if key != "value" {
					r.keep([]vffClassificationRecord{other})
				}
			}
			metadata, err := r.fieldMetadata(schema, componentType, p.path.field, field, leaf, "string", 20, 26)
			if err != nil {
				return nil, err
			}
			if _, exists := leaf.entries["options"]; exists {
				return nil, fmt.Errorf("string field options remain unproved")
			}
			raw, err := r.stringValue(entry, 26, true)
			if err != nil {
				return nil, err
			}
			value, err := p.checkedValue(raw)
			if err != nil {
				return nil, err
			}
			facets, _, err := b.profileFacets(p.profile)
			if err != nil {
				return nil, err
			}
			result.values = append(result.values, vffComponentFieldValue{component: component, path: p.path, field: p.field, typeDeclaration: p.declaration, primitive: p.profile.primitive, facets: facets, raw: raw, value: value, fieldOffset: field.offset, dictionaryOffset: leaf.offset, valueOffset: entry.offset - 6, valueSHA256: sha256.Sum256(entry.data), metadata: metadata})
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.opaque = r.result.opaque
	sort.Slice(result.opaque, func(i, j int) bool { return result.opaque[i].offset < result.opaque[j].offset })
	return result, nil
}
