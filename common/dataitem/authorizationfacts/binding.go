package authorizationfacts

import (
	"bytes"
	"encoding/json"
	"errors"
	"math"
	"reflect"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
)

// Binding is the immutable minimal approved boundary. NativeTarget,
// FileComposition, Scope and MetadataTarget are mutually exclusive representations, not separate ACLs.
// Observation time and mutable metadata never belong to an approval binding.
type Binding struct {
	MetadataTarget  *MetadataBoundary        `json:"metadata_target,omitempty"`
	ItemFingerprint string                   `json:"item_fingerprint"`
	BoundaryHash    string                   `json:"boundary_hash"`
	NativeTarget    plugin.EngineCatalogPath `json:"native_target"`
	FileComposition *FileCompositionBoundary `json:"file_composition,omitempty"`
	Scope           *ScopeBoundary           `json:"scope,omitempty"`
}

func (b Binding) Validate() error {
	if !ValidItemFingerprint(b.ItemFingerprint) || !ValidItemFingerprint(b.BoundaryHash) {
		return errors.New("invalid DataItem authorization binding")
	}
	p := b.NativeTarget
	boundary := &AuthorizationBoundary{Layout: "single", NativeTarget: p}
	itemType, fullName, err := nativeSingleIdentity(p)
	if b.MetadataTarget != nil {
		if b.MetadataTarget.Validate() != nil || b.Scope != nil || b.FileComposition != nil || !reflect.DeepEqual(p, plugin.EngineCatalogPath{}) {
			return errors.New("ambiguous metadata boundary")
		}
		p = b.MetadataTarget.Target
		itemType, fullName, err = MetadataTargetIdentity(p)
		boundary = &AuthorizationBoundary{Layout: b.MetadataTarget.Layout, MetadataTarget: b.MetadataTarget}
	} else if b.Scope != nil {
		if b.FileComposition != nil || !reflect.DeepEqual(p, plugin.EngineCatalogPath{}) || b.Scope.Validate(b.Scope.Root.EngineID) != nil {
			return errors.New("ambiguous or invalid whole boundary")
		}
		p, itemType, fullName, err = b.Scope.Root, "file", b.Scope.Root.StringPath(), nil
		boundary = &AuthorizationBoundary{Layout: "whole", Scope: b.Scope}
	} else if b.FileComposition != nil {
		if !reflect.DeepEqual(p, plugin.EngineCatalogPath{}) {
			return errors.New("ambiguous DataItem authorization binding")
		}
		p, err = b.FileComposition.PrimaryTarget()
		if err == nil {
			itemType, fullName = p.Segments[len(p.Segments)-1].Term, p.StringPath()
			boundary = &AuthorizationBoundary{Layout: b.FileComposition.Layout(), FileComposition: b.FileComposition}
		}
	}
	if err != nil {
		return err
	}
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: p.EngineID,
		ItemFingerprint: b.ItemFingerprint, ItemType: itemType, FullName: fullName,
		Boundary: boundary}
	hash, err := f.ComputeBoundaryHash()
	if err != nil || hash != b.BoundaryHash {
		return errors.New("invalid DataItem authorization binding")
	}
	return nil
}

func (f AuthorizationFacts) ApprovedBinding() (Binding, error) {
	if f.Validate() != nil || f.BasisState != AuthorizationBasisComplete || f.Boundary.MetadataTarget != nil {
		return Binding{}, errors.New("complete DataItem boundary required")
	}
	b := Binding{ItemFingerprint: f.ItemFingerprint, BoundaryHash: *f.BoundaryHash, NativeTarget: f.Boundary.NativeTarget}
	b.NativeTarget.Segments = append([]plugin.EngineCatalogSegment(nil), b.NativeTarget.Segments...)
	b.FileComposition = f.Boundary.FileComposition.Clone()
	b.Scope = f.Boundary.Scope.Clone()
	return b, b.Validate()
}

func (b Binding) PrimaryTarget() (plugin.EngineCatalogPath, error) {
	if err := b.Validate(); err != nil {
		return plugin.EngineCatalogPath{}, err
	}
	if b.MetadataTarget != nil {
		return b.MetadataTarget.Clone().Target, nil
	}
	if b.Scope != nil {
		return b.Scope.Clone().Root, nil
	}
	if b.FileComposition != nil {
		return b.FileComposition.PrimaryTarget()
	}
	p := b.NativeTarget
	p.Segments = append([]plugin.EngineCatalogSegment(nil), p.Segments...)
	return p, nil
}

