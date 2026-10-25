package dataitem

import (
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/format"
)

// GLTFCompositionRefs projects already parsed resources into exact storage
// names. URI decoding belongs solely to format; this helper never decodes,
// enumerates directories, opens content or makes an authorization decision.
func GLTFCompositionRefs(primary string, resources []format.GLTFResourceRef) ([]ItemRef, error) {
	if !validGLTFStoragePath(primary) || !strings.EqualFold(path.Ext(primary), ".gltf") || resources == nil {
		return nil, fmt.Errorf("invalid glTF composition")
	}
	byPath := make(map[string]string, len(resources))
	for _, resource := range resources {
		if (resource.Role != "buffer" && resource.Role != "image") || !validGLTFStoragePath(resource.Path) {
			return nil, fmt.Errorf("invalid glTF resource path or role")
		}
		name := path.Join(path.Dir(primary), resource.Path)
		if name == primary {
			return nil, fmt.Errorf("glTF cannot reference its manifest as a resource")
		}
		if role, exists := byPath[name]; exists && role != resource.Role {
			return nil, fmt.Errorf("conflicting glTF resource roles")
		}
		byPath[name] = resource.Role
	}
	if len(byPath)+1 > authorizationfacts.MaxFileCompositionRefs {
		return nil, fmt.Errorf("glTF exceeds the exact composition budget")
	}
	names := make([]string, 0, len(byPath))
	for name := range byPath {
		names = append(names, name)
	}
	sort.Strings(names)
	refs := []ItemRef{{Path: primary, Role: "manifest", Required: true, Primary: true, Extension: strings.ToLower(path.Ext(primary))}}
	counts := map[string]int{}
	for _, name := range names {
		role := byPath[name]
		index := counts[role]
		counts[role]++
		if index > 0 {
			role += "_" + strconv.Itoa(index)
		}
		refs = append(refs, ItemRef{Path: name, Role: role, Required: true, Extension: strings.ToLower(path.Ext(name))})
	}
	return refs, nil
}

func validGLTFStoragePath(name string) bool {
	if name == "" || len(name) > 4096 || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, r := range name {
		if unicode.IsControl(r) {
			return false
		}
	}
	for _, segment := range strings.Split(name, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
	}
	return true
}
