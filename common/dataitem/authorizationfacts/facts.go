package authorizationfacts

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math"
	"reflect"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

const AuthorizationFactsSchemaV1 = "meta.data_item_authorization_facts/v1"

type AuthorizationBasisState string

const (
	AuthorizationBasisComplete     AuthorizationBasisState = "complete"
	AuthorizationBasisIncomplete   AuthorizationBasisState = "incomplete"
	AuthorizationBasisNeedsRefresh AuthorizationBasisState = "needs_refresh"
	AuthorizationBasisUnsupported  AuthorizationBasisState = "unsupported"
)

// AuthorizationBoundary is one exact single-item representation. NativeTarget
// and FileComposition are mutually exclusive; neither is a permission decision.
type AuthorizationBoundary struct {
	Layout          string                   `json:"layout" enums:"single"`
	NativeTarget    plugin.EngineCatalogPath `json:"native_target"`
	FileComposition *FileCompositionBoundary `json:"file_composition,omitempty"`
}

// AuthorizationFacts contains only Meta's current resource facts, never an Allow.
type AuthorizationFacts struct {
	SchemaVersion   string                  `json:"schema_version"`
	ItemFingerprint string                  `json:"item_fingerprint"`
	EngineID        uint                    `json:"engine_id,string" swaggertype:"string"`
	ItemType        string                  `json:"item_type"`
	FullName        string                  `json:"full_name"`
	Boundary        *AuthorizationBoundary  `json:"boundary"`
	BasisState      AuthorizationBasisState `json:"basis_state" enums:"complete,incomplete,needs_refresh,unsupported"`
	BoundaryHash    *string                 `json:"boundary_hash"`
	ObservedAt      time.Time               `json:"observed_at"`
}

func ValidItemFingerprint(value string) bool {
	if len(value) != sha256.Size*2 || strings.ToLower(value) != value {
		return false
	}
	_, err := hex.DecodeString(value)
	return err == nil
}

func (f AuthorizationFacts) validateIdentity() error {
	if f.SchemaVersion != AuthorizationFactsSchemaV1 || f.EngineID == 0 || uint64(f.EngineID) > math.MaxInt64 ||
		!ValidItemFingerprint(f.ItemFingerprint) || f.FullName == "" || len(f.FullName) > 4096 || !utf8.ValidString(f.FullName) ||
		strings.ContainsRune(f.FullName, 0) || f.ItemType == "" || len(f.ItemType) > 64 ||
		f.ItemFingerprint != models.GenerateItemFingerprint(f.EngineID, f.FullName) {
		return errors.New("invalid data item authorization identity")
	}
	return nil
}

func (f AuthorizationFacts) Validate() error {
	if err := f.validateIdentity(); err != nil {
		return err
	}
	if f.ObservedAt.IsZero() {
		return errors.New("authorization facts observation is required")
	}
	switch f.BasisState {
	case AuthorizationBasisComplete:
		hash, err := f.ComputeBoundaryHash()
		if err != nil {
			return err
		}
		if f.BoundaryHash == nil || *f.BoundaryHash != hash {
			return errors.New("authorization boundary hash mismatch")
		}
	case AuthorizationBasisIncomplete, AuthorizationBasisNeedsRefresh, AuthorizationBasisUnsupported:
		if f.Boundary != nil || f.BoundaryHash != nil {
			return errors.New("unresolved authorization facts must not declare a boundary")
		}
	default:
		return errors.New("unknown authorization basis state")
	}
	return nil
}

