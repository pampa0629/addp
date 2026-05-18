package contentio

import (
	"path/filepath"
	"strings"
)

type RelatedRefSpec struct {
	Extension string
	Role      string
	Required  bool
	Primary   bool
}

func SameBasenameRefs(mainPath string, specs []RelatedRefSpec) []Ref {
	basePath := strings.TrimSuffix(strings.Trim(mainPath, "/"), filepath.Ext(mainPath))
	refs := make([]Ref, 0, len(specs))
	for _, spec := range specs {
		ext := NormalizeExtension(spec.Extension)
		role := normalizedRefRole(spec.Role, ext, spec.Primary)
		ref := NewRef(basePath+ext, role)
		ref.Required = spec.Required
		ref.Primary = spec.Primary
		refs = append(refs, ref)
	}
	return refs
}

func NormalizeExtension(ext string) string {
	ext = strings.ToLower(strings.TrimSpace(ext))
	if ext == "" {
		return ""
	}
	if strings.HasPrefix(ext, ".") {
		return ext
	}
	return "." + ext
}

func normalizedRefRole(role, ext string, primary bool) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "" {
		return role
	}
	if primary {
		return RoleMain
	}
	return strings.TrimPrefix(NormalizeExtension(ext), ".")
}
