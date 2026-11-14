package avro

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/addp/common/datatype"
)

const maxStructureDepth = 16
const maxStructureValues = 65536
const maxStructureJSON = 1 << 20

var avroFullName = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*(\.[A-Za-z_][A-Za-z0-9_]*)*$`)

// valueSchema is compiled once from the declared writer schema. It binds wire
// framing, logical interpretation and public field facts to the same type.
type valueSchema struct {
	field      datatype.FieldInfo
	codecName  string
	nullBranch int
	fixedSize  int
	children   []*valueSchema
	item       *valueSchema
}
type schemaParser struct {
	named  map[string]*valueSchema
	active map[string]bool
}

func recordSchema(raw []byte) ([]datatype.FieldInfo, map[string]interface{}, *valueSchema, error) {
	var schema map[string]interface{}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.UseNumber()
	if err := decoder.Decode(&schema); err != nil {
		return nil, nil, nil, fmt.Errorf("Avro schema: %w", err)
	}
	var trailing interface{}
	if decoder.Decode(&trailing) != io.EOF {
		return nil, nil, nil, fmt.Errorf("Avro schema has trailing data")
	}
	if schema["type"] != "record" {
		return nil, nil, nil, fmt.Errorf("Avro requires a record")
	}
	p := schemaParser{named: map[string]*valueSchema{}, active: map[string]bool{}}
	node, err := p.parse(schema, "", 0)
	if err != nil {
		return nil, nil, nil, err
	}
	count := 0
	for _, child := range node.children {
		if err := validateSchemaSize(child, 1, &count); err != nil {
			return nil, nil, nil, err
		}
	}
	return node.field.Fields, schema, node, nil
}
func validateSchemaSize(n *valueSchema, depth int, count *int) error {
	*count++
	if depth > maxStructureDepth || *count > maxFields {
		return fmt.Errorf("Avro schema exceeds structure budget")
	}
	for _, child := range n.children {
		if err := validateSchemaSize(child, depth+1, count); err != nil {
			return err
		}
	}
	if n.item != nil {
		return validateSchemaSize(n.item, depth+1, count)
	}
	return nil
}
func (p *schemaParser) parse(value interface{}, namespace string, depth int) (*valueSchema, error) {
	if depth > maxStructureDepth {
		return nil, fmt.Errorf("Avro schema exceeds depth budget")
	}
	if union, ok := value.([]interface{}); ok {
		if len(union) != 2 {
			return nil, fmt.Errorf("Avro only supports a null and one value union")
		}
		null := -1
		for i, v := range union {
			if primitiveName(v) == "null" {
				if null != -1 {
					return nil, fmt.Errorf("duplicate Avro null branch")
				}
				null = i
			}
		}
		if null < 0 {
			return nil, fmt.Errorf("Avro only supports a null and one value union")
		}
		child, err := p.parse(union[1-null], namespace, depth)
		if err != nil {
			return nil, err
		}
		n := *child
		n.field.Nullable = true
		n.nullBranch = null
		return &n, nil
	}
	object, _ := value.(map[string]interface{})
	kind := primitiveName(value)
	if object != nil {
		if _, logical := object["logicalType"]; logical {
			kind = ""
		}
	}
	if object == nil && !primitiveTypeName(kind) {
		full := qualifiedName(kind, namespace)
		if p.active[full] {
			return nil, fmt.Errorf("recursive Avro schema is unsupported")
		}
		if n := p.named[full]; n != nil {
			return n, nil
		}
		return nil, fmt.Errorf("undefined Avro named type")
	}
	if kind == "record" {
		name, _ := object["name"].(string)
		if name == "" || primitiveTypeName(name[strings.LastIndexByte(name, '.')+1:]) {
			return nil, fmt.Errorf("invalid Avro record name")
		}
		if raw, ok := object["namespace"]; ok {
			var valid bool
			namespace, valid = raw.(string)
			if !valid {
				return nil, fmt.Errorf("invalid Avro record namespace")
			}
		}
		full := qualifiedName(name, namespace)
		if !avroFullName.MatchString(full) {
			return nil, fmt.Errorf("invalid Avro record fullname")
		}
		if p.active[full] || p.named[full] != nil {
			return nil, fmt.Errorf("duplicate Avro named declaration")
		}
		p.active[full] = true
		list, ok := object["fields"].([]interface{})
		if !ok || len(list) == 0 || len(list) > maxFields {
			return nil, fmt.Errorf("Avro record requires 1..%d fields", maxFields)
		}
		node := &valueSchema{field: datatype.FieldInfo{Type: datatype.FieldTypeJSON, NativeType: full}, nullBranch: -1}
		namespace = recordNamespace(map[string]interface{}{"name": full})
		seen := map[string]bool{}
		for i, raw := range list {
			f, ok := raw.(map[string]interface{})
			if !ok {
				return nil, fmt.Errorf("invalid Avro field")
			}
			name, _ := f["name"].(string)
			if name == "" || seen[name] {
				return nil, fmt.Errorf("invalid or duplicate Avro field name")
			}
			seen[name] = true
			child, err := p.parse(f["type"], namespace, depth+1)
			if err != nil {
				return nil, fmt.Errorf("Avro field %q: %w", name, err)
			}
			copy := *child
			copy.field.Name = name
			copy.field.OrdinalPosition = i + 1
			copy.field.Comment, _ = f["doc"].(string)
			node.children = append(node.children, &copy)
			node.field.Fields = append(node.field.Fields, copy.field)
		}
		delete(p.active, full)
		node.codecName = fmt.Sprintf("addp.avro.Type%d", len(p.named))
		p.named[full] = node
		return node, nil
	}
	if kind == "array" {
		item, err := p.parse(object["items"], namespace, depth+1)
		if err != nil {
			return nil, err
		}
		element := item.field
		element.Name = ""
		element.OrdinalPosition = 0
		return &valueSchema{field: datatype.FieldInfo{Type: datatype.FieldTypeArray, NativeType: "array", ElementType: element.Type, Element: &element}, nullBranch: -1, item: item}, nil
	}
	switch kind {
	case "boolean", "int", "long", "float", "double", "bytes", "string", "null", "map", "enum", "fixed", "":
	default:
		full := qualifiedName(kind, namespace)
		if p.active[full] {
			return nil, fmt.Errorf("recursive Avro schema is unsupported")
		}
		if n := p.named[full]; n != nil {
			return n, nil
		}
	}
	definition, err := fieldDefinition(value, namespace)
	if err != nil {
		return nil, err
	}
	node := &valueSchema{field: definition, nullBranch: -1}
	if definition.Type == datatype.FieldTypeDecimal && primitiveName(value) == "" {
		if typ, _ := object["type"].(string); typ == "fixed" {
			size, _ := object["size"].(json.Number)
			n, _ := size.Int64()
			node.fixedSize = int(n)
		}
	}
	if node.fixedSize > 0 {
		if p.named[definition.NativeType] != nil || p.active[definition.NativeType] {
			return nil, fmt.Errorf("duplicate Avro named declaration")
		}
		node.codecName = fmt.Sprintf("addp.avro.Type%d", len(p.named))
		p.named[definition.NativeType] = node
	}
	return node, nil
}

func primitiveTypeName(name string) bool {
	switch name {
	case "null", "boolean", "int", "long", "float", "double", "bytes", "string":
		return true
	}
	return false
}