// UnmarshalJSON enforces this small wire schema on every consumer.
func (f *AuthorizationFacts) UnmarshalJSON(encoded []byte) error {
	if len(encoded) > 64<<10 || !utf8.Valid(encoded) {
		return errors.New("invalid authorization facts JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	keys, err := authorizationJSONValue(decoder, 0)
	if err != nil {
		return err
	}
	if _, err := decoder.Token(); err != io.EOF {
		return errors.New("trailing authorization facts JSON")
	}
	for _, key := range []string{"schema_version", "item_fingerprint", "engine_id", "item_type", "full_name", "boundary", "basis_state", "boundary_hash", "observed_at"} {
		if !keys[key] {
			return errors.New("authorization facts field is missing")
		}
	}
	type wire AuthorizationFacts
	var result wire
	decoder = json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result); err != nil {
		return errors.New("invalid authorization facts JSON")
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil || string(fields["engine_id"]) != strconv.Quote(strconv.FormatUint(uint64(result.EngineID), 10)) {
		return errors.New("noncanonical authorization engine ID")
	}
	if err := AuthorizationFacts(result).Validate(); err != nil {
		return err
	}
	*f = AuthorizationFacts(result)
	return nil
}

func authorizationJSONValue(decoder *json.Decoder, depth int) (map[string]bool, error) {
	invalid := errors.New("invalid or duplicate authorization facts JSON field")
	if depth > 8 {
		return nil, invalid
	}
	token, err := decoder.Token()
	if err != nil {
		return nil, invalid
	}
	delim, compound := token.(json.Delim)
	if !compound {
		return nil, nil
	}
	keys := map[string]bool{}
	if delim != '{' && delim != '[' {
		return nil, invalid
	}
	for decoder.More() {
		if delim == '{' {
			key, err := decoder.Token()
			name, ok := key.(string)
			if err != nil || !ok || name == "" || keys[name] {
				return nil, invalid
			}
			for _, ch := range name {
				if ch != '_' && (ch < 'a' || ch > 'z') {
					return nil, invalid
				}
			}
			keys[name] = true
		}
		if _, err := authorizationJSONValue(decoder, depth+1); err != nil {
			return nil, err
		}
	}
	end, err := decoder.Token()
	if err != nil || (delim == '{' && end != json.Delim('}')) || (delim == '[' && end != json.Delim(']')) {
		return nil, invalid
	}
	return keys, nil
}

// ComputeBoundaryHash excludes observation time, scan depth and all content facts.
func (f AuthorizationFacts) ComputeBoundaryHash() (string, error) {
	if err := f.validateIdentity(); err != nil {
		return "", err
	}
	if f.Boundary == nil || f.Boundary.Layout != "single" {
		return "", errors.New("unsupported authorization boundary")
	}
	if f.Boundary.FileComposition != nil {
		if !reflect.DeepEqual(f.Boundary.NativeTarget, plugin.EngineCatalogPath{}) || f.Boundary.FileComposition.Validate(f.EngineID) != nil {
			return "", errFileComposition
		}
		primary, err := f.Boundary.FileComposition.PrimaryTarget()
		if err != nil || f.ItemType != primary.Segments[len(primary.Segments)-1].Term || f.FullName != primary.StringPath() {
			return "", errors.New("file composition does not match data item")
		}
	} else {
		p := f.Boundary.NativeTarget
		itemType, fullName, err := nativeSingleIdentity(p)
		if err != nil || p.EngineID != f.EngineID || f.ItemType != itemType || f.FullName != fullName {
			return "", errors.New("native boundary does not match data item")
		}
	}
	canonical := struct {
		SchemaVersion   string                 `json:"schema_version"`
		ItemFingerprint string                 `json:"item_fingerprint"`
		EngineID        uint                   `json:"engine_id,string"`
		ItemType        string                 `json:"item_type"`
		FullName        string                 `json:"full_name"`
		Boundary        *AuthorizationBoundary `json:"boundary"`
	}{f.SchemaVersion, f.ItemFingerprint, f.EngineID, f.ItemType, f.FullName, f.Boundary}
	encoded, err := json.Marshal(canonical)
	if err != nil {
		return "", err
	}
	sum := sha256.Sum256(encoded)
	return hex.EncodeToString(sum[:]), nil
}

// nativeSingleIdentity validates exact typed coordinates. A dotted full name is
// an identity input, never a substitute for comparing the complete native path.
func nativeSingleIdentity(p plugin.EngineCatalogPath) (string, string, error) {
	invalid := errors.New("unsupported native authorization target")
	if p.EngineID == 0 || len(p.Segments) != 3 || p.Segments[1].Name == "" || p.Segments[2].Name == "" {
		return "", "", invalid
	}
	namespace, leaf := p.Segments[1], p.Segments[2]
	var expected plugin.EngineCatalogPath
	switch {
	case leaf.Term == plugin.EngineCatalogTermTable && (namespace.Term == plugin.EngineCatalogTermSchema || namespace.Term == plugin.EngineCatalogTermDatabase):
		expected = plugin.TabularItemPath(p.EngineID, namespace.Term, namespace.Name, leaf.Name)
	case leaf.Term == plugin.EngineCatalogTermCollection && namespace.Term == plugin.EngineCatalogTermDatabase:
		expected = plugin.EngineCatalogBranchLeafPath(plugin.DynamicSchemaCatalogModel(), p.EngineID, namespace.Term, namespace.Name, leaf.Term, plugin.EngineCatalogKindCollection, leaf.Name)
	default:
		return "", "", invalid
	}
	if !reflect.DeepEqual(p, expected) {
		return "", "", invalid
	}
	for _, segment := range p.Segments {
		if !utf8.ValidString(segment.Name) || strings.ContainsRune(segment.Name, 0) {
			return "", "", invalid
		}
	}
	return leaf.Term, namespace.Name + "." + leaf.Name, nil
}
