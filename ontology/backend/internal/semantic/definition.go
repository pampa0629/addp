// Package semantic implements bounded, deterministic ontology definition and
// hypothetical classification. It does not authenticate callers or fetch facts.
package semantic

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"regexp"
	"slices"
	"strings"
	"unicode/utf8"
)

const (
	ContractVersion        = "addp.semantic.v1"
	CompilerVersion        = "cel.dev/cel-go@v0.32.0"
	maxText                = 4096
	maxInputs              = 16
	maxCost         uint64 = 256
)

type Kind string

const (
	Boolean Kind = "bool"
	String  Kind = "string"
)

type Scope struct {
	TenantID   uint64 `json:"tenant_id"`
	OntologyID string `json:"ontology_id"`
	Revision   uint64 `json:"revision"`
}

type Class struct {
	ID      string   `json:"id"`
	Name    string   `json:"name"`
	Parents []string `json:"parents"`
}

type Property struct {
	ID      string   `json:"id"`
	ClassID string   `json:"class_id"`
	Key     string   `json:"key"`
	Name    string   `json:"name"`
	Kind    Kind     `json:"kind"`
	Enum    []string `json:"enum"`
}

// Relation declares type-level semantics, not observed instance edges.
type Relation struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	From       string `json:"from"`
	To         string `json:"to"`
	InverseID  string `json:"inverse_id,omitempty"`
	Transitive bool   `json:"transitive"`
}

type AbsencePolicy string

const (
	AbsenceUnknown AbsencePolicy = "unknown"
	AbsenceFalse   AbsencePolicy = "false"
)

type Input struct {
	Variable   string        `json:"variable"`
	PropertyID string        `json:"property_id"`
	OnAbsent   AbsencePolicy `json:"on_absent"`
}

type Rule struct {
	ID         string  `json:"id"`
	ClassID    string  `json:"class_id"`
	Expression string  `json:"expression"`
	Basis      string  `json:"basis"`
	Inputs     []Input `json:"inputs"`
}

// Definition currently accepts native members only. External owner capture and
// publication are intentionally not represented by editable placeholder fields.
type Definition struct {
	Scope      Scope      `json:"scope"`
	Classes    []Class    `json:"classes"`
	Properties []Property `json:"properties"`
	Relations  []Relation `json:"relations"`
	Rules      []Rule     `json:"rules"`
}

type Snapshot struct {
	definition Definition
	canonical  []byte
	digest     string
	ancestors  map[string][]string
	rules      map[string]*compiledRule
}

// Freeze validates and compiles a native definition. The returned snapshot has
// no mutable aliases to caller-owned input. It does not publish a revision.
func Freeze(d Definition) (*Snapshot, error) {
	ancestors, err := validate(d)
	if err != nil {
		return nil, err
	}
	// JSON round-trip copies every nested slice before canonical normalization.
	data, err := json.Marshal(d)
	if err != nil {
		return nil, err
	}
	if len(data) > 1<<20 {
		return nil, fmt.Errorf("definition_size_limit")
	}
	var frozen Definition
	if err := json.Unmarshal(data, &frozen); err != nil {
		return nil, err
	}
	d = frozen
	normalize(&d)
	s := &Snapshot{definition: d, ancestors: ancestors, rules: map[string]*compiledRule{}}
	properties := map[string]Property{}
	for _, p := range d.Properties {
		properties[p.ID] = p
	}
	for _, r := range d.Rules {
		compiled, err := compile(r, properties)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", r.ID, err)
		}
		s.rules[r.ID] = compiled
	}
	s.canonical, err = json.Marshal(struct {
		Contract   string     `json:"contract"`
		Compiler   string     `json:"compiler"`
		Definition Definition `json:"definition"`
	}{ContractVersion, CompilerVersion, d})
	if err != nil {
		return nil, err
	}
	h := sha256.Sum256(s.canonical)
	s.digest = hex.EncodeToString(h[:])
	return s, nil
}

