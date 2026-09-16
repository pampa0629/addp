package plan

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"sort"
	"strings"
	"unicode/utf8"
)

// Decode accepts exactly one strict plan document, including case-sensitive
// field names. Duplicate members are rejected before decoding into Go structs.
func Decode(data []byte) (Plan, error) {
	var p Plan
	if len(data) > MaxBytes || !utf8.Valid(data) {
		return p, fmt.Errorf("invalid plan encoding or size")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkJSONValue(decoder, 0); err != nil {
		return p, err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return p, fmt.Errorf("expected one JSON document")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&p); err != nil {
		return Plan{}, fmt.Errorf("invalid plan JSON")
	}
	// encoding/json accepts case-insensitive field matches. Check the exact
	// declared JSON tags, including optional members absent from a re-encoding.
	var input any
	if err := json.Unmarshal(data, &input); err != nil {
		return Plan{}, fmt.Errorf("invalid plan JSON")
	}
	if !knownKeys(input, reflect.TypeOf(p)) {
		return Plan{}, fmt.Errorf("non-canonical field name")
	}
	if err := Validate(p); err != nil {
		return Plan{}, err
	}
	return p, nil
}

func checkJSONValue(d *json.Decoder, depth int) error {
	if depth > MaxDepth*4+32 {
		return fmt.Errorf("JSON nesting exceeds budget")
	}
	token, err := d.Token()
	if err != nil {
		return fmt.Errorf("invalid plan JSON")
	}
	delim, ok := token.(json.Delim)
	if !ok {
		return nil
	}
	switch delim {
	case '{':
		seen := map[string]bool{}
		for d.More() {
			key, err := d.Token()
			if err != nil {
				return fmt.Errorf("invalid JSON member")
			}
			s, ok := key.(string)
			if !ok || seen[s] {
				return fmt.Errorf("duplicate or invalid JSON member")
			}
			seen[s] = true
			if err := checkJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	case '[':
		for d.More() {
			if err := checkJSONValue(d, depth+1); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid JSON delimiter")
	}
	if _, err := d.Token(); err != nil {
		return fmt.Errorf("invalid JSON close")
	}
	return nil
}
func knownKeys(input any, typed reflect.Type) bool {
	for typed.Kind() == reflect.Pointer {
		typed = typed.Elem()
	}
	switch x := input.(type) {
	case map[string]any:
		if typed.Kind() != reflect.Struct {
			return false
		}
		fields := map[string]reflect.Type{}
		for i := 0; i < typed.NumField(); i++ {
			f := typed.Field(i)
			key := strings.Split(f.Tag.Get("json"), ",")[0]
			fields[key] = f.Type
		}
		for k, v := range x {
			t, ok := fields[k]
			if !ok || !knownKeys(v, t) {
				return false
			}
		}
	case []any:
		if typed.Kind() != reflect.Slice {
			return false
		}
		for _, v := range x {
			if !knownKeys(v, typed.Elem()) {
				return false
			}
		}
	}
	return true
}

// boundedValue protects programmatic callers as well as JSON callers. It runs
// before recursion/type inference or encoding, so cyclic Go values cannot hang
// the validator and oversized strings cannot cause unbounded encoder allocation.
func boundedValue(value any) error {
	budget := MaxBytes * 2
	var walk func(reflect.Value, int) error
	walk = func(v reflect.Value, depth int) error {
		budget -= 8
		if budget < 0 || depth > MaxDepth*4+32 {
			return fmt.Errorf("plan exceeds resource budget")
		}
		switch v.Kind() {
		case reflect.Pointer, reflect.Interface:
			if !v.IsNil() {
				return walk(v.Elem(), depth+1)
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				if err := walk(v.Field(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Slice:
			if v.Len() > MaxBytes/8 {
				return fmt.Errorf("plan exceeds resource budget")
			}
			for i := 0; i < v.Len(); i++ {
				if err := walk(v.Index(i), depth+1); err != nil {
					return err
				}
			}
		case reflect.Map:
			if v.Len() > MaxBytes/8 {
				return fmt.Errorf("plan exceeds resource budget")
			}
			iter := v.MapRange()
			for iter.Next() {
				if err := walk(iter.Key(), depth+1); err != nil {
					return err
				}
				if err := walk(iter.Value(), depth+1); err != nil {
					return err
				}
			}
		case reflect.String:
			budget -= v.Len()
			if budget < 0 {
				return fmt.Errorf("plan exceeds resource budget")
			}
		}
		return nil
	}
	return walk(reflect.ValueOf(value), 0)
}

// CanonicalJSON normalizes value encodings and unordered sets on a private copy.
// It deliberately retains output, parameter, sort-key and row ordering.
func CanonicalJSON(p Plan) ([]byte, error) {
	if err := Validate(p); err != nil {
		return nil, err
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return nil, err
	}
	var clone Plan
	if err = json.Unmarshal(raw, &clone); err != nil {
		return nil, err
	}
	normalize(&clone)
	return json.Marshal(clone)
}
func Fingerprint(p Plan) (string, error) {
	data, err := CanonicalJSON(p)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:]), nil
}
func normalize(p *Plan) {
	if p.Parameters == nil {
		p.Parameters = []Parameter{}
	}
	if p.Assertions == nil {
		p.Assertions = []Assertion{}
	}
	sort.Slice(p.Nodes, func(i, j int) bool { return p.Nodes[i].ID < p.Nodes[j].ID })
	sort.Slice(p.Assertions, func(i, j int) bool {
		a, b := p.Assertions[i], p.Assertions[j]
		if a.Violation != b.Violation {
			return a.Violation < b.Violation
		}
		return a.Code < b.Code
	})
	for i := range p.Parameters {
		a := p.Parameters[i].Allowed
		for j := range a {
			a[j], _ = a[j].Canonical()
		}
		sort.Slice(a, func(i, j int) bool { return a[i].Text < a[j].Text })
	}
	for i := range p.Nodes {
		n := &p.Nodes[i]
		switch n.Op {
		case "filter":
			normalizeExpr(&n.Filter.Predicate)
		case "project":
			for j := range n.Project.Columns {
				normalizeExpr(&n.Project.Columns[j].Expr)
			}
		case "join":
			if n.Join.On != nil {
				normalizeExpr(n.Join.On)
			}
		case "aggregate":
			if n.Aggregate.Groups == nil {
				n.Aggregate.Groups = []Projection{}
			}
			for j := range n.Aggregate.Groups {
				normalizeExpr(&n.Aggregate.Groups[j].Expr)
			}
			for j := range n.Aggregate.Measures {
				if n.Aggregate.Measures[j].Value != nil {
					normalizeExpr(n.Aggregate.Measures[j].Value)
				}
			}
		case "constant_rows":
			if n.ConstantRows.Rows == nil {
				n.ConstantRows.Rows = [][]Literal{}
			}
			for j := range n.ConstantRows.Rows {
				for k := range n.ConstantRows.Rows[j] {
					n.ConstantRows.Rows[j][k], _ = n.ConstantRows.Rows[j][k].Canonical()
				}
			}
		case "date_buckets":
			normalizeExpr(&n.DateBuckets.Start)
			normalizeExpr(&n.DateBuckets.End)
		}
	}
}
func normalizeExpr(e *Expr) {
	if e.Literal != nil {
		*e.Literal, _ = e.Literal.Canonical()
	}
	for i := range e.Args {
		normalizeExpr(&e.Args[i])
	}
}
