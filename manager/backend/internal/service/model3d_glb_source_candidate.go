package service

import (
	"bytes"
	"encoding/json"
	"path"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	"github.com/addp/common/format/plugins/obj"
	commonJSON "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// This is an owner candidate assembled from the existing Meta detail response.
// It is not trusted authorization facts: System independently obtains and
// validates those facts, including declaration matching and parent ownership.
type model3DGLBSourceCandidate struct {
	composition  *authorizationfacts.FileCompositionBoundary
	declarations []string
	libraries    []obj.MaterialLibraryDeclaration
}

func model3DGLBCandidateFromMeta(tenantID uint, source Model3DGLBSourceConfig, item *commonModels.MetaItem) (*model3DGLBSourceCandidate, error) {
	reject := ErrModel3DGLBSourceAuthorizationRequired
	identity := authorizationfacts.FileCompositionBoundary{Format: source.Format}
	if tenantID == 0 || source.ItemID == 0 || item == nil || item.ID != source.ItemID || item.TenantID != tenantID ||
		item.EngineID != source.SourceEngineID || (item.ItemType != "file" && item.ItemType != "object") || item.Fingerprint == "" || item.Fingerprint != source.ItemFingerprint ||
		!commonExecution.ManagerModel3DFileFormat(source.Format) {
		return nil, reject
	}
	locator, err := resourcetree.ParseURI(source.ItemLocator)
	if err != nil || locator.EngineID != item.EngineID || string(locator.Type) != item.ItemType || locator.FullName() != item.FullName {
		return nil, reject
	}
	targetFor := func(name string) plugin.EngineCatalogPath {
		if item.ItemType == "object" {
			target, _ := plugin.ObjectItemPathFromRefPath(item.EngineID, name)
			return target
		}
		return plugin.FileItemPath(item.EngineID, name)
	}
	primary := targetFor(item.FullName)
	if primary.StringPath() != item.FullName || path.Base(item.FullName) != item.Name ||
		commonJSON.String(item.Attributes, "item", "layout") != identity.Layout() ||
		commonJSON.String(item.Attributes, "item", "data_type") != "model_3d" ||
		commonJSON.String(item.Attributes, "item", "format") != source.Format ||
		commonJSON.String(item.Attributes, "storage", "physical_path") != item.FullName {
		return nil, reject
	}
	type rawRef struct {
		Path      string `json:"path"`
		Role      string `json:"role"`
		Required  *bool  `json:"required"`
		Primary   bool   `json:"primary"`
		Extension string `json:"extension"`
	}
	var refs []rawRef
	itemAttrs := commonJSON.Section(item.Attributes, "item")
	raw, exists := itemAttrs["refs"]
	if exists {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, reject
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&refs) != nil || refs == nil {
			return nil, reject
		}
	} else if source.Format == "ifc" {
		required := true
		refs = []rawRef{{Path: item.FullName, Role: "model", Required: &required, Primary: true}}
	}
	if len(refs) == 0 || len(refs) > authorizationfacts.MaxFileCompositionRefs {
		return nil, reject
	}
	compositionRefs := make([]authorizationfacts.FileCompositionRef, 0, len(refs))
	seen := make(map[string]bool, len(refs))
	for _, ref := range refs {
		target := targetFor(ref.Path)
		if target.StringPath() != ref.Path || seen[ref.Path] || ref.Required == nil || !*ref.Required {
			return nil, reject
		}
		seen[ref.Path] = true
		if ref.Primary && (ref.Path != item.FullName || ref.Role != identity.PrimaryRole()) {
			return nil, reject
		}
		compositionRefs = append(compositionRefs, authorizationfacts.FileCompositionRef{Target: target, Role: ref.Role, Required: *ref.Required, Primary: ref.Primary})
	}
	composition, err := authorizationfacts.NewFileCompositionBoundary(source.Format, compositionRefs)
	if err != nil {
		return nil, reject
	}
	if (source.Format == "ifc" || source.Format == "stl" || source.Format == "glb") && len(composition.Refs) != 1 {
		return nil, reject
	}
	if (source.Format == "stl" || source.Format == "glb" || source.Format == "gltf") && commonJSON.Section(item.Attributes, "format_info."+source.Format)["composition_complete"] != true {
		return nil, reject
	}
	candidate := &model3DGLBSourceCandidate{composition: composition}
	if source.Format == "obj" {
		native := commonJSON.Section(item.Attributes, "format_info.obj")
		if native["composition_complete"] != true {
			return nil, reject
		}
		candidate.libraries, err = dataitem.DecodeOBJMaterialLibraries(native["material_libraries"])
		if err != nil {
			return nil, reject
		}
		expected, err := dataitem.OBJCompositionRefs(item.FullName, candidate.libraries)
		if err != nil || len(expected) != len(refs) {
			return nil, reject
		}
		byPath := make(map[string]rawRef, len(refs))
		for _, ref := range refs {
			byPath[ref.Path] = ref
		}
		for _, ref := range expected {
			actual, exists := byPath[ref.Path]
			if !exists || actual.Role != ref.Role || actual.Primary != ref.Primary {
				return nil, reject
			}
		}
	}
	if source.Format == "3ds" || source.Format == "dae" {
		native := commonJSON.Section(item.Attributes, "format_info."+source.Format)
		if native["scan_complete"] != true {
			return nil, reject
		}
		encoded, err := json.Marshal(native["texture_refs"])
		if err != nil || json.Unmarshal(encoded, &candidate.declarations) != nil || candidate.declarations == nil {
			return nil, reject
		}
	}
	return candidate, nil
}
