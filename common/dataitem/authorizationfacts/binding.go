package authorizationfacts

import (
	"errors"
	"reflect"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/models"
)

// Binding is the immutable, minimal approved boundary for the supported native
// single-table or single-collection slice. NativeTarget locates the actual read; it is not an ACL key.
// Observation time and mutable metadata never belong to an approval binding.
type Binding struct {
	ItemFingerprint string                   `json:"item_fingerprint"`
	BoundaryHash    string                   `json:"boundary_hash"`
	NativeTarget    plugin.EngineCatalogPath `json:"native_target"`
}

func (b Binding) Validate() error {
	p := b.NativeTarget
	if len(p.Segments) != 3 || !ValidItemFingerprint(b.ItemFingerprint) || !ValidItemFingerprint(b.BoundaryHash) {
		return errors.New("invalid DataItem authorization binding")
	}
	itemType, fullName, err := nativeSingleIdentity(p)
	if err != nil {
		return err
	}
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: p.EngineID,
		ItemFingerprint: b.ItemFingerprint, ItemType: itemType, FullName: fullName,
		Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: p}}
	hash, err := f.ComputeBoundaryHash()
	if err != nil || hash != b.BoundaryHash {
		return errors.New("invalid DataItem authorization binding")
	}
	return nil
}

func (f AuthorizationFacts) ApprovedBinding() (Binding, error) {
	if f.Validate() != nil || f.BasisState != AuthorizationBasisComplete {
		return Binding{}, errors.New("complete DataItem boundary required")
	}
	b := Binding{ItemFingerprint: f.ItemFingerprint, BoundaryHash: *f.BoundaryHash, NativeTarget: f.Boundary.NativeTarget}
	b.NativeTarget.Segments = append([]plugin.EngineCatalogSegment(nil), b.NativeTarget.Segments...)
	return b, b.Validate()
}

// NativeCandidateFingerprint selects one possible Meta record only. Ownership
// is established by reading that record from Meta and comparing its complete
// typed boundary; a candidate (including a dotted-name collision) is no proof.
func NativeCandidateFingerprint(path plugin.EngineCatalogPath) (string, error) {
	itemType, fullName, err := nativeSingleIdentity(path)
	if err != nil {
		return "", err
	}
	fingerprint := models.GenerateItemFingerprint(path.EngineID, fullName)
	f := AuthorizationFacts{SchemaVersion: AuthorizationFactsSchemaV1, EngineID: path.EngineID,
		ItemFingerprint: fingerprint, ItemType: itemType, FullName: fullName,
		Boundary: &AuthorizationBoundary{Layout: "single", NativeTarget: path}}
	if _, err := f.ComputeBoundaryHash(); err != nil {
		return "", err
	}
	return fingerprint, nil
}

// ProveNativeTarget must only consume facts read from the trusted Meta owner.
// Complete identity and typed path equality are mandatory, not a name fallback.
func (f AuthorizationFacts) ProveNativeTarget(path plugin.EngineCatalogPath) (Binding, error) {
	b, err := f.ApprovedBinding()
	if err != nil || !reflect.DeepEqual(b.NativeTarget, path) {
		return Binding{}, errors.New("native target has no matching complete DataItem boundary")
	}
	return b, nil
}