func (b Binding) MarshalJSON() ([]byte, error) {
	if err := b.Validate(); err != nil {
		return nil, err
	}
	if b.MetadataTarget != nil {
		return json.Marshal(struct {
			ItemFingerprint string            `json:"item_fingerprint"`
			BoundaryHash    string            `json:"boundary_hash"`
			MetadataTarget  *MetadataBoundary `json:"metadata_target"`
		}{b.ItemFingerprint, b.BoundaryHash, b.MetadataTarget})
	}
	if b.Scope != nil {
		return json.Marshal(struct {
			ItemFingerprint string         `json:"item_fingerprint"`
			BoundaryHash    string         `json:"boundary_hash"`
			Scope           *ScopeBoundary `json:"scope"`
		}{b.ItemFingerprint, b.BoundaryHash, b.Scope})
	}
	if b.FileComposition != nil {
		return json.Marshal(struct {
			ItemFingerprint string                   `json:"item_fingerprint"`
			BoundaryHash    string                   `json:"boundary_hash"`
			FileComposition *FileCompositionBoundary `json:"file_composition"`
		}{b.ItemFingerprint, b.BoundaryHash, b.FileComposition})
	}
	return json.Marshal(struct {
		ItemFingerprint string                   `json:"item_fingerprint"`
		BoundaryHash    string                   `json:"boundary_hash"`
		NativeTarget    plugin.EngineCatalogPath `json:"native_target"`
	}{b.ItemFingerprint, b.BoundaryHash, b.NativeTarget})
}

func (b *Binding) UnmarshalJSON(raw []byte) error {
	// The complete composition has its own 64 KiB budget. Leave room for the
	// immutable identity/hash envelope instead of truncating a valid snapshot.
	fields, err := jsonmap.DecodeObjectStrict(raw, MaxAuthorizationFactsBytes, 64)
	if err != nil || len(fields) != 3 || fields["item_fingerprint"] == nil || fields["boundary_hash"] == nil ||
		(fields["native_target"] == nil && fields["file_composition"] == nil && fields["scope"] == nil && fields["metadata_target"] == nil) {
		return errors.New("invalid approved boundary JSON")
	}
	type wire Binding
	var decoded wire
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&decoded) != nil || Binding(decoded).Validate() != nil {
		return errors.New("invalid approved boundary JSON")
	}
	*b = Binding(decoded)
	return nil
}

// PrimaryCandidateFingerprint only locates a possible Meta record. A file
// candidate proves neither its composition nor that an arbitrary ref is primary.
func PrimaryCandidateFingerprint(path plugin.EngineCatalogPath) (string, error) {
	if path.EngineID == 0 || uint64(path.EngineID) > math.MaxInt64 {
		return "", errors.New("invalid primary candidate engine")
	}
	if len(path.Segments) > 0 && (path.Segments[len(path.Segments)-1].Term == plugin.EngineCatalogTermFile || path.Segments[len(path.Segments)-1].Term == plugin.EngineCatalogTermObject) {
		if err := validateFileCompositionTarget(path); err != nil {
			return "", err
		}
		if len(path.StringPath()) > 4096 {
			return "", errors.New("invalid primary candidate path")
		}
		return models.GenerateItemFingerprint(path.EngineID, path.StringPath()), nil
	}
	if ValidWholeScopeRoot(path) {
		return models.GenerateItemFingerprint(path.EngineID, path.StringPath()), nil
	}
	_, name, err := MetadataTargetIdentity(path)
	if err != nil {
		return "", err
	}
	return models.GenerateItemFingerprint(path.EngineID, name), nil
}

func (f AuthorizationFacts) ProvePrimaryTarget(path plugin.EngineCatalogPath) (Binding, error) {
	b, err := f.ApprovedBinding()
	if err != nil {
		return Binding{}, err
	}
	primary, err := b.PrimaryTarget()
	if err != nil || !reflect.DeepEqual(primary, path) {
		return Binding{}, errors.New("primary target has no matching complete DataItem boundary")
	}
	return b, nil
}

// NativeCandidateFingerprint selects one possible Meta record only. Ownership
// is established by reading that record from Meta and comparing its complete
// typed boundary; a candidate (including a dotted-name collision) is no proof.
func NativeCandidateFingerprint(path plugin.EngineCatalogPath) (string, error) {
	binding, err := NativeCandidateBinding(path)
	return binding.ItemFingerprint, err
}

// NativeCandidateBinding describes the proposed path. Authorization must still
// verify the complete boundary independently against the current Meta record.
func NativeCandidateBinding(path plugin.EngineCatalogPath) (Binding, error) {
	itemType, fullName, err := nativeSingleIdentity(path)
	if err != nil {
		return Binding{}, err
	}
	fingerprint := models.GenerateItemFingerprint(path.EngineID, fullName)
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: path.EngineID,
		ItemFingerprint: fingerprint, ItemType: itemType, FullName: fullName,
		Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: path}}
	hash, err := f.ComputeBoundaryHash()
	if err != nil {
		return Binding{}, err
	}
	return Binding{ItemFingerprint: fingerprint, BoundaryHash: hash, NativeTarget: path}, nil
}

// ProveNativeTarget must only consume facts read from the trusted Meta owner.
// Complete identity and typed path equality are mandatory, not a name fallback.
func (f AuthorizationFacts) ProveNativeTarget(path plugin.EngineCatalogPath) (Binding, error) {
	if f.Boundary != nil && (f.Boundary.FileComposition != nil || f.Boundary.Scope != nil) {
		return Binding{}, errors.New("file composition is not a native target proof")
	}
	b, err := f.ApprovedBinding()
	if err != nil || !reflect.DeepEqual(b.NativeTarget, path) {
		return Binding{}, errors.New("native target has no matching complete DataItem boundary")
	}
	return b, nil
}
