package authorizationfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
)

// FileCompositionRef describes an exact constituent of one logical DataItem.
// The target is a catalog leaf, not a prefix grant or an independent ACL key.
type FileCompositionRef struct {
	Target   plugin.EngineCatalogPath `json:"target"`
	Role     string                   `json:"role"`
	Required bool                     `json:"required"`
	Primary  bool                     `json:"primary"`
}

// FileCompositionBoundary carries complete, Meta-confirmed model composition.
// It has no content versions, byte counts, credentials or permission decisions.
type FileCompositionBoundary struct {
	Format string               `json:"format"`
	Refs   []FileCompositionRef `json:"refs"`
}

const MaxFileCompositionRefs = 200

var errFileComposition = errors.New("invalid exact file composition boundary")

// NewFileCompositionBoundary canonicalizes facts supplied by the owner. It
// neither discovers dependencies nor proves that their discovery is complete.
func NewFileCompositionBoundary(format string, refs []FileCompositionRef) (*FileCompositionBoundary, error) {
	if len(refs) == 0 || len(refs) > MaxFileCompositionRefs {
		return nil, errFileComposition
	}
	result := (&FileCompositionBoundary{Format: format, Refs: refs}).Clone()
	sort.Slice(result.Refs, func(i, j int) bool { return fileRefKey(result.Refs[i]) < fileRefKey(result.Refs[j]) })
	unique := result.Refs[:0]
	for _, ref := range result.Refs {
		if len(unique) > 0 && reflect.DeepEqual(unique[len(unique)-1].Target, ref.Target) {
			if !reflect.DeepEqual(unique[len(unique)-1], ref) {
				return nil, errFileComposition
			}
			continue
		}
		unique = append(unique, ref)
	}
	result.Refs = unique
	if err := result.Validate(result.Refs[0].Target.EngineID); err != nil {
		return nil, err
	}
	return result, nil
}

// Validate requires the wire set to be sorted, unique and already canonical.
// All references are exact leaves beneath the primary's typed parent; that
// parent itself never becomes an authorized input.
func (b *FileCompositionBoundary) Validate(engineID uint) error {
	if b == nil || engineID == 0 || uint64(engineID) > math.MaxInt64 || !fileCompositionToken(b.Format) ||
		len(b.Refs) == 0 || len(b.Refs) > MaxFileCompositionRefs {
		return errFileComposition
	}
	primaryIndex := -1
	previous := ""
	for i, ref := range b.Refs {
		if ref.Target.EngineID != engineID || !fileCompositionToken(ref.Role) || validateFileCompositionTarget(ref.Target) != nil {
			return errFileComposition
		}
		key := fileRefKey(ref)
		if i > 0 && previous >= key {
			return errFileComposition
		}
		previous = key
		if ref.Primary {
			if primaryIndex >= 0 || !ref.Required {
				return errFileComposition
			}
			primaryIndex = i
		}
	}
	if primaryIndex < 0 {
		return errFileComposition
	}
	encoded, err := json.Marshal(b)
	if err != nil || len(encoded) > 64<<10 {
		return errFileComposition
	}
	primary := b.Refs[primaryIndex].Target
	parent := primary.Segments[:len(primary.Segments)-1]
	leaf := primary.Segments[len(primary.Segments)-1]
	for _, ref := range b.Refs {
		target := ref.Target
		if len(target.Segments) <= len(parent) || target.Segments[len(target.Segments)-1].Term != leaf.Term ||
			!reflect.DeepEqual(target.Segments[:len(parent)], parent) {
			return errFileComposition
		}
	}
	return nil
}

func (b *FileCompositionBoundary) Clone() *FileCompositionBoundary {
	if b == nil {
		return nil
	}
	clone := &FileCompositionBoundary{Format: b.Format, Refs: make([]FileCompositionRef, len(b.Refs))}
	for i, ref := range b.Refs {
		clone.Refs[i] = ref
		clone.Refs[i].Target.Segments = append([]plugin.EngineCatalogSegment(nil), ref.Target.Segments...)
	}
	return clone
}

func (b *FileCompositionBoundary) PrimaryTarget() (plugin.EngineCatalogPath, error) {
	if b == nil || len(b.Refs) == 0 || b.Validate(b.Refs[0].Target.EngineID) != nil {
		return plugin.EngineCatalogPath{}, errFileComposition
	}
	for _, ref := range b.Clone().Refs {
		if ref.Primary {
			return ref.Target, nil
		}
	}
	return plugin.EngineCatalogPath{}, errFileComposition
}

func (b *FileCompositionBoundary) Targets() ([]plugin.EngineCatalogPath, error) {
	if b == nil || len(b.Refs) == 0 || b.Validate(b.Refs[0].Target.EngineID) != nil {
		return nil, errFileComposition
	}
	result := make([]plugin.EngineCatalogPath, len(b.Refs))
	for i, ref := range b.Clone().Refs {
		result[i] = ref.Target
	}
	return result, nil
}

