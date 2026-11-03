package authorizationfacts

import (
	"encoding/json"
	"errors"
	"io"
	"reflect"
	"strings"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
)

// MetadataBoundary describes a logical item only. It is never a content plan,
// composition completeness proof or execution authorization.
type MetadataBoundary struct {
	Target            plugin.EngineCatalogPath `json:"target"`
	Layout            string                   `json:"layout"`
	DataType          string                   `json:"data_type"`
	Format            string                   `json:"format"`
	CompositionDigest string                   `json:"composition_digest"`
}

// MetadataTargetIdentity validates typed coordinates without consulting a
// registry, engine connection or display-name parser. The owner must also
// prove the actual parent chain and the engine's declared catalog model.
func MetadataTargetIdentity(p plugin.EngineCatalogPath) (string, string, error) {
	invalid := errors.New("invalid metadata item target")
	if p.Version != plugin.EngineCatalogPathVersion || p.EngineID == 0 || len(p.Segments) < 2 || len(p.Segments) > 64 || !plugin.IsEngineCatalogRootSegment(p.Segments[0]) {
		return "", "", invalid
	}
	last := p.Segments[len(p.Segments)-1]
	if ValidMetadataScopeRoot(p) {
		kind := "file"
		if last.Term != "directory" {
			kind = "object"
		}
		return kind, p.StringPath(), nil
	}
	if last.Term != last.Kind && !(last.Term == "table" && (last.Kind == "view" || last.Kind == "materialized_view" || last.Kind == "external_table")) {
		return "", "", invalid
	}
	switch last.Kind {
	case "table", "view", "materialized_view", "external_table", "collection", "graph", "index", "keyspace", "topic", "file", "object":
	default:
		return "", "", invalid
	}
	names := make([]string, 0, len(p.Segments)-1)
	for i, segment := range p.Segments[1:] {
		if segment.Name == "" || len(segment.Name) > 4096 || !utf8.ValidString(segment.Name) || strings.ContainsRune(segment.Name, 0) {
			return "", "", invalid
		}
		if i < len(p.Segments)-2 {
			switch segment.Term {
			case "catalog", "database", "schema":
				if segment.Kind != "namespace" {
					return "", "", invalid
				}
			case "directory", "bucket", "prefix":
				if segment.Kind != segment.Term {
					return "", "", invalid
				}
			default:
				return "", "", invalid
			}
		}
		names = append(names, segment.Name)
	}
	separator := "."
	if last.Kind == "file" || last.Kind == "object" {
		separator = "/"
	}
	name := strings.Join(names, separator)
	if len(name) > 4096 {
		return "", "", invalid
	}
	if last.Term == "view" || last.Term == "materialized_view" || last.Term == "external_table" {
		return "", "", invalid
	}
	return last.Term, name, nil
}

func (m *MetadataBoundary) Validate() error {
	if m == nil {
		return errors.New("metadata boundary required")
	}
	kind, _, err := MetadataTargetIdentity(m.Target)
	if err != nil {
		return err
	}
	if (m.Layout == "whole") != ValidMetadataScopeRoot(m.Target) {
		return errors.New("whole metadata identity requires its exact scope root")
	}
	if (m.Layout != "single" && m.Layout != "multi" && m.Layout != "whole") || ((kind != "file" && kind != "object") && m.Layout != "single") || !ValidItemFingerprint(m.CompositionDigest) {
		return errors.New("invalid metadata boundary")
	}
	for _, v := range []string{m.DataType, m.Format} {
		if len(v) > 64 || !utf8.ValidString(v) || strings.ContainsRune(v, 0) {
			return errors.New("invalid metadata identity token")
		}
	}
	return nil
}

func (m *MetadataBoundary) Clone() *MetadataBoundary {
	if m == nil {
		return nil
	}
	result := *m
	result.Target.Segments = append([]plugin.EngineCatalogSegment(nil), m.Target.Segments...)
	return &result
}

