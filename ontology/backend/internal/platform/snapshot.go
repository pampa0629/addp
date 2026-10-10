package platform

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"math"
	"reflect"
	"regexp"
	"strings"
	"unicode/utf8"
)

const (
	ContractVersion = "addp.platform-definition/v2"
	CompilerVersion = "addp.platform-compiler/v2"
	maxSourceBytes  = 64 << 10
	maxContextBytes = 32 << 10
	maxJSONDepth    = 32
)

var (
	identifier = regexp.MustCompile(`^[a-z][a-z0-9_]{0,63}$`)
	qualified  = regexp.MustCompile(`^[a-z][a-z0-9_]*(\.[a-z][a-z0-9_]*)+$`)
	skillName  = regexp.MustCompile(`^[a-z][a-z0-9-]{0,63}$`)
)

type envelope struct {
	Contract   string  `json:"contract"`
	Compiler   string  `json:"compiler"`
	Definition Context `json:"definition"`
	Review     Review  `json:"review"`
}

// Snapshot binds a static definition to the compiler contract. It contains no
// Tenant, publication, graph readiness or authorization facts.
type Snapshot struct {
	canonical string
	digest    string
}

// Compile validates a single source definition and freezes the release bytes.
// It does not check live owner capabilities or publish anything.
func Compile(data, reviewData []byte) (*Snapshot, error) {
	var definition Context
	if err := decodeStrict(data, &definition); err != nil {
		return nil, err
	}
	var review Review
	if err := decodeStrict(reviewData, &review); err != nil {
		return nil, err
	}
	return freeze(definition, review)
}

func freeze(definition Context, review Review) (*Snapshot, error) {
	if err := validate(definition); err != nil {
		return nil, err
	}
	if err := validateReview(definition, review); err != nil {
		return nil, err
	}
	canonical, err := json.Marshal(envelope{ContractVersion, CompilerVersion, definition, review})
	if err != nil {
		return nil, err
	}
	if len(canonical) > maxSourceBytes {
		return nil, fmt.Errorf("platform_snapshot_size_limit")
	}
	hash := sha256.Sum256(canonical)
	digest := hex.EncodeToString(hash[:])
	definition.Digest = digest
	response, err := json.Marshal(definition)
	if err != nil {
		return nil, err
	}
	if len(response) > maxContextBytes {
		return nil, fmt.Errorf("platform_context_size_limit")
	}
	return &Snapshot{canonical: string(canonical), digest: digest}, nil
}

// Restore accepts only current, byte-exact canonical releases. It never
// normalizes stored history, upgrades the compiler or falls back to source JSON.
func Restore(data []byte, digest string) (*Snapshot, error) {
	var stored envelope
	if err := decodeStrict(data, &stored); err != nil {
		return nil, err
	}
	if stored.Contract != ContractVersion || stored.Compiler != CompilerVersion {
		return nil, fmt.Errorf("unsupported_platform_snapshot_version")
	}
	snapshot, err := freeze(stored.Definition, stored.Review)
	if err != nil {
		return nil, err
	}
	if snapshot.digest != digest || snapshot.canonical != string(data) {
		return nil, fmt.Errorf("platform_snapshot_integrity_mismatch")
	}
	return snapshot, nil
}

func (s *Snapshot) Digest() string { return s.digest }

// CanonicalJSON returns independent bytes suitable for immutable owner storage.
func (s *Snapshot) CanonicalJSON() []byte { return []byte(s.canonical) }

// Context returns an independent API value; mutations cannot affect the release.
func (s *Snapshot) Context() (Context, error) {
	if s == nil || s.canonical == "" {
		return Context{}, fmt.Errorf("invalid_platform_snapshot")
	}
	var stored envelope
	if err := json.Unmarshal([]byte(s.canonical), &stored); err != nil {
		return Context{}, err
	}
	stored.Definition.Digest = s.digest
	return stored.Definition, nil
}