func fileRefKey(ref FileCompositionRef) string {
	encoded, _ := json.Marshal(ref.Target)
	return string(encoded)
}

func fileCompositionToken(value string) bool {
	if len(value) == 0 || len(value) > 64 {
		return false
	}
	for _, char := range value {
		if !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9' || char == '_') {
			return false
		}
	}
	return true
}

func validateFileCompositionTarget(target plugin.EngineCatalogPath) error {
	if target.Version != plugin.EngineCatalogPathVersion || len(target.Segments) < 2 || len(target.Segments) > 64 {
		return errFileComposition
	}
	for _, part := range target.Segments[1:] {
		if part.Name == "" || part.Name == "." || part.Name == ".." || !utf8.ValidString(part.Name) ||
			len(part.Name) > 4096 || strings.ContainsAny(part.Name, "/\\") || strings.ContainsFunc(part.Name, unicode.IsControl) {
			return errFileComposition
		}
	}
	leaf := target.Segments[len(target.Segments)-1]
	var expected plugin.EngineCatalogPath
	switch leaf.Term {
	case plugin.EngineCatalogTermFile:
		expected = plugin.FileItemPath(target.EngineID, strings.Repeat("segment/", len(target.Segments)-2)+"leaf")
	case plugin.EngineCatalogTermObject:
		if len(target.Segments) < 3 {
			return errFileComposition
		}
		expected = plugin.ObjectItemPath(target.EngineID, "bucket", strings.Repeat("segment/", len(target.Segments)-3)+"leaf")
	default:
		return errFileComposition
	}
	// Reuse the model's typed shape. Names above are already validated as
	// exact segments and do not need external path spelling normalization.
	for i := 1; i < len(expected.Segments); i++ {
		expected.Segments[i].Name = target.Segments[i].Name
	}
	if !reflect.DeepEqual(target, expected) || len(target.StringPath()) > 4096 {
		return errFileComposition
	}
	return nil
}

// UnmarshalJSON preserves explicit false and rejects omitted or null flags.
func (r *FileCompositionRef) UnmarshalJSON(encoded []byte) error {
	strict := json.NewDecoder(bytes.NewReader(encoded))
	strict.UseNumber()
	if _, err := authorizationJSONValue(strict, 0); err != nil {
		return errFileComposition
	}
	if _, err := strict.Token(); err != io.EOF {
		return errFileComposition
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(encoded, &fields) != nil || len(fields) != 4 {
		return errFileComposition
	}
	for _, key := range []string{"target", "role", "required", "primary"} {
		value, ok := fields[key]
		if !ok || bytes.Equal(bytes.TrimSpace(value), []byte("null")) {
			return errFileComposition
		}
	}
	type wire FileCompositionRef
	var result wire
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil {
		return errFileComposition
	}
	*r = FileCompositionRef(result)
	return nil
}

func (b *FileCompositionBoundary) UnmarshalJSON(encoded []byte) error {
	if len(encoded) > 64<<10 {
		return errFileComposition
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	keys, err := authorizationJSONValue(decoder, 0)
	if err != nil || len(keys) != 2 || !keys["format"] || !keys["refs"] {
		return errFileComposition
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errFileComposition
	}
	type wire FileCompositionBoundary
	var result wire
	decoder = json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || len(result.Refs) == 0 ||
		(*FileCompositionBoundary)(&result).Validate(result.Refs[0].Target.EngineID) != nil {
		return errFileComposition
	}
	*b = FileCompositionBoundary(result)
	return nil
}

// MarshalJSON expresses the two single boundary variants without a zero-value
// native path beside file refs. Native canonical bytes remain unchanged.
func (b AuthorizationBoundary) MarshalJSON() ([]byte, error) {
	if b.FileComposition != nil {
		if !reflect.DeepEqual(b.NativeTarget, plugin.EngineCatalogPath{}) {
			return nil, errFileComposition
		}
		return json.Marshal(struct {
			Layout          string                   `json:"layout"`
			FileComposition *FileCompositionBoundary `json:"file_composition"`
		}{b.Layout, b.FileComposition})
	}
	return json.Marshal(struct {
		Layout       string                   `json:"layout"`
		NativeTarget plugin.EngineCatalogPath `json:"native_target"`
	}{b.Layout, b.NativeTarget})
}

func (b *AuthorizationBoundary) UnmarshalJSON(encoded []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	keys, err := authorizationJSONValue(decoder, 0)
	if err != nil || !keys["layout"] || len(keys) != 2 || keys["native_target"] == keys["file_composition"] {
		return errFileComposition
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errFileComposition
	}
	type wire AuthorizationBoundary
	var result wire
	decoder = json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result.Layout != "single" ||
		(keys["file_composition"] && result.FileComposition == nil) {
		return errFileComposition
	}
	if keys["native_target"] {
		if _, _, err := nativeSingleIdentity(result.NativeTarget); err != nil {
			return errFileComposition
		}
	}
	*b = AuthorizationBoundary(result)
	return nil
}
