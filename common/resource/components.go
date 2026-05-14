package resource

import (
	"errors"
	"path/filepath"
	"strings"
)

var ErrComponentNotFound = errors.New("resource component not found")

type ComponentSpec struct {
	Extension string
	Role      string
	Required  bool
	Primary   bool
}

func SameBasenameComponents(mainPath string, specs []ComponentSpec) []ComponentRef {
	basePath := strings.TrimSuffix(strings.Trim(mainPath, "/"), filepath.Ext(mainPath))
	components := make([]ComponentRef, 0, len(specs))
	for _, spec := range specs {
		ext := NormalizeExtension(spec.Extension)
		componentPath := basePath + ext
		components = append(components, ComponentRef{
			ResourceRef:   NewResourceRef(componentPath, ResourceRoleComponent),
			ComponentRole: normalizedComponentRole(spec.Role, ext),
			Required:      spec.Required,
		})
	}
	return components
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

func normalizedComponentRole(role, ext string) string {
	role = strings.ToLower(strings.TrimSpace(role))
	if role != "" {
		return role
	}
	return strings.TrimPrefix(NormalizeExtension(ext), ".")
}
