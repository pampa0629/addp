package dataprotection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/addp/common/datatype"
)

const (
	tableSchemaSnapshotV1  = "addp.table_schema_snapshot/v1"
	tableComponentSchemaV1 = "addp.table_component_schema/v1"
)

type canonicalTableSchema struct {
	SchemaVersion string                `json:"schema_version"`
	Fields        []canonicalTableField `json:"fields"`
}

type canonicalTableField struct {
	Path        []string           `json:"path"`
	Type        datatype.FieldType `json:"type"`
	ElementType datatype.FieldType `json:"element_type,omitempty"`
	Nullable    bool               `json:"nullable"`
}

// TableSchemaSnapshotHash returns the stable structure snapshot shared by
// Security and table-data owners. Display and engine-native facts are
// deliberately excluded from the checksum payload.
func TableSchemaSnapshotHash(fields []datatype.FieldInfo) (string, error) {
	canonical, _, err := canonicalizeTableFields(fields)
	if err != nil {
		return "", err
	}
	return canonicalJSONHash(canonicalTableSchema{
		SchemaVersion: tableSchemaSnapshotV1,
		Fields:        canonical,
	})
}

// ComponentSchemaFingerprint validates a typed component path against the
// table schema and fingerprints every structural field from the record root to
// the terminal scalar.
func ComponentSchemaFingerprint(fields []datatype.FieldInfo, component Component) (string, error) {
	_, byPath, err := canonicalizeTableFields(fields)
	if err != nil {
		return "", err
	}
	if len(component.Path) == 0 {
		return "", errors.New("component path is required")
	}
	chain := make([]canonicalTableField, 0, len(component.Path))
	path := make([]string, 0, len(component.Path))
	for index, segment := range component.Path {
		name := strings.TrimSpace(segment.Name)
		if name == "" {
			return "", errors.New("component path segment name is required")
		}
		path = append(path, name)
		field, exists := byPath[pathIdentity(path)]
		if !exists {
			return "", fmt.Errorf("component field path %q is missing", strings.Join(path, "."))
		}
		switch segment.Container {
		case "object":
			if index == len(component.Path)-1 || field.Type != datatype.FieldTypeJSON {
				return "", fmt.Errorf("component field path %q is not an object", strings.Join(path, "."))
			}
		case "array":
			if index == len(component.Path)-1 || field.Type != datatype.FieldTypeArray {
				return "", fmt.Errorf("component field path %q is not an array", strings.Join(path, "."))
			}
		case "scalar":
			if index != len(component.Path)-1 || field.Type == datatype.FieldTypeJSON || field.Type == datatype.FieldTypeArray {
				return "", fmt.Errorf("component field path %q is not a terminal scalar", strings.Join(path, "."))
			}
			if string(field.Type) != strings.TrimSpace(component.ValueType) {
				return "", fmt.Errorf("component field path %q value type does not match", strings.Join(path, "."))
			}
		default:
			return "", errors.New("unsupported component path container")
		}
		chain = append(chain, field)
	}
	return canonicalJSONHash(canonicalTableSchema{
		SchemaVersion: tableComponentSchemaV1,
		Fields:        chain,
	})
}

// ValidateTableProjection validates an active projection against the current
// table structure and returns the rules for one owner action. Missing action
// rules fail closed for a managed resource.
func ValidateTableProjection(projection Projection, action string, fields []datatype.FieldInfo, now time.Time) ([]Rule, error) {
	if projection.State != ProjectionStateActive {
		return nil, errors.New("active protection projection is required")
	}
	if err := projection.Validate(now); err != nil {
		return nil, err
	}
	snapshotHash, err := TableSchemaSnapshotHash(fields)
	if err != nil {
		return nil, err
	}
	if projection.SourceSnapshotHash != snapshotHash {
		return nil, errors.New("protection projection source snapshot does not match")
	}
	action = strings.TrimSpace(action)
	rules := make([]Rule, 0, len(projection.Rules))
	for _, rule := range projection.Rules {
		if rule.Action != action {
			continue
		}
		if projection.Target.ComponentKey != "" && projection.Target.ComponentKey != rule.Component.Key {
			return nil, errors.New("protection projection target component does not match rule")
		}
		fingerprint, err := ComponentSchemaFingerprint(fields, rule.Component)
		if err != nil {
			return nil, err
		}
		if fingerprint != rule.Component.SchemaFingerprint {
			return nil, errors.New("protection rule component schema does not match")
		}
		rules = append(rules, rule)
	}
	if len(rules) == 0 {
		return nil, errors.New("protection projection has no rules for action")
	}
	return rules, nil
}

func canonicalizeTableFields(fields []datatype.FieldInfo) ([]canonicalTableField, map[string]canonicalTableField, error) {
	if len(fields) == 0 {
		return nil, nil, errors.New("table schema fields are required")
	}
	canonical := make([]canonicalTableField, 0, len(fields))
	byPath := make(map[string]canonicalTableField, len(fields))
	for _, field := range fields {
		path := append([]string(nil), field.Path...)
		if len(path) == 0 {
			path = []string{field.Name}
		}
		for index := range path {
			path[index] = strings.TrimSpace(path[index])
			if path[index] == "" {
				return nil, nil, errors.New("table field path contains an empty segment")
			}
		}
		if !datatype.IsKnownFieldType(field.Type) || field.Type == "" {
			return nil, nil, fmt.Errorf("table field path %q has an invalid type", strings.Join(path, "."))
		}
		if field.ElementType != "" && !datatype.IsKnownFieldType(field.ElementType) {
			return nil, nil, fmt.Errorf("table field path %q has an invalid element type", strings.Join(path, "."))
		}
		item := canonicalTableField{
			Path: path, Type: field.Type, ElementType: field.ElementType, Nullable: field.Nullable,
		}
		key := pathIdentity(path)
		if _, exists := byPath[key]; exists {
			return nil, nil, fmt.Errorf("duplicate table field path %q", strings.Join(path, "."))
		}
		byPath[key] = item
		canonical = append(canonical, item)
	}
	sort.Slice(canonical, func(i, j int) bool {
		return pathIdentity(canonical[i].Path) < pathIdentity(canonical[j].Path)
	})
	return canonical, byPath, nil
}

func pathIdentity(path []string) string {
	encoded, _ := json.Marshal(path)
	return string(encoded)
}

func canonicalJSONHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("marshal protection schema checksum payload: %w", err)
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}