func (m *MetadataBoundary) UnmarshalJSON(raw []byte) error {
	fields, err := strictMetadataObject(raw)
	if err != nil || len(fields) != 5 {
		return errors.New("invalid metadata boundary JSON")
	}
	var values map[string]json.RawMessage
	if json.Unmarshal(raw, &values) != nil {
		return errors.New("invalid metadata boundary JSON")
	}
	for _, key := range []string{"target", "layout", "data_type", "format", "composition_digest"} {
		value, ok := values[key]
		if !ok || string(value) == "null" {
			return errors.New("invalid metadata boundary JSON")
		}
	}
	var path struct {
		Segments []map[string]json.RawMessage `json:"segments"`
	}
	if json.Unmarshal(values["target"], &path) != nil {
		return errors.New("invalid metadata target JSON")
	}
	for _, segment := range path.Segments {
		if len(segment) != 3 {
			return errors.New("invalid metadata segment JSON")
		}
		for _, key := range []string{"term", "kind", "name"} {
			value, ok := segment[key]
			if !ok || string(value) == "null" {
				return errors.New("invalid metadata segment JSON")
			}
		}
	}
	type wire MetadataBoundary
	var result wire
	decoder := json.NewDecoder(strings.NewReader(string(raw)))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || (*MetadataBoundary)(&result).Validate() != nil {
		return errors.New("invalid metadata boundary JSON")
	}
	*m = MetadataBoundary(result)
	return nil
}

func strictMetadataObject(raw []byte) (map[string]bool, error) {
	if len(raw) > MaxAuthorizationFactsBytes {
		return nil, errors.New("metadata boundary too large")
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	fields, err := authorizationJSONValue(d, 0)
	if err != nil {
		return nil, err
	}
	if _, err := d.Token(); err != io.EOF {
		return nil, errors.New("trailing metadata JSON")
	}
	return fields, nil
}

// MetadataBinding shares the existing immutable boundary identity, including
// native bytes for established PG/content profiles; it adds no second policy.
func (f AuthorizationFacts) MetadataBinding() (Binding, error) {
	if f.Validate() != nil || f.BasisState != AuthorizationBasisComplete {
		return Binding{}, errors.New("complete metadata identity required")
	}
	if f.Boundary.MetadataTarget == nil {
		return f.ApprovedBinding()
	}
	b := Binding{ItemFingerprint: f.ItemFingerprint, BoundaryHash: *f.BoundaryHash, MetadataTarget: f.Boundary.MetadataTarget.Clone()}
	return b, b.Validate()
}

func (f AuthorizationFacts) ProveMetadataTarget(path plugin.EngineCatalogPath) (Binding, error) {
	b, err := f.MetadataBinding()
	if err != nil {
		return Binding{}, err
	}
	primary, err := b.PrimaryTarget()
	if err != nil || !reflect.DeepEqual(path, primary) {
		return Binding{}, errors.New("metadata target mismatch")
	}
	return b, nil
}

// A metadata scope root identifies one already-recognized whole DataItem. It
// supplies no node grant, content completeness or permission to enumerate.
func ValidMetadataScopeRoot(root plugin.EngineCatalogPath) bool {
	if len(root.Segments) < 2 {
		return false
	}
	last := root.Segments[len(root.Segments)-1]
	if last.Term != last.Kind {
		return false
	}
	leaf := root
	leaf.Segments = append([]plugin.EngineCatalogSegment(nil), root.Segments...)
	switch last.Term {
	case "directory":
		leaf.Segments[len(leaf.Segments)-1].Term = "file"
		leaf.Segments[len(leaf.Segments)-1].Kind = "file"
	case "prefix":
		leaf.Segments[len(leaf.Segments)-1].Term = "object"
		leaf.Segments[len(leaf.Segments)-1].Kind = "object"
	case "bucket":
		leaf.Segments = append(leaf.Segments, plugin.EngineCatalogSegment{Term: "object", Kind: "object", Name: "_"})
	default:
		return false
	}
	return validateFileCompositionTarget(leaf) == nil
}
