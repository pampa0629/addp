// Package factstest creates complete, canonical single-table test facts.
// It is not a production facts reader or a native authorization fallback.
package factstest

import (
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"time"
)

func Single(path plugin.EngineCatalogPath) authorizationfacts.AuthorizationFacts {
	fingerprint, err := authorizationfacts.NativeCandidateFingerprint(path)
	if err != nil {
		panic(err)
	}
	facts := authorizationfacts.AuthorizationFacts{SchemaVersion: authorizationfacts.AuthorizationFactsSchemaV1,
		EngineID: path.EngineID, ItemFingerprint: fingerprint, ItemType: "table", FullName: path.Segments[1].Name + "." + path.Segments[2].Name,
		BasisState: authorizationfacts.AuthorizationBasisComplete, ObservedAt: time.Now().UTC(),
		Boundary: &authorizationfacts.AuthorizationBoundary{Layout: "single", NativeTarget: path}}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		panic(err)
	}
	facts.BoundaryHash = &hash
	return facts
}

func Binding(path plugin.EngineCatalogPath) authorizationfacts.Binding {
	binding, err := Single(path).ApprovedBinding()
	if err != nil {
		panic(err)
	}
	return binding
}
