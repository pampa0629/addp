package service

import (
	"bytes"
	"encoding/json"
	"errors"
	"path"
	"strconv"
	"strings"

	"github.com/addp/common/dataitem"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	commonExecution "github.com/addp/common/execution"
	commonformat "github.com/addp/common/format"
	"github.com/addp/common/format/plugins/fbx"
	commonjson "github.com/addp/common/jsonmap"
	"github.com/addp/meta/internal/metaitem"
	"github.com/addp/meta/internal/models"
	"gorm.io/gorm"
)

type fileAuthorizationRef struct {
	Path      string `json:"path"`
	Role      string `json:"role"`
	Required  *bool  `json:"required"`
	Primary   bool   `json:"primary"`
	Extension string `json:"extension"`
}

// This projection consumes committed facts only. Source reads and current
// permissions belong to the execution owner and System, respectively.
func projectFileAuthorizationFacts(tx *gorm.DB, item models.MetaItem, facts *authorizationfacts.AuthorizationFacts) error {
	finish := func(state authorizationfacts.AuthorizationBasisState) error {
		facts.BasisState = state
		return facts.Validate()
	}
	layout := commonjson.String(item.Attributes, "item", "layout")
	dataType := commonjson.String(item.Attributes, "item", "data_type")
	format := commonjson.String(item.Attributes, "item", "format")
	if layout == "" || dataType == "" || format == "" {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	identity := authorizationfacts.FileCompositionBoundary{Format: format}
	if layout != identity.Layout() || dataType != "model_3d" || !commonExecution.ManagerModel3DFileFormat(format) {
		return finish(authorizationfacts.AuthorizationBasisUnsupported)
	}
	targetFor := func(name string) plugin.EngineCatalogPath {
		if item.ItemType == plugin.EngineCatalogTermObject {
			target, _ := plugin.ObjectItemPathFromRefPath(item.EngineID, name)
			return target
		}
		return plugin.FileItemPath(item.EngineID, name)
	}
	primary := targetFor(item.FullName)
	if primary.StringPath() != item.FullName || path.Base(item.FullName) != item.Name ||
		strings.ToLower(path.Ext(item.FullName)) != "."+format || commonjson.String(item.Attributes, "storage", "physical_path") != item.FullName {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	state, err := validateFileAuthorizationParents(tx, item)
	if err != nil {
		return err
	}
	if state != authorizationfacts.AuthorizationBasisComplete {
		return finish(state)
	}
	itemAttrs, _ := item.Attributes["item"].(map[string]interface{})
	rawRefs, hasRefs := itemAttrs["refs"]
	var refs []fileAuthorizationRef
	if hasRefs {
		encoded, err := json.Marshal(rawRefs)
		if err != nil {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&refs) != nil || refs == nil {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
	}
	if !hasRefs && format == "ifc" {
		required := true
		refs = []fileAuthorizationRef{{Path: item.FullName, Role: "model", Required: &required, Primary: true}}
	}
	if len(refs) == 0 {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	if len(refs) > authorizationfacts.MaxFileCompositionRefs {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	byPath := make(map[string]fileAuthorizationRef, len(refs))
	compositionRefs := make([]authorizationfacts.FileCompositionRef, 0, len(refs))
	primaryCount := 0
	for _, ref := range refs {
		target := targetFor(ref.Path)
		if ref.Path == "" || target.StringPath() != ref.Path || ref.Required == nil || !*ref.Required {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		if _, duplicate := byPath[ref.Path]; duplicate {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		if ref.Primary {
			primaryCount++
			if ref.Path != item.FullName || ref.Role != identity.PrimaryRole() {
				return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
			}
		} else if ref.Role != "texture" && format != "obj" && format != "gltf" {
			suffix := strings.TrimPrefix(ref.Role, "texture_")
			n, err := strconv.Atoi(suffix)
			if !strings.HasPrefix(ref.Role, "texture_") || err != nil || n < 2 || strconv.Itoa(n) != suffix {
				return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
			}
		}
		byPath[ref.Path] = ref
		compositionRefs = append(compositionRefs, authorizationfacts.FileCompositionRef{Target: target, Role: ref.Role, Required: true, Primary: ref.Primary})
	}
	if primaryCount != 1 {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	if (format == "ifc" || format == "stl" || format == "glb" || format == "fbx") && len(refs) != 1 {
		return finish(authorizationfacts.AuthorizationBasisUnsupported)
	}
	if (format == "stl" || format == "glb" || format == "gltf" || format == "fbx") && commonjson.Section(item.Attributes, "format_info."+format)["composition_complete"] != true {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	if format == "fbx" && commonjson.String(item.Attributes, "format_info.fbx", "profile") != fbx.Profile {
		return finish(authorizationfacts.AuthorizationBasisIncomplete)
	}
	if format == "obj" {
		native := commonjson.Section(item.Attributes, "format_info.obj")
		if native["composition_complete"] != true {
			return finish(authorizationfacts.AuthorizationBasisIncomplete)
		}
		libraries, err := dataitem.DecodeOBJMaterialLibraries(native["material_libraries"])
		if err != nil {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		expected, err := dataitem.OBJCompositionRefs(item.FullName, libraries)
		if err != nil {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		for _, ref := range expected {
			actual, found := byPath[ref.Path]
			if !found {
				return finish(authorizationfacts.AuthorizationBasisIncomplete)
			}
			if actual.Role != ref.Role || actual.Primary != ref.Primary {
				return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
			}
		}
		if len(expected) != len(byPath) {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
	}
	if format == "3ds" || format == "dae" {
		native := commonjson.Section(item.Attributes, "format_info."+format)
		if complete, ok := native["scan_complete"].(bool); !ok || !complete {
			return finish(authorizationfacts.AuthorizationBasisIncomplete)
		}
		encoded, err := json.Marshal(native["texture_refs"])
		if err != nil {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
		var declarations []string
		if json.Unmarshal(encoded, &declarations) != nil || declarations == nil {
			return finish(authorizationfacts.AuthorizationBasisIncomplete)
		}
		expected := map[string]bool{item.FullName: true}
		for _, ref := range declarations {
			if format == "dae" {
				decoded, err := commonformat.DecodeDAETextureRef(ref)
				if err != nil {
					return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
				}
				ref = decoded
			}
			resolved, ok := metaitem.ResolveExchangeModelTexturePath(item.FullName, ref)
			if !ok {
				return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
			}
			if expected[resolved] {
				return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
			}
			expected[resolved] = true
			if _, found := byPath[resolved]; !found {
				return finish(authorizationfacts.AuthorizationBasisIncomplete)
			}
		}
		if len(expected) != len(byPath) {
			return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
		}
	}
	composition, err := authorizationfacts.NewFileCompositionBoundary(format, compositionRefs)
	if err != nil {
		return finish(authorizationfacts.AuthorizationBasisNeedsRefresh)
	}
	facts.Boundary = &authorizationfacts.AuthorizationBoundary{Layout: composition.Layout(), FileComposition: composition}
	hash, err := facts.ComputeBoundaryHash()
	if err != nil {
		return err
	}
	facts.BoundaryHash = &hash
	return finish(authorizationfacts.AuthorizationBasisComplete)
}

func validateFileAuthorizationParents(tx *gorm.DB, item models.MetaItem) (authorizationfacts.AuthorizationBasisState, error) {
	parts := strings.Split(item.FullName, "/")
	if len(parts) > 64 {
		return authorizationfacts.AuthorizationBasisNeedsRefresh, nil
	}
	id := item.NodeID
	rootTerm := plugin.EngineCatalogTermRoot
	if item.ItemType == plugin.EngineCatalogTermObject {
		rootTerm = plugin.EngineCatalogTermService
	}
	for i := len(parts) - 2; i >= -1; i-- {
		var node models.MetaNode
		err := tx.Where("tenant_id = ? AND engine_id = ? AND id = ? AND deleted_at IS NULL", item.TenantID, item.EngineID, id).First(&node).Error
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return authorizationfacts.AuthorizationBasisIncomplete, nil
		}
		if err != nil {
			return "", err
		}
		if i == -1 {
			if node.NodeType != rootTerm || node.ParentNodeID != nil || node.FullName != "" {
				return authorizationfacts.AuthorizationBasisNeedsRefresh, nil
			}
			return authorizationfacts.AuthorizationBasisComplete, nil
		}
		expectedType := "dir"
		if item.ItemType == plugin.EngineCatalogTermObject {
			expectedType = "prefix"
			if i == 0 {
				expectedType = "bucket"
			}
		}
		if node.NodeType != expectedType || node.Name != parts[i] || node.FullName != strings.Join(parts[:i+1], "/") || node.ParentNodeID == nil {
			return authorizationfacts.AuthorizationBasisNeedsRefresh, nil
		}
		id = *node.ParentNodeID
	}
	return authorizationfacts.AuthorizationBasisNeedsRefresh, nil
}