func validate(d Context) error {
	if d.SchemaVersion != "addp.platform-capability-context/v1" || d.KnowledgeKind != "platform_definition" ||
		d.Availability != "not_observed" || d.Digest != "" || ValidateIdentity(d.Capability, d.Revision) != nil {
		return fmt.Errorf("invalid_platform_definition_identity")
	}
	if len(d.Concepts) == 0 || len(d.Concepts) > 32 || d.Relations == nil || len(d.Relations) > 64 ||
		d.Requirements == nil || len(d.Requirements) > 16 {
		return fmt.Errorf("platform_member_limit")
	}
	concepts := make(map[string]bool, len(d.Concepts))
	for _, concept := range d.Concepts {
		if !identifier.MatchString(concept.ID) || concepts[concept.ID] ||
			!validText(concept.Name["en"]) || !validText(concept.Name["zh-cn"]) || len(concept.Name) > 16 {
			return fmt.Errorf("invalid_platform_concept")
		}
		for language, name := range concept.Name {
			if !validText(language) || !validText(name) {
				return fmt.Errorf("invalid_platform_concept_name")
			}
		}
		concepts[concept.ID] = true
	}
	relations := make(map[Relation]bool, len(d.Relations))
	for _, relation := range d.Relations {
		if !concepts[relation.From] || !concepts[relation.To] || !identifier.MatchString(relation.Kind) || relations[relation] {
			return fmt.Errorf("invalid_platform_relation")
		}
		relations[relation] = true
	}
	operation := d.Operation
	if !identifier.MatchString(operation.Owner) || !skillName.MatchString(operation.Skill) || operation.Tool != d.Capability ||
		!uniqueIDs(operation.InputsRequired, identifier.MatchString) || !uniqueIDs(operation.Effects, qualifiedID) ||
		!uniqueIDs(operation.ExcludedEffects, qualifiedID) {
		return fmt.Errorf("invalid_platform_operation")
	}
	effects := make(map[string]bool, len(operation.Effects))
	for _, effect := range operation.Effects {
		effects[effect] = true
	}
	for _, excluded := range operation.ExcludedEffects {
		if effects[excluded] {
			return fmt.Errorf("conflicting_platform_effect")
		}
	}
	requirements := make(map[string]bool, len(d.Requirements))
	for _, requirement := range d.Requirements {
		if !identifier.MatchString(requirement.ID) || requirements[requirement.ID] || !uniqueIDs(requirement.Tools, qualifiedID) {
			return fmt.Errorf("invalid_platform_requirement")
		}
		requirements[requirement.ID] = true
	}
	return nil
}

// ValidateIdentity is shared by compilation and exact stored-revision lookup.
func ValidateIdentity(capability string, revision uint64) error {
	if !qualifiedID(capability) || revision == 0 || revision > math.MaxInt64 {
		return fmt.Errorf("invalid_platform_definition_identity")
	}
	return nil
}

func qualifiedID(value string) bool { return len(value) <= 128 && qualified.MatchString(value) }

func validText(value string) bool {
	return utf8.ValidString(value) && strings.TrimSpace(value) != "" && len(value) <= 512
}

func uniqueIDs(values []string, valid func(string) bool) bool {
	if len(values) == 0 || len(values) > 32 {
		return false
	}
	seen := make(map[string]bool, len(values))
	for _, value := range values {
		if !valid(value) || seen[value] {
			return false
		}
		seen[value] = true
	}
	return true
}

// HTTP body binding requires Gin and does not check duplicate keys; source and
// stored release JSON instead share this owner-local, bounded strict decoder.
func decodeStrict(data []byte, target any) error {
	if len(data) == 0 || len(data) > maxSourceBytes || !utf8.Valid(data) {
		return fmt.Errorf("invalid_platform_json_size_or_encoding")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := checkJSONValue(decoder, 0, reflect.TypeOf(target).Elem()); err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return fmt.Errorf("invalid_platform_json_trailer")
	}
	decoder = json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return fmt.Errorf("invalid_platform_json: %w", err)
	}
	return nil
}

func checkJSONValue(decoder *json.Decoder, depth int, expected reflect.Type) error {
	if depth > maxJSONDepth {
		return fmt.Errorf("platform_json_depth_limit")
	}
	token, err := decoder.Token()
	if err != nil {
		return fmt.Errorf("invalid_platform_json: %w", err)
	}
	if token == nil {
		return fmt.Errorf("null_platform_json_value")
	}
	delimiter, nested := token.(json.Delim)
	if !nested {
		return nil
	}
	switch delimiter {
	case '{':
		if expected.Kind() != reflect.Struct && expected.Kind() != reflect.Map {
			return fmt.Errorf("invalid_platform_json_object")
		}
		fields := map[string]reflect.Type{}
		if expected.Kind() == reflect.Struct {
			for i := 0; i < expected.NumField(); i++ {
				field := expected.Field(i)
				fields[field.Tag.Get("json")] = field.Type
			}
		}
		seen := map[string]bool{}
		for decoder.More() {
			key, err := decoder.Token()
			if err != nil {
				return err
			}
			name, ok := key.(string)
			if !ok || seen[name] {
				return fmt.Errorf("duplicate_or_invalid_platform_json_key")
			}
			seen[name] = true
			var valueType reflect.Type
			if expected.Kind() == reflect.Map {
				valueType = expected.Elem()
			} else {
				valueType = fields[name]
				if valueType == nil {
					return fmt.Errorf("unknown_platform_json_field")
				}
			}
			if err := checkJSONValue(decoder, depth+1, valueType); err != nil {
				return err
			}
		}
		if expected.Kind() == reflect.Struct && len(seen) != len(fields) {
			return fmt.Errorf("missing_platform_json_field")
		}
	case '[':
		if expected.Kind() != reflect.Slice {
			return fmt.Errorf("invalid_platform_json_array")
		}
		for decoder.More() {
			if err := checkJSONValue(decoder, depth+1, expected.Elem()); err != nil {
				return err
			}
		}
	default:
		return fmt.Errorf("invalid_platform_json_delimiter")
	}
	_, err = decoder.Token()
	return err
}
