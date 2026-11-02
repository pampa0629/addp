package service

import (
	"encoding/json"
	"path"
	"strings"

	commonAPI "github.com/addp/common/api"
	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format/plugins/envi"
	commonjson "github.com/addp/common/jsonmap"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
)

// This is a projection of trusted current Meta facts, not a Grant or permission
// verdict. Both current reads and the COG owner use this same composition.
func enviCompositionFromMeta(tenantID, engineID, itemID uint, fingerprint, locator string, item *commonModels.MetaItem) (*authorizationfacts.FileCompositionBoundary, error) {
	if tenantID == 0 || item == nil || item.ID != itemID || item.TenantID != tenantID || item.EngineID != engineID || item.Fingerprint != fingerprint || item.Fingerprint != commonModels.GenerateItemFingerprint(engineID, item.FullName) || (item.ItemType != "file" && item.ItemType != "object") || path.Base(item.FullName) != item.Name {
		return nil, commonAPI.ErrForbidden
	}
	loc, err := resourcetree.ParseURI(locator)
	if err != nil || loc.EngineID != engineID || loc.FullName() != item.FullName || string(loc.Type) != item.ItemType {
		return nil, commonAPI.ErrForbidden
	}
	attrs := commonjson.Section(item.Attributes, "item")
	native := commonjson.Section(item.Attributes, "format_info.envi")
	sha, _ := native["header_sha256"].(string)
	headerPath, _ := native["header_path"].(string)
	if attrs["format"] != "envi" || attrs["data_type"] != "media" || attrs["layout"] != "multi" || native["profile"] != envi.Profile || native["composition_complete"] != true || !authorizationfacts.ValidItemFingerprint(sha) || commonjson.String(item.Attributes, "storage", "physical_path") != item.FullName {
		return nil, commonAPI.ErrForbidden
	}
	raw, err := json.Marshal(attrs["refs"])
	if err != nil {
		return nil, commonAPI.ErrForbidden
	}
	var refs []struct {
		Path     string `json:"path"`
		Role     string `json:"role"`
		Required *bool  `json:"required"`
		Primary  bool   `json:"primary"`
	}
	if json.Unmarshal(raw, &refs) != nil || len(refs) != 2 {
		return nil, commonAPI.ErrForbidden
	}
	compositionRefs := make([]authorizationfacts.FileCompositionRef, 0, 2)
	for _, ref := range refs {
		if ref.Required == nil || !*ref.Required || (ref.Primary && (ref.Path != item.FullName || ref.Role != "raster")) || (!ref.Primary && (ref.Path != headerPath || ref.Role != "header")) {
			return nil, commonAPI.ErrForbidden
		}
		if path.Dir(ref.Path) != path.Dir(item.FullName) {
			return nil, commonAPI.ErrForbidden
		}
		target := plugin.FileItemPath(engineID, ref.Path)
		if item.ItemType == "object" {
			target, err = plugin.ObjectItemPathFromRefPath(engineID, ref.Path)
			if err != nil {
				return nil, commonAPI.ErrForbidden
			}
		}
		if target.StringPath() != ref.Path {
			return nil, commonAPI.ErrForbidden
		}
		compositionRefs = append(compositionRefs, authorizationfacts.FileCompositionRef{Target: target, Role: ref.Role, Required: true, Primary: ref.Primary})
	}
	base := strings.TrimSuffix(item.FullName, path.Ext(item.FullName))
	if !strings.EqualFold(headerPath, item.FullName+".hdr") && !strings.EqualFold(headerPath, base+".hdr") {
		return nil, commonAPI.ErrForbidden
	}
	composition, err := authorizationfacts.NewFileCompositionBoundary("envi", compositionRefs)
	if err != nil {
		return nil, commonAPI.ErrForbidden
	}
	return composition, nil
}
