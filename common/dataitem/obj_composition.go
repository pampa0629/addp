package dataitem

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"sort"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/dataitem/authorizationfacts"
	"github.com/addp/common/format/plugins/obj"
)

// DecodeOBJMaterialLibraries rejects absent, partial or unknown structural
// fields. Complete empty observations must arrive as [], never null.
func DecodeOBJMaterialLibraries(raw any) ([]obj.MaterialLibraryDeclaration, error) {
	encoded, err := json.Marshal(raw)
	if err != nil || len(encoded) > 64<<10 {
		return nil, fmt.Errorf("invalid OBJ material library snapshot")
	}
	var result []obj.MaterialLibraryDeclaration
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&result) != nil || result == nil {
		return nil, fmt.Errorf("incomplete OBJ material library snapshot")
	}
	for _, library := range result {
		if library.TextureRefs == nil {
			return nil, fmt.Errorf("incomplete OBJ material texture snapshot")
		}
	}
	return result, nil
}

// OBJCompositionRefs resolves already-observed declarations without I/O or
// authorization. MTLs are direct siblings of the OBJ in the admitted Runtime.
// Texture paths are literal and remain beneath that same parent.
func OBJCompositionRefs(primary string, libraries []obj.MaterialLibraryDeclaration) ([]ItemRef, error) {
	if !validOBJRelativePath(primary) || !strings.EqualFold(path.Ext(primary), ".obj") || libraries == nil {
		return nil, fmt.Errorf("invalid OBJ composition")
	}
	refs := []ItemRef{{Path: primary, Role: "model", Required: true, Primary: true, Extension: strings.ToLower(path.Ext(primary))}}
	parent := path.Dir(primary)
	textures := map[string]bool{}
	previous := ""
	for i, library := range libraries {
		name := library.Reference
		if !ValidOBJMaterialLibraryRef(name) || (i > 0 && previous >= name) || library.TextureRefs == nil {
			return nil, fmt.Errorf("invalid OBJ material declaration")
		}
		previous = name
		refs = append(refs, ItemRef{Path: path.Join(parent, name), Role: objRefRole("material_library", i), Required: true, Extension: strings.ToLower(path.Ext(name))})
		seen := map[string]bool{}
		for _, texture := range library.TextureRefs {
			if !validOBJRelativePath(texture) || seen[texture] {
				return nil, fmt.Errorf("invalid OBJ texture declaration")
			}
			switch strings.ToLower(path.Ext(texture)) {
			case ".png", ".jpg", ".jpeg":
			default:
				return nil, fmt.Errorf("unsupported OBJ texture extension")
			}
			seen[texture] = true
			textures[path.Join(parent, texture)] = true
		}
	}
	names := make([]string, 0, len(textures))
	for name := range textures {
		names = append(names, name)
	}
	sort.Strings(names)
	for i, name := range names {
		refs = append(refs, ItemRef{Path: name, Role: objRefRole("texture", i), Required: true, Extension: strings.ToLower(path.Ext(name))})
	}
	if len(refs) > authorizationfacts.MaxFileCompositionRefs {
		return nil, fmt.Errorf("OBJ composition exceeds the exact reference budget")
	}
	return refs, nil
}

func ValidOBJMaterialLibraryRef(name string) bool {
	return validOBJRelativePath(name) && path.Base(name) == name && strings.EqualFold(path.Ext(name), ".mtl")
}

func validOBJRelativePath(name string) bool {
	if name == "" || len(name) > obj.MaxCompositionNameBytes || !utf8.ValidString(name) || strings.ContainsAny(name, "\\:\x00\r\n\"'#") || strings.HasPrefix(name, "/") {
		return false
	}
	for _, c := range name {
		if unicode.IsControl(c) {
			return false
		}
	}
	for _, part := range strings.Split(name, "/") {
		if part == "" || part == "." || part == ".." {
			return false
		}
	}
	return true
}

func objRefRole(role string, index int) string {
	if index == 0 {
		return role
	}
	return role + "_" + strconv.Itoa(index+1)
}
