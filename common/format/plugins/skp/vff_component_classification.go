package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type vffComponentClassification struct {
	entityID                                           uint64
	component                                          [16]byte
	name, schema, schemaType, declaredType             string
	definitionOffset, mappingOffset, declarationOffset int
	mappingSHA256, declarationSHA256                   [32]byte
	nestedDictionary                                   vffClassificationRecord
}
type vffComponentClassificationFacts struct {
	modelSHA256 [32]byte
	definitions int
	bindings    []vffComponentClassification
	opaque      []vffOpaqueRecord
}
type vffClassificationRecord struct {
	tag    string
	data   []byte
	offset int
}
type vffComponentClassificationReader struct {
	*vffRecords
	result *vffComponentClassificationFacts
}

func (r *vffComponentClassificationReader) recordsAt(data []byte, depth, offset int) ([]vffClassificationRecord, error) {
	var nodes []vffClassificationRecord
	err := r.each(data, depth, offset, func(tag string, b []byte, p int) error {
		nodes = append(nodes, vffClassificationRecord{tag, b, p})
		return nil
	})
	return nodes, err
}
func (r *vffComponentClassificationReader) one(nodes []vffClassificationRecord, tag string, required bool) (vffClassificationRecord, error) {
	var found vffClassificationRecord
	for _, n := range nodes {
		if n.tag == tag {
			if found.tag != "" {
				return found, fmt.Errorf("duplicate component classification record %s", tag)
			}
			found = n
		}
	}
	if required && found.tag == "" {
		return found, fmt.Errorf("missing component classification record %s", tag)
	}
	return found, nil
}
func (r *vffComponentClassificationReader) keep(nodes []vffClassificationRecord, known ...string) {
	for _, n := range nodes {
		matched := false
		for _, tag := range known {
			if n.tag == tag {
				matched = true
				break
			}
		}
		if !matched {
			r.result.opaque = append(r.result.opaque, vffOpaque("model.dat", "component-classification/"+n.tag, n.data, n.offset-6))
		}
	}
}
func (r *vffComponentClassificationReader) level(parent vffClassificationRecord, tag string, depth int) (vffClassificationRecord, error) {
	nodes, err := r.recordsAt(parent.data, depth, parent.offset)
	if err != nil {
		return vffClassificationRecord{}, err
	}
	r.keep(nodes, tag)
	return r.one(nodes, tag, true)
}
func vffClassificationText(n vffClassificationRecord, empty bool) (string, error) {
	if !utf8.Valid(n.data) || (!empty && len(n.data) == 0) || strings.ContainsRune(string(n.data), 0) {
		return "", fmt.Errorf("invalid component classification text")
	}
	return string(n.data), nil
}
func readVFFComponentClassifications(ctx context.Context, c *vffContainer, schemaName string, limits vffClassificationLimits) (*vffComponentClassificationFacts, error) {
	if c == nil || schemaName == "" || !utf8.ValidString(schemaName) || limits.bytes <= 0 || limits.records <= 0 || limits.depth <= 0 {
		return nil, fmt.Errorf("component classifications require explicit budgets")
	}
	raw := c.members["model.dat"]
	if len(raw) == 0 || len(raw) > limits.bytes {
		return nil, fmt.Errorf("component classification model exceeds budget")
	}
	r := &vffComponentClassificationReader{&vffRecords{ctx: ctx, maxRecords: limits.records, maxDepth: limits.depth}, &vffComponentClassificationFacts{modelSHA256: sha256.Sum256(raw)}}
	root, err := r.recordsAt(raw, 0, 0)
	if err != nil {
		return nil, err
	}
	if len(root) != 1 || root[0].tag != "F401" {
		return nil, fmt.Errorf("component classifications require one model root")
	}
	current := root[0]
	for depth, tag := range []string{"F901", "7017", "7117"} {
		current, err = r.level(current, tag, depth+1)
		if err != nil {
			return nil, err
		}
	}
	nodes, err := r.recordsAt(current.data, 4, current.offset)
	if err != nil {
		return nil, err
	}
	r.keep(nodes, "7C15")
	ids := map[uint64]bool{}
	guids := map[[16]byte]bool{}
	for _, node := range nodes {
		if node.tag != "7C15" {
			continue
		}
		r.result.definitions++
		definition, err := r.recordsAt(node.data, 5, node.offset)
		if err != nil {
			return nil, err
		}
		r.keep(definition, "8813", "7D15", "7E15")
		guid, err := r.one(definition, "7D15", true)
		if err != nil {
			return nil, err
		}
		if len(guid.data) != 16 {
			return nil, fmt.Errorf("invalid component identifier")
		}
		var identity [16]byte
		copy(identity[:], guid.data)
		if identity == ([16]byte{}) || guids[identity] {
			return nil, fmt.Errorf("duplicate or empty component identifier")
		}
		guids[identity] = true
		nameNode, err := r.one(definition, "7E15", true)
		if err != nil {
			return nil, err
		}
		name, err := vffClassificationText(nameNode, true)
		if err != nil {
			return nil, err
		}
		geometry, err := r.one(definition, "8813", true)
		if err != nil {
			return nil, err
		}
		properties, err := r.level(geometry, "D007", 6)
		if err != nil {
			return nil, err
		}
		entity, err := r.level(properties, "DC05", 7)
		if err != nil {
			return nil, err
		}
		entityNodes, err := r.recordsAt(entity.data, 8, entity.offset)
		if err != nil {
			return nil, err
		}
		r.keep(entityNodes, "DE05", "DD05")
		idNode, err := r.one(entityNodes, "DE05", true)
		if err != nil {
			return nil, err
		}
		id, err := vffRecordID(idNode.data)
		if err != nil {
			return nil, err
		}
		if ids[id] {
			return nil, fmt.Errorf("duplicate component entity ID")
		}
		ids[id] = true
		dictionaries, err := r.one(entityNodes, "DD05", false)
		if err != nil {
			return nil, err
		}
		if dictionaries.tag == "" {
			continue
		}
		bindings, err := r.componentDictionaries(dictionaries, schemaName)
		if err != nil {
			return nil, err
		}
		for _, binding := range bindings {
			binding.entityID = id
			binding.component = identity
			binding.name = name
			binding.definitionOffset = node.offset - 6
			r.result.bindings = append(r.result.bindings, binding)
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	sort.Slice(r.result.opaque, func(i, j int) bool { return r.result.opaque[i].offset < r.result.opaque[j].offset })
	return r.result, nil
}

type vffClassificationDictionary struct {
	entries map[string]vffClassificationRecord
	nested  vffClassificationRecord
	offset  int
}

func (r *vffComponentClassificationReader) componentDictionaries(parent vffClassificationRecord, schemaName string) ([]vffComponentClassification, error) {
	var err error
	for depth, tag := range []string{"B136", "B236"} {
		parent, err = r.level(parent, tag, 9+depth)
		if err != nil {
			return nil, err
		}
	}
	dictionaries, order, err := r.dictionaries(parent, 11)
	if err != nil {
		return nil, err
	}
	applied, exists := dictionaries["AppliedSchemaTypes"]
	if !exists {
		for _, dictionary := range dictionaries {
			for _, entry := range dictionary.entries {
				r.keep([]vffClassificationRecord{entry})
			}
		}
		return nil, nil
	}
	mapping, selected := applied.entries[schemaName]
	// The declaration order is retained by reading the original key records.
	for _, name := range order {
		if name == "AppliedSchemaTypes" {
			continue
		}
		if !selected || name != schemaName {
			dict := dictionaries[name]
			for _, n := range dict.entries {
				r.keep([]vffClassificationRecord{n})
			}
		}
	}
	for name, entry := range applied.entries {
		if name != schemaName {
			r.keep([]vffClassificationRecord{entry})
		}
	}
	if !selected {
		return nil, nil
	}
	schemaType, err := r.stringValue(mapping, 14, false)
	if err != nil {
		return nil, err
	}
	dict, ok := dictionaries[schemaName]
	if !ok {
		return nil, fmt.Errorf("component classification dictionary missing")
	}
	declaration, ok := dict.entries["SchemaType"]
	if !ok {
		return nil, fmt.Errorf("component classification SchemaType missing")
	}
	declared, err := r.stringValue(declaration, 14, false)
	if err != nil {
		return nil, err
	}
	// SchemaType stores a namespace URI followed by ':' and the local type.
	cut := strings.LastIndex(declared, ":")
	if cut < 1 || cut == len(declared)-1 {
		return nil, fmt.Errorf("invalid component SchemaType")
	}
	namespace, local := declared[:cut], declared[cut+1:]
	if namespace == "" || local != schemaType || strings.ContainsAny(schemaType, ": \t\r\n") {
		return nil, fmt.Errorf("component classification type mismatch")
	}
	binding := vffComponentClassification{schema: schemaName, schemaType: schemaType, declaredType: declared,
		mappingOffset: mapping.offset - 6, declarationOffset: declaration.offset - 6,
		mappingSHA256: sha256.Sum256(mapping.data), declarationSHA256: sha256.Sum256(declaration.data), nestedDictionary: dict.nested}
	for key, n := range dict.entries {
		if key != "SchemaType" {
			r.keep([]vffClassificationRecord{n})
		}
	}
	return []vffComponentClassification{binding}, nil
}
func (r *vffComponentClassificationReader) dictionaries(parent vffClassificationRecord, depth int) (map[string]vffClassificationDictionary, []string, error) {
	nodes, err := r.recordsAt(parent.data, depth, parent.offset)
	if err != nil {
		return nil, nil, err
	}
	r.keep(nodes, "B336")
	dictionaries := map[string]vffClassificationDictionary{}
	var order []string
	for _, node := range nodes {
		if node.tag != "B336" {
			continue
		}
		fields, err := r.recordsAt(node.data, depth+1, node.offset)
		if err != nil {
			return nil, nil, err
		}
		r.keep(fields, "B436", "B536")
		nameNode, err := r.one(fields, "B436", true)
		if err != nil {
			return nil, nil, err
		}
		name, err := vffClassificationText(nameNode, false)
		if err != nil {
			return nil, nil, err
		}
		if _, exists := dictionaries[name]; exists {
			return nil, nil, fmt.Errorf("duplicate component dictionary")
		}
		entryNode, err := r.one(fields, "B536", true)
		if err != nil {
			return nil, nil, err
		}
		entries, err := r.dictionaryEntries(entryNode, depth+2)
		if err != nil {
			return nil, nil, err
		}
		nested, err := r.one(fields, "B036", false)
		if err != nil {
			return nil, nil, err
		}
		dictionaries[name] = vffClassificationDictionary{entries: entries, nested: nested, offset: node.offset - 6}
		order = append(order, name)
	}
	return dictionaries, order, nil
}

func (r *vffComponentClassificationReader) dictionaryEntries(parent vffClassificationRecord, depth int) (map[string]vffClassificationRecord, error) {
	nodes, err := r.recordsAt(parent.data, depth, parent.offset)
	if err != nil {
		return nil, err
	}
	entries := map[string]vffClassificationRecord{}
	for i := 0; i < len(nodes); i += 2 {
		if i+1 >= len(nodes) || nodes[i].tag != "B636" || nodes[i+1].tag != "A438" {
			return nil, fmt.Errorf("invalid component dictionary entry")
		}
		key, err := vffClassificationText(nodes[i], false)
		if err != nil {
			return nil, err
		}
		if _, exists := entries[key]; exists {
			return nil, fmt.Errorf("duplicate component dictionary key")
		}
		entries[key] = nodes[i+1]
	}
	return entries, nil
}
func (r *vffComponentClassificationReader) stringValue(parent vffClassificationRecord, depth int, empty bool) (string, error) {
	nodes, err := r.recordsAt(parent.data, depth, parent.offset)
	if err != nil {
		return "", err
	}
	if len(nodes) != 1 || nodes[0].tag != "AD38" {
		return "", fmt.Errorf("classification requires a string value")
	}
	return vffClassificationText(nodes[0], empty)
}
