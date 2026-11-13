package authorizationfacts

import (
	"errors"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
)

// NativeTableBindingFromMeta proves a schema/table ReadSet target against the
// actual Meta record already obtained by its consumer. The caller must verify
// the record's tenant and the Engine's PostgreSQL type. This is resource
// evidence, never a Grant or a caller-supplied permission decision.
func NativeTableBindingFromMeta(item models.MetaItem, target plugin.EngineCatalogPath) (Binding, error) {
	if item.ID == 0 || item.NodeID == 0 || item.TenantID == 0 || item.ItemType != "table" ||
		item.EngineID != target.EngineID || len(target.Segments) != 3 ||
		target.Segments[1].Term != plugin.EngineCatalogTermSchema ||
		target.Segments[2].Term != plugin.EngineCatalogTermTable ||
		jsonmap.String(item.Attributes, "item", "layout") != "single" {
		return Binding{}, errors.New("incomplete native table Meta evidence")
	}
	kind := jsonmap.String(item.Attributes, "type_info.table", "kind")
	if kind != "" && kind != "table" {
		return Binding{}, errors.New("native table evidence must not describe a view")
	}
	if item.Name != target.Segments[2].Name ||
		jsonmap.String(item.Attributes, "storage", "schema_name") != target.Segments[1].Name ||
		item.FullName != target.Segments[1].Name+"."+item.Name {
		return Binding{}, errors.New("native table Meta identity does not match ReadSet")
	}
	binding, err := NativeCandidateBinding(target)
	if err != nil || item.Fingerprint != binding.ItemFingerprint {
		return Binding{}, errors.New("native table Meta fingerprint does not match ReadSet")
	}
	// Detach the target from the request/Provider's mutable slice.
	binding.NativeTarget.Segments = append([]plugin.EngineCatalogSegment(nil), target.Segments...)
	return binding, nil
}
