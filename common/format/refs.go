package format

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/addp/common/contentio"
)

type RelatedRefSpec struct {
	Extension string
	Role      string
	Required  bool
	Primary   bool
}

// RelatedRef describes a content ref inside a related-ref collection.
//
// Ref is the bottom-level content locator. Required and Primary
// are collection annotations and must not be interpreted by contentio itself.
type RelatedRef struct {
	Ref      contentio.Ref
	Required bool
	Primary  bool
}

func NewRelatedRef(ref contentio.Ref, required, primary bool) RelatedRef {
	return RelatedRef{Ref: ref, Required: required, Primary: primary}
}

func ValidateRelatedRefSpecs(specs []RelatedRefSpec) error {
	if len(specs) == 0 {
		return fmt.Errorf("related ref specs cannot be empty")
	}
	primaryCount := 0
	for _, spec := range specs {
		if NormalizeExtension(spec.Extension) == "" {
			return fmt.Errorf("related ref spec extension cannot be empty")
		}
		if spec.Primary {
			primaryCount++
		}
	}
	switch primaryCount {
	case 1:
		return nil
	case 0:
		return fmt.Errorf("related ref specs require exactly one primary ref spec, got none")
	default:
		return fmt.Errorf("related ref specs require exactly one primary ref spec, got %d", primaryCount)
	}
}

func SameBasenameRelatedRefs(mainPath string, specs []RelatedRefSpec) []RelatedRef {
	basePath := strings.TrimSuffix(strings.Trim(mainPath, "/"), filepath.Ext(mainPath))
	refs := make([]RelatedRef, 0, len(specs))
	for _, spec := range specs {
		ext := NormalizeExtension(spec.Extension)
		role := normalizedRefRole(spec.Role, ext, spec.Primary)
		ref := contentio.NewRef(basePath+ext, role)
		refs = append(refs, NewRelatedRef(ref, spec.Required, spec.Primary))
	}
	return refs
}

func PrimaryRelatedRef(refs []RelatedRef) (RelatedRef, error) {
	primaryRefs := make([]RelatedRef, 0, 1)
	for _, ref := range refs {
		if ref.Primary {
			primaryRefs = append(primaryRefs, ref)
		}
	}
	switch len(primaryRefs) {
	case 1:
		if strings.TrimSpace(primaryRefs[0].Ref.Path) == "" {
			return RelatedRef{}, fmt.Errorf("primary related ref path is empty")
		}
		return primaryRefs[0], nil
	case 0:
		return RelatedRef{}, fmt.Errorf("related refs require exactly one primary ref, got none")
	default:
		return RelatedRef{}, fmt.Errorf("related refs require exactly one primary ref, got %d", len(primaryRefs))
	}
}

func ValidateRelatedRefs(refs []RelatedRef) error {
	if len(refs) == 0 {
		return fmt.Errorf("related refs cannot be empty")
	}
	_, err := PrimaryRelatedRef(refs)
	return err
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
		return contentio.RoleMain
	}
	return strings.TrimPrefix(NormalizeExtension(ext), ".")
}
