package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
)

type vffComponentBoolean struct {
	value  bool
	offset int
	sha256 [32]byte
}
type vffComponentFilterMembership struct {
	member string
	sha256 [32]byte
	rule   vffSKCFilterRule
	listed bool
}
type vffComponentFieldMetadata struct {
	attributeType           string
	typeOffset              int
	typeSHA256              [32]byte
	fieldHidden, leafHidden vffComponentBoolean
	filters                 []vffComponentFilterMembership
}
type vffComponentChoiceOption struct {
	name                   string
	offset                 int
	sha256                 [32]byte
	reference, declaration vffSKCDeclarationWitness
	via                    []vffSKCSchemaLink
}
type vffComponentChoiceFact struct {
	component int
	path      vffComponentFieldPath
	field     vffSKCFieldCandidate
	metadata  vffComponentFieldMetadata
	options   []vffComponentChoiceOption
}
type vffComponentChoiceFacts struct {
	schema  *vffComponentSchemaFacts
	choices []vffComponentChoiceFact
	opaque  []vffOpaqueRecord
}

func (r *vffComponentClassificationReader) hidden(dictionary vffClassificationDictionary, depth int) (vffComponentBoolean, error) {
	entry, exists := dictionary.entries["is_hidden"]
	if !exists {
		return vffComponentBoolean{}, fmt.Errorf("classification hidden flag is missing")
	}
	nodes, err := r.recordsAt(entry.data, depth, entry.offset)
	if err != nil {
		return vffComponentBoolean{}, err
	}
	if len(nodes) != 1 || nodes[0].tag != "AA38" || len(nodes[0].data) != 1 || nodes[0].data[0] > 1 {
		return vffComponentBoolean{}, fmt.Errorf("classification hidden flag requires an exact boolean")
	}
	return vffComponentBoolean{nodes[0].data[0] == 1, entry.offset - 6, sha256.Sum256(entry.data)}, nil
}

func (r *vffComponentClassificationReader) fieldMetadata(schema *vffComponentSchemaFacts, componentType, fieldName string, field, leaf vffClassificationDictionary, expected string) (vffComponentFieldMetadata, error) {
	entry, exists := leaf.entries["attribute_type"]
	if !exists {
		return vffComponentFieldMetadata{}, fmt.Errorf("classification attribute type marker is missing")
	}
	role, err := r.stringValue(entry, 26, false)
	if err != nil {
		return vffComponentFieldMetadata{}, err
	}
	if role != expected {
		return vffComponentFieldMetadata{}, fmt.Errorf("classification attribute type marker differs from the proved schema role")
	}
	parent, err := r.hidden(field, 20)
	if err != nil {
		return vffComponentFieldMetadata{}, err
	}
	child, err := r.hidden(leaf, 26)
	if err != nil {
		return vffComponentFieldMetadata{}, err
	}
	result := vffComponentFieldMetadata{attributeType: role, typeOffset: entry.offset - 6, typeSHA256: sha256.Sum256(entry.data), fieldHidden: parent, leafHidden: child}
	for _, filter := range schema.document.filters {
		if filter.member != schema.document.rootSchema+".filter" {
			continue
		}
		for _, rule := range filter.rules {
			if err := r.ctx.Err(); err != nil {
				return vffComponentFieldMetadata{}, err
			}
			if rule.typeName != "" && rule.typeName != componentType {
				continue
			}
			listed := false
			for _, name := range rule.attributes {
				listed = listed || name == fieldName
			}
			result.filters = append(result.filters, vffComponentFilterMembership{filter.member, filter.sha256, rule, listed})
		}
	}
	return result, nil
}

// Direct choice declaration identities only. Substitutions, selected values,
// effective UI visibility and a complete instance are deliberately not inferred.
func (b *vffSKCBinder) componentChoiceOptions(field *vffXMLNode) ([]vffComponentChoiceOption, error) {
	if _, typed := field.attrs["type"]; typed {
		return nil, fmt.Errorf("choice metadata requires an anonymous field type")
	}
	current := field
	for _, kind := range []string{"complexType", "choice"} {
		var selected *vffXMLNode
		for _, child := range current.children {
			if err := b.tick(0); err != nil {
				return nil, err
			}
			if child.name.Local == "annotation" {
				continue
			}
			if child.name.Local != kind || selected != nil {
				return nil, fmt.Errorf("classification choice shape is not uniquely bound")
			}
			selected = child
		}
		if selected == nil {
			return nil, fmt.Errorf("classification direct choice declaration is missing")
		}
		allowed := " id "
		if kind == "choice" {
			allowed += "minOccurs maxOccurs "
		}
		for name := range selected.attrs {
			if !strings.Contains(allowed, " "+name+" ") {
				return nil, fmt.Errorf("classification choice declaration attribute is unproved")
			}
		}
		current = selected
	}
	var options []vffComponentChoiceOption
	seen := map[string]bool{}
	for _, child := range current.children {
		if err := b.tick(0); err != nil {
			return nil, err
		}
		if child.name.Local == "annotation" {
			continue
		}
		if child.name.Local != "element" || child.attrs["ref"] == "" {
			return nil, fmt.Errorf("classification choice requires direct element references")
		}
		for name := range child.attrs {
			if !strings.Contains(" ref minOccurs maxOccurs id ", " "+name+" ") {
				return nil, fmt.Errorf("classification choice reference attribute is unproved")
			}
		}
		for _, nested := range child.children {
			if nested.name.Local != "annotation" {
				return nil, fmt.Errorf("classification choice reference has unproved content")
			}
		}
		name, err := b.qname(child, child.attrs["ref"])
		if err != nil {
			return nil, err
		}
		target, err := b.resolveDeclaration(child, name, "element")
		if err != nil {
			return nil, err
		}
		if target == nil || name.Space == "" {
			return nil, fmt.Errorf("classification choice reference is not uniquely bound")
		}
		literal := name.Space + ":" + name.Local
		if seen[literal] {
			return nil, fmt.Errorf("classification direct choice reference is ambiguous")
		}
		seen[literal] = true
		options = append(options, vffComponentChoiceOption{name: literal, reference: b.witness(child), declaration: b.witness(target.node), via: target.via})
	}
	if len(options) == 0 {
		return nil, fmt.Errorf("classification direct choice has no options")
	}
	return options, nil
}