func (s *Snapshot) Digest() string        { return s.digest }
func (s *Snapshot) CanonicalJSON() []byte { return slices.Clone(s.canonical) }

// Ancestors returns the complete, sorted type ancestry (excluding the class).
// Rejected or absent classes never masquerade as classes with no parents.
func (s *Snapshot) Ancestors(classID string) ([]string, error) {
	a, ok := s.ancestors[classID]
	if !ok {
		return nil, fmt.Errorf("unknown_class")
	}
	return slices.Clone(a), nil
}

var identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)

func validText(value string, max int) bool {
	return strings.TrimSpace(value) != "" && len(value) <= max && utf8.ValidString(value)
}

func validate(d Definition) (map[string][]string, error) {
	if d.Scope.TenantID == 0 || d.Scope.Revision == 0 || !identifier.MatchString(d.Scope.OntologyID) {
		return nil, fmt.Errorf("invalid_scope")
	}
	if len(d.Classes) == 0 || len(d.Classes) > 64 || len(d.Properties) > 256 || len(d.Relations) > 128 || len(d.Rules) > 64 {
		return nil, fmt.Errorf("member_limit")
	}
	ids := map[string]bool{}
	unique := func(id string) bool {
		if !identifier.MatchString(id) || ids[id] {
			return false
		}
		ids[id] = true
		return true
	}
	classes := map[string]Class{}
	for _, c := range d.Classes {
		if !unique(c.ID) || !validText(c.Name, 256) || len(c.Parents) > 8 {
			return nil, fmt.Errorf("invalid_class: %s", c.ID)
		}
		classes[c.ID] = c
	}
	ancestors := map[string][]string{}
	visiting := map[string]bool{}
	var visit func(string, int) error
	visit = func(id string, depth int) error {
		if visiting[id] {
			return fmt.Errorf("inheritance_cycle: %s", id)
		}
		if depth > 32 {
			return fmt.Errorf("inheritance_depth_limit")
		}
		if _, ok := ancestors[id]; ok {
			return nil
		}
		c, ok := classes[id]
		if !ok {
			return fmt.Errorf("unknown_parent: %s", id)
		}
		visiting[id] = true
		parents, all := map[string]bool{}, map[string]bool{}
		for _, parent := range c.Parents {
			if parents[parent] {
				return fmt.Errorf("duplicate_parent: %s", parent)
			}
			parents[parent] = true
			if err := visit(parent, depth+1); err != nil {
				return err
			}
			all[parent] = true
			for _, a := range ancestors[parent] {
				all[a] = true
			}
		}
		list := []string{}
		for a := range all {
			list = append(list, a)
		}
		slices.Sort(list)
		ancestors[id] = list
		visiting[id] = false
		return nil
	}
	for _, c := range d.Classes {
		if err := visit(c.ID, 0); err != nil {
			return nil, err
		}
	}
	// The depth check must also cover paths to previously memoized ancestors.
	var height func(string) int
	heights := map[string]int{}
	height = func(id string) int {
		if n, ok := heights[id]; ok {
			return n
		}
		n := 0
		for _, p := range classes[id].Parents {
			n = max(n, 1+height(p))
		}
		heights[id] = n
		return n
	}
	for id := range classes {
		if height(id) > 32 {
			return nil, fmt.Errorf("inheritance_depth_limit")
		}
	}
	properties := map[string]Property{}
	for _, p := range d.Properties {
		if !unique(p.ID) || classes[p.ClassID].ID == "" || !identifier.MatchString(p.Key) || !validText(p.Name, 256) {
			return nil, fmt.Errorf("invalid_property: %s", p.ID)
		}
		if p.Kind != Boolean && p.Kind != String {
			return nil, fmt.Errorf("unsupported_property_type: %s", p.ID)
		}
		if len(p.Enum) > 64 || (p.Kind != String && len(p.Enum) != 0) {
			return nil, fmt.Errorf("invalid_enum: %s", p.ID)
		}
		values := map[string]bool{}
		for _, v := range p.Enum {
			if len(v) > maxText || !utf8.ValidString(v) || values[v] {
				return nil, fmt.Errorf("invalid_enum: %s", p.ID)
			}
			values[v] = true
		}
		properties[p.ID] = p
	}
	for id := range classes {
		keys := map[string]string{}
		for _, p := range d.Properties {
			if p.ClassID != id && !slices.Contains(ancestors[id], p.ClassID) {
				continue
			}
			if keys[p.Key] != "" {
				return nil, fmt.Errorf("ambiguous_property: %s.%s", id, p.Key)
			}
			keys[p.Key] = p.ID
		}
	}
	relations := map[string]Relation{}
	for _, r := range d.Relations {
		if !unique(r.ID) || !validText(r.Name, 256) || classes[r.From].ID == "" || classes[r.To].ID == "" {
			return nil, fmt.Errorf("invalid_relation: %s", r.ID)
		}
		// v1 only permits transitivity on one exact endpoint type.
		if r.Transitive && r.From != r.To {
			return nil, fmt.Errorf("transitive_endpoint_mismatch: %s", r.ID)
		}
		relations[r.ID] = r
	}
	for _, r := range d.Relations {
		if r.InverseID == "" {
			continue
		}
		i, ok := relations[r.InverseID]
		if !ok || i.InverseID != r.ID || i.From != r.To || i.To != r.From || i.Transitive != r.Transitive {
			return nil, fmt.Errorf("invalid_inverse: %s", r.ID)
		}
	}
	for _, r := range d.Rules {
		if !unique(r.ID) || classes[r.ClassID].ID == "" || !validText(r.Expression, maxText) || !validText(r.Basis, maxText) || len(r.Inputs) > maxInputs {
			return nil, fmt.Errorf("invalid_rule: %s", r.ID)
		}
		variables, bound := map[string]bool{}, map[string]bool{}
		for _, input := range r.Inputs {
			p, ok := properties[input.PropertyID]
			if !identifier.MatchString(input.Variable) || variables[input.Variable] || bound[input.PropertyID] || !ok {
				return nil, fmt.Errorf("invalid_input: %s", r.ID)
			}
			if p.ClassID != r.ClassID && !slices.Contains(ancestors[r.ClassID], p.ClassID) {
				return nil, fmt.Errorf("input_class_mismatch: %s", r.ID)
			}
			if input.OnAbsent != AbsenceUnknown && (input.OnAbsent != AbsenceFalse || p.Kind != Boolean) {
				return nil, fmt.Errorf("invalid_absence_policy: %s", r.ID)
			}
			variables[input.Variable], bound[input.PropertyID] = true, true
		}
	}
	return ancestors, nil
}

func normalize(d *Definition) {
	if d.Properties == nil {
		d.Properties = []Property{}
	}
	if d.Relations == nil {
		d.Relations = []Relation{}
	}
	if d.Rules == nil {
		d.Rules = []Rule{}
	}
	slices.SortFunc(d.Classes, func(a, b Class) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Properties, func(a, b Property) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Relations, func(a, b Relation) int { return strings.Compare(a.ID, b.ID) })
	slices.SortFunc(d.Rules, func(a, b Rule) int { return strings.Compare(a.ID, b.ID) })
	for i := range d.Classes {
		if d.Classes[i].Parents == nil {
			d.Classes[i].Parents = []string{}
		}
		slices.Sort(d.Classes[i].Parents)
	}
	for i := range d.Properties {
		if d.Properties[i].Enum == nil {
			d.Properties[i].Enum = []string{}
		}
		slices.Sort(d.Properties[i].Enum)
	}
	for i := range d.Rules {
		if d.Rules[i].Inputs == nil {
			d.Rules[i].Inputs = []Input{}
		}
		slices.SortFunc(d.Rules[i].Inputs, func(a, b Input) int { return strings.Compare(a.Variable, b.Variable) })
	}
}
