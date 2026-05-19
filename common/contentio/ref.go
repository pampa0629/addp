package contentio

import (
	"path/filepath"
	"strings"
)

const (
	RoleMain      = "main"
	RoleScope     = "scope"
	RoleManifest  = "manifest"
	RoleAuxiliary = "auxiliary"
)

type Ref struct {
	Path string `json:"path"`
	Role string `json:"role,omitempty"`
}

// NewRef creates a content ref with ADDP's default normalization:
// slash-trimmed path and lowercase role.
func NewRef(path string, role string) Ref {
	path = strings.Trim(path, "/")
	role = strings.ToLower(strings.TrimSpace(role))
	return Ref{
		Path: path,
		Role: role,
	}
}

func BaseName(ref Ref) string {
	path := strings.Trim(ref.Path, "/")
	if path == "" {
		return ""
	}
	return filepath.Base(path)
}