func readVFFComponentChoiceMetadata(ctx context.Context, c *vffContainer, schemaName, packageMember, componentType string, paths []vffComponentFieldPath, componentLimits vffClassificationLimits, limits vffSKCBindingLimits) (*vffComponentChoiceFacts, error) {
	seen := map[string]bool{}
	if len(paths) == 0 {
		return nil, fmt.Errorf("choice metadata requires explicit paths")
	}
	for _, path := range paths {
		if !vffSKCBindingName(path.field) || path.field != path.valueType || seen[path.field] {
			return nil, fmt.Errorf("choice metadata requires unique same-name field paths")
		}
		seen[path.field] = true
	}
	scope, err := readVFFComponentFieldScope(ctx, c, schemaName, packageMember, componentType, componentLimits, limits)
	if err != nil {
		return nil, err
	}
	type plan struct {
		field   vffSKCFieldCandidate
		options []vffComponentChoiceOption
	}
	var plans []plan
	for _, path := range paths {
		field, err := scope.element(path.field)
		if err != nil {
			return nil, err
		}
		node := scope.b.nodes[vffSKCNodeKey{field.declaration.member, field.declaration.begin}]
		options, err := scope.b.componentChoiceOptions(node)
		if err != nil {
			return nil, err
		}
		plans = append(plans, plan{*field, options})
	}
	r := &vffComponentClassificationReader{vffRecords: &vffRecords{ctx: ctx, maxRecords: componentLimits.records, maxDepth: componentLimits.depth}, result: &vffComponentClassificationFacts{}}
	result := &vffComponentChoiceFacts{schema: scope.schema}
	for _, component := range scope.selected {
		dictionaries, err := r.nestedDictionaries(scope.schema.components.bindings[component].nestedDictionary, 13)
		if err != nil {
			return nil, err
		}
		for i, path := range paths {
			field, exists := dictionaries[path.field]
			if !exists {
				return nil, fmt.Errorf("choice field dictionary is missing")
			}
			leaves, err := r.nestedDictionaries(field.nested, 19)
			if err != nil {
				return nil, err
			}
			leaf, exists := leaves[path.valueType]
			if !exists {
				return nil, fmt.Errorf("choice metadata leaf is missing")
			}
			metadata, err := r.fieldMetadata(scope.schema, componentType, path.field, field, leaf, "choice")
			if err != nil {
				return nil, err
			}
			entry, exists := leaf.entries["options"]
			if !exists {
				return nil, fmt.Errorf("choice options are missing")
			}
			wrapped, err := r.recordsAt(entry.data, 26, entry.offset)
			if err != nil {
				return nil, err
			}
			if len(wrapped) != 1 || wrapped[0].tag != "AE38" {
				return nil, fmt.Errorf("choice options require an exact array")
			}
			items, err := r.recordsAt(wrapped[0].data, 27, wrapped[0].offset)
			if err != nil {
				return nil, err
			}
			if len(items) != len(plans[i].options) {
				return nil, fmt.Errorf("choice option coverage differs from direct declarations")
			}
			candidates := map[string]vffComponentChoiceOption{}
			for _, option := range plans[i].options {
				candidates[option.name] = option
			}
			fact := vffComponentChoiceFact{component: component, path: path, field: plans[i].field, metadata: metadata}
			for _, item := range items {
				if item.tag != "A438" {
					return nil, fmt.Errorf("choice option item is not a wrapped value")
				}
				name, err := r.stringValue(item, 28, false)
				if err != nil {
					return nil, err
				}
				option, exists := candidates[name]
				if !exists {
					return nil, fmt.Errorf("choice option is missing, duplicate or not an exact reference")
				}
				delete(candidates, name)
				option.offset, option.sha256 = item.offset-6, sha256.Sum256(item.data)
				fact.options = append(fact.options, option)
			}
			for _, n := range field.entries {
				r.keep([]vffClassificationRecord{n})
			}
			for key, n := range leaf.entries {
				if key != "attribute_type" && key != "is_hidden" && key != "options" {
					r.keep([]vffClassificationRecord{n})
				}
			}
			result.choices = append(result.choices, fact)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	result.opaque = r.result.opaque
	sort.Slice(result.opaque, func(i, j int) bool { return result.opaque[i].offset < result.opaque[j].offset })
	return result, nil
}
