package format

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"math/big"
	"net/url"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/addp/common/jsonmap"
)

const MaxGLTFExternalFiles = 199 // The primary plus dependencies fit the existing 200-file boundary.
const MaxGLTFImagePixels = 16 * 1024 * 1024
const MaxGLTFTotalImagePixels = 64 * 1024 * 1024
const MaxGLTFAccessorBytes = 64 * 1024 * 1024

// GLTFComposition contains format facts only. Document must not be persisted
// wholesale: it can contain inline binary data. Resources exclude embedded data.
type GLTFComposition struct {
	Document  *GLTFDocument
	Resources []GLTFResourceRef
}

// Paths returns a non-nil, stable, unique external-file set, without opening it.
func (c *GLTFComposition) Paths() []string {
	paths := []string{}
	seen := map[string]bool{}
	for _, ref := range c.Resources {
		if !seen[ref.Path] {
			paths = append(paths, ref.Path)
			seen[ref.Path] = true
		}
	}
	return paths
}

// ParseGLTFComposition is the sole glTF source parser. Success proves complete
// bounded structural declarations under the static core profile, not external
// content integrity, source versions, or permission to read any resource.
func ParseGLTFComposition(ctx context.Context, raw []byte) (*GLTFComposition, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if len(raw) == 0 || int64(len(raw)) > MaxGLTFManifestBytes {
		return nil, fmt.Errorf("glTF manifest outside size budget")
	}
	if !gltfValidStringEscapes(raw) {
		return nil, fmt.Errorf("invalid glTF Unicode escape")
	}
	root, err := jsonmap.DecodeObjectStrict(raw, int(MaxGLTFManifestBytes), 64)
	if err != nil {
		return nil, fmt.Errorf("invalid glTF manifest: %w", err)
	}
	if err := gltfProfile(root, ""); err != nil {
		return nil, err
	}
	asset, ok := root["asset"].(map[string]any)
	if !ok || asset["version"] != "2.0" {
		return nil, fmt.Errorf("glTF requires asset.version 2.0")
	}
	if v, exists := asset["minVersion"]; exists && v != "2.0" {
		return nil, fmt.Errorf("unsupported glTF minVersion")
	}
	arrays := map[string][]map[string]any{}
	for _, key := range []string{"buffers", "bufferViews", "images", "accessors", "meshes", "nodes", "scenes", "textures", "materials", "samplers", "cameras"} {
		values, err := gltfObjects(root, key)
		if err != nil {
			return nil, err
		}
		arrays[key] = values
	}
	refs := []GLTFResourceRef{}
	inline := make([][]byte, len(arrays["buffers"]))
	lengths := make([]int64, len(inline))
	roles := map[string]string{}
	imageBudget := int64(MaxGLTFTotalImagePixels)
	addRef := func(ref GLTFResourceRef) error {
		if role, exists := roles[ref.Path]; exists && role != ref.Role {
			return fmt.Errorf("glTF resource has conflicting roles")
		}
		roles[ref.Path] = ref.Role
		if len(roles) > MaxGLTFExternalFiles {
			return fmt.Errorf("glTF external-file budget exceeded")
		}
		refs = append(refs, ref)
		return nil
	}
	for i, buffer := range arrays["buffers"] {
		length, err := gltfIntField(buffer, "byteLength", 1, math.MaxInt64, false)
		if err != nil {
			return nil, err
		}
		lengths[i] = length
		uri, ok := buffer["uri"].(string)
		if !ok {
			return nil, fmt.Errorf("glTF buffer requires URI")
		}
		path, payload, mime, err := gltfResourceURI(uri)
		if err != nil {
			return nil, err
		}
		if path == "" {
			if mime != "application/octet-stream" && mime != "application/gltf-buffer" {
				return nil, fmt.Errorf("invalid glTF buffer data MIME")
			}
			if int64(len(payload)) < length {
				return nil, fmt.Errorf("glTF embedded buffer is truncated")
			}
			inline[i] = payload
		} else if err := addRef(GLTFResourceRef{Role: "buffer", Index: i, URI: uri, Path: path, ByteLength: length}); err != nil {
			return nil, err
		}
	}
	for _, view := range arrays["bufferViews"] {
		index, err := gltfIntField(view, "buffer", 0, int64(len(lengths))-1, false)
		if err != nil {
			return nil, err
		}
		offset, err := gltfIntField(view, "byteOffset", 0, math.MaxInt64, true)
		if err != nil {
			return nil, err
		}
		length, err := gltfIntField(view, "byteLength", 1, math.MaxInt64, false)
		if err != nil {
			return nil, err
		}
		if offset > lengths[index] || length > lengths[index]-offset {
			return nil, fmt.Errorf("glTF bufferView exceeds declared buffer")
		}
		if _, exists := view["byteStride"]; exists {
			stride, err := gltfIntField(view, "byteStride", 4, 252, false)
			if err != nil || stride%4 != 0 {
				return nil, fmt.Errorf("invalid glTF byteStride")
			}
		}
	}
	for i, picture := range arrays["images"] {
		uriValue, hasURI := picture["uri"]
		_, hasView := picture["bufferView"]
		if hasURI == hasView {
			return nil, fmt.Errorf("glTF image requires exactly one URI or bufferView")
		}
		mime := ""
		if value, exists := picture["mimeType"]; exists {
			mime, ok = value.(string)
			if !ok || (mime != "image/png" && mime != "image/jpeg") {
				return nil, fmt.Errorf("unsupported glTF image MIME")
			}
		}
		if hasURI {
			uri, ok := uriValue.(string)
			if !ok {
				return nil, fmt.Errorf("invalid glTF image URI")
			}
			path, payload, embeddedMIME, err := gltfResourceURI(uri)
			if err != nil {
				return nil, err
			}
			if path != "" {
				if err := addRef(GLTFResourceRef{Role: "image", Index: i, URI: uri, Path: path, MimeType: mime}); err != nil {
					return nil, err
				}
			} else {
				if mime != "" && mime != embeddedMIME {
					return nil, fmt.Errorf("glTF embedded image MIME mismatch")
				}
				if err := gltfImage(payload, embeddedMIME, &imageBudget); err != nil {
					return nil, err
				}
			}
		} else {
			if mime == "" {
				return nil, fmt.Errorf("glTF bufferView image requires MIME")
			}
			index, err := gltfIntField(picture, "bufferView", 0, int64(len(arrays["bufferViews"]))-1, false)
			if err != nil {
				return nil, err
			}
			view := arrays["bufferViews"][index]
			buffer := view["buffer"].(int64)
			if data := inline[buffer]; data != nil {
				offset, _ := view["byteOffset"].(int64)
				if err := gltfImage(data[offset:offset+view["byteLength"].(int64)], mime, &imageBudget); err != nil {
					return nil, err
				}
			}
		}
	}
	if err := gltfAccessors(arrays); err != nil {
		return nil, err
	}
	if err := gltfScene(root, arrays); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	// Domain integer fields were normalized exactly, accepting integer-valued
	// decimal/exponent JSON numbers without losing precision through float64.
	normalized, err := json.Marshal(root)
	if err != nil {
		return nil, err
	}
	var doc GLTFDocument
	if err := json.Unmarshal(normalized, &doc); err != nil {
		return nil, fmt.Errorf("invalid glTF metadata fields: %w", err)
	}
	return &GLTFComposition{Document: &doc, Resources: refs}, nil
}

func gltfProfile(value any, location string) error {
	switch v := value.(type) {
	case map[string]any:
		for key, child := range v {
			if key == "extensions" {
				m, ok := child.(map[string]any)
				if !ok || len(m) != 0 {
					return fmt.Errorf("glTF extensions unsupported")
				}
			}
			if key == "extensionsUsed" || key == "extensionsRequired" || (location == "" && (key == "animations" || key == "skins")) || key == "targets" || key == "weights" {
				items, ok := child.([]any)
				if !ok || len(items) != 0 {
					return fmt.Errorf("glTF animated, skinned, morph or extended sources unsupported")
				}
			}
			if key == "skin" {
				return fmt.Errorf("glTF skin unsupported")
			}
			if key == "uri" {
				parts := strings.Split(location, "/")
				if len(parts) != 3 || (parts[1] != "buffers" && parts[1] != "images") {
					return fmt.Errorf("glTF URI outside core resource declaration")
				}
			}
			token := strings.ReplaceAll(strings.ReplaceAll(key, "~", "~0"), "/", "~1")
			if err := gltfProfile(child, location+"/"+token); err != nil {
				return err
			}
		}
	case []any:
		for i, child := range v {
			if err := gltfProfile(child, fmt.Sprintf("%s/%d", location, i)); err != nil {
				return err
			}
		}
	case json.Number:
		lexeme := v.String()
		if len(lexeme) > 64 {
			return fmt.Errorf("glTF numeric representation exceeds budget")
		}
		if i := strings.IndexAny(lexeme, "eE"); i >= 0 {
			exponent, err := strconv.Atoi(lexeme[i+1:])
			if err != nil || exponent < -64 || exponent > 64 {
				return fmt.Errorf("glTF numeric exponent exceeds budget")
			}
		}
		value, err := v.Float64()
		if err != nil || math.IsInf(value, 0) || math.IsNaN(value) {
			return fmt.Errorf("nonfinite glTF number")
		}
	}
	return nil
}

func gltfObjects(root map[string]any, key string) ([]map[string]any, error) {
	value, exists := root[key]
	if !exists {
		return []map[string]any{}, nil
	}
	items, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid glTF %s array", key)
	}
	result := make([]map[string]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid glTF %s member", key)
		}
		result = append(result, object)
	}
	return result, nil
}

func gltfIntField(object map[string]any, key string, min, max int64, optional bool) (int64, error) {
	value, exists := object[key]
	if !exists && optional {
		return 0, nil
	}
	number, ok := value.(json.Number)
	if !ok {
		if n, ok := value.(int64); ok && n >= min && n <= max {
			return n, nil
		}
		return 0, fmt.Errorf("invalid glTF %s integer", key)
	}
	lexeme := number.String()
	if len(lexeme) > 64 {
		return 0, fmt.Errorf("glTF integer representation exceeds budget")
	}
	if i := strings.IndexAny(lexeme, "eE"); i >= 0 {
		exponent, err := strconv.Atoi(lexeme[i+1:])
		if err != nil || exponent < -64 || exponent > 64 {
			return 0, fmt.Errorf("glTF integer exponent exceeds budget")
		}
	}
	rat, ok := new(big.Rat).SetString(lexeme)
	if !ok || !rat.IsInt() || !rat.Num().IsInt64() {
		return 0, fmt.Errorf("invalid glTF %s integer", key)
	}
	n := rat.Num().Int64()
	if n < min || n > max {
		return 0, fmt.Errorf("glTF %s outside range", key)
	}
	object[key] = n
	return n, nil
}

func gltfResourceURI(raw string) (string, []byte, string, error) {
	invalid := fmt.Errorf("unsupported or unsafe glTF resource URI")
	if raw == "" || !utf8.ValidString(raw) || strings.ContainsFunc(raw, unicode.IsControl) {
		return "", nil, "", invalid
	}
	if strings.HasPrefix(strings.ToLower(raw), "data:") {
		comma := strings.IndexByte(raw, ',')
		if comma < 0 {
			return "", nil, "", invalid
		}
		header := strings.ToLower(raw[5:comma])
		mime := header
		encoded := false
		if strings.HasSuffix(header, ";base64") {
			mime = strings.TrimSuffix(header, ";base64")
			encoded = true
		}
		if mime != "application/octet-stream" && mime != "application/gltf-buffer" && mime != "image/png" && mime != "image/jpeg" {
			return "", nil, "", invalid
		}
		payload, err := url.PathUnescape(raw[comma+1:])
		if err != nil {
			return "", nil, "", invalid
		}
		data := []byte(payload)
		if encoded {
			if strings.ContainsAny(payload, "\r\n\t ") {
				return "", nil, "", invalid
			}
			data, err = base64.StdEncoding.Strict().DecodeString(payload)
			if err != nil {
				return "", nil, "", invalid
			}
		}
		if len(data) == 0 {
			return "", nil, "", invalid
		}
		return "", data, mime, nil
	}
	if len(raw) > 4096 || strings.ContainsAny(raw, "\\?#") || strings.ContainsFunc(raw, unicode.IsSpace) {
		return "", nil, "", invalid
	}
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "" || u.Host != "" || u.User != nil || u.Opaque != "" || u.ForceQuery || u.RawQuery != "" || u.Fragment != "" {
		return "", nil, "", invalid
	}
	escaped := u.EscapedPath()
	lower := strings.ToLower(escaped)
	if strings.Contains(lower, "%2f") || strings.Contains(lower, "%5c") {
		return "", nil, "", invalid
	}
	// URL.Path is already decoded exactly once. Never PathUnescape it again.
	path := u.Path
	if path == "" || strings.HasPrefix(path, "/") || !utf8.ValidString(path) || strings.ContainsAny(path, "\\:") || strings.ContainsFunc(path, unicode.IsControl) {
		return "", nil, "", invalid
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return "", nil, "", invalid
		}
	}
	return path, nil, "", nil
}

func gltfImage(payload []byte, mime string, budget *int64) error {
	expected := map[string]string{"image/png": "png", "image/jpeg": "jpeg"}[mime]
	if expected == "" {
		return fmt.Errorf("unsupported glTF embedded image")
	}
	cfg, kind, err := image.DecodeConfig(bytes.NewReader(payload))
	if err != nil || kind != expected || cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width > MaxGLTFImagePixels/cfg.Height {
		return fmt.Errorf("invalid glTF image or pixel budget")
	}
	pixels := int64(cfg.Width) * int64(cfg.Height)
	if pixels > *budget {
		return fmt.Errorf("glTF total image pixel budget exceeded")
	}
	*budget -= pixels
	_, kind, err = image.Decode(bytes.NewReader(payload))
	if err != nil || kind != expected {
		return fmt.Errorf("invalid glTF image payload")
	}
	return nil
}

func gltfAccessors(a map[string][]map[string]any) error {
	remaining := int64(MaxGLTFAccessorBytes)
	for _, accessor := range a["accessors"] {
		count, err := gltfIntField(accessor, "count", 1, math.MaxInt64, false)
		if err != nil {
			return err
		}
		component, err := gltfIntField(accessor, "componentType", 5120, 5126, false)
		if err != nil {
			return err
		}
		size := map[int64]int64{5120: 1, 5121: 1, 5122: 2, 5123: 2, 5125: 4, 5126: 4}[component]
		if size == 0 {
			return fmt.Errorf("invalid glTF component type")
		}
		kind, ok := accessor["type"].(string)
		dimensions := map[string]int64{"SCALAR": 1, "VEC2": 2, "VEC3": 3, "VEC4": 4, "MAT2": 4, "MAT3": 9, "MAT4": 16}
		dimension := dimensions[kind]
		if !ok || dimension == 0 {
			return fmt.Errorf("invalid glTF accessor type")
		}
		element := dimension * size
		if strings.HasPrefix(kind, "MAT") {
			columns := int64(kind[3] - '0')
			element = columns * ((columns*size + 3) / 4 * 4)
		}
		if count > remaining/element {
			return fmt.Errorf("glTF addressed accessor bytes outside budget")
		}
		remaining -= count * element
		offset, err := gltfIntField(accessor, "byteOffset", 0, math.MaxInt64, true)
		if err != nil || offset%size != 0 {
			return fmt.Errorf("invalid glTF accessor offset")
		}
		if _, exists := accessor["bufferView"]; exists {
			index, err := gltfIntField(accessor, "bufferView", 0, int64(len(a["bufferViews"]))-1, false)
			if err != nil {
				return err
			}
			view := a["bufferViews"][index]
			length := view["byteLength"].(int64)
			element := dimension * size
			if strings.HasPrefix(kind, "MAT") {
				columns := int64(kind[3] - '0')
				element = columns * ((columns*size + 3) / 4 * 4)
			}
			stride := element
			if v, ok := view["byteStride"].(int64); ok {
				stride = v
			}
			viewOffset, _ := view["byteOffset"].(int64)
			if stride < element || (viewOffset+offset)%size != 0 || offset > length || element > length-offset || count-1 > (length-offset-element)/stride {
				return fmt.Errorf("glTF accessor exceeds bufferView")
			}
		}
		if value, exists := accessor["sparse"]; exists {
			sparse, ok := value.(map[string]any)
			if !ok {
				return fmt.Errorf("invalid glTF sparse accessor")
			}
			sparseCount, err := gltfIntField(sparse, "count", 1, count, false)
			if err != nil {
				return err
			}
			for _, key := range []string{"indices", "values"} {
				part, ok := sparse[key].(map[string]any)
				if !ok {
					return fmt.Errorf("invalid glTF sparse part")
				}
				index, err := gltfIntField(part, "bufferView", 0, int64(len(a["bufferViews"]))-1, false)
				if err != nil {
					return err
				}
				partOffset, err := gltfIntField(part, "byteOffset", 0, math.MaxInt64, true)
				if err != nil {
					return err
				}
				view := a["bufferViews"][index]
				if _, hasStride := view["byteStride"]; hasStride {
					return fmt.Errorf("glTF sparse view cannot have stride")
				}
				element := dimension * size
				alignment := size
				if strings.HasPrefix(kind, "MAT") {
					columns := int64(kind[3] - '0')
					element = columns * ((columns*size + 3) / 4 * 4)
				}
				if key == "indices" {
					typeID, err := gltfIntField(part, "componentType", 5121, 5125, false)
					if err != nil {
						return err
					}
					element = map[int64]int64{5121: 1, 5123: 2, 5125: 4}[typeID]
					if element == 0 {
						return fmt.Errorf("invalid glTF sparse index type")
					}
					alignment = element
				}
				length := view["byteLength"].(int64)
				viewOffset, _ := view["byteOffset"].(int64)
				if partOffset%alignment != 0 || (viewOffset+partOffset)%alignment != 0 || partOffset > length || sparseCount > (length-partOffset)/element {
					return fmt.Errorf("glTF sparse data exceeds bufferView")
				}
			}
		}
	}
	return nil
}

// encoding/json replaces unpaired UTF-16 escapes. Reject them before names can
// alias a different physical resource; generic duplicate/UTF-8 checks stay shared.
func gltfValidStringEscapes(raw []byte) bool {
	inString := false
	for i := 0; i < len(raw); i++ {
		if raw[i] == '"' {
			inString = !inString
			continue
		}
		if !inString || raw[i] != '\\' {
			continue
		}
		i++
		if i >= len(raw) {
			return false
		}
		if raw[i] != 'u' {
			continue
		}
		if i+4 >= len(raw) {
			return false
		}
		n, err := strconv.ParseUint(string(raw[i+1:i+5]), 16, 16)
		if err != nil {
			return false
		}
		i += 4
		if n >= 0xdc00 && n <= 0xdfff {
			return false
		}
		if n >= 0xd800 && n <= 0xdbff {
			if i+6 >= len(raw) || raw[i+1] != '\\' || raw[i+2] != 'u' {
				return false
			}
			low, err := strconv.ParseUint(string(raw[i+3:i+7]), 16, 16)
			if err != nil || low < 0xdc00 || low > 0xdfff {
				return false
			}
			i += 6
		}
	}
	return !inString
}

func gltfScene(root map[string]any, a map[string][]map[string]any) error {
	for _, texture := range a["textures"] {
		if _, err := gltfIntField(texture, "source", 0, int64(len(a["images"]))-1, false); err != nil {
			return err
		}
		if _, exists := texture["sampler"]; exists {
			if _, err := gltfIntField(texture, "sampler", 0, int64(len(a["samplers"]))-1, false); err != nil {
				return err
			}
		}
	}
	for _, mesh := range a["meshes"] {
		primitives, err := gltfObjects(mesh, "primitives")
		if err != nil || len(primitives) == 0 {
			return fmt.Errorf("glTF mesh requires primitives")
		}
		for _, primitive := range primitives {
			attrs, ok := primitive["attributes"].(map[string]any)
			if !ok || len(attrs) == 0 {
				return fmt.Errorf("glTF primitive requires attributes")
			}
			for name := range attrs {
				if _, err := gltfIntField(attrs, name, 0, int64(len(a["accessors"]))-1, false); err != nil {
					return err
				}
			}
			if _, exists := attrs["POSITION"]; !exists {
				return fmt.Errorf("glTF primitive requires POSITION")
			}
			for key, max := range map[string]int64{"indices": int64(len(a["accessors"])) - 1, "material": int64(len(a["materials"])) - 1, "mode": 6} {
				if _, exists := primitive[key]; exists {
					if _, err := gltfIntField(primitive, key, 0, max, false); err != nil {
						return err
					}
				}
			}
		}
	}
	parents := make([]int, len(a["nodes"]))
	children := make([][]int, len(parents))
	for i, node := range a["nodes"] {
		for key, max := range map[string]int64{"mesh": int64(len(a["meshes"])) - 1, "camera": int64(len(a["cameras"])) - 1} {
			if _, exists := node[key]; exists {
				if _, err := gltfIntField(node, key, 0, max, false); err != nil {
					return err
				}
			}
		}
		_, matrix := node["matrix"]
		for key, length := range map[string]int{"matrix": 16, "translation": 3, "rotation": 4, "scale": 3} {
			if value, exists := node[key]; exists {
				if matrix && key != "matrix" {
					return fmt.Errorf("glTF node mixes matrix and TRS")
				}
				values, ok := value.([]any)
				if !ok || len(values) != length {
					return fmt.Errorf("invalid glTF node transform")
				}
				norm := 0.0
				for _, v := range values {
					number, ok := v.(json.Number)
					if !ok {
						return fmt.Errorf("invalid glTF transform number")
					}
					n, err := number.Float64()
					if err != nil || math.IsNaN(n) || math.IsInf(n, 0) {
						return fmt.Errorf("nonfinite glTF transform")
					}
					norm += n * n
				}
				if key == "rotation" && math.Abs(norm-1) > 1e-5 {
					return fmt.Errorf("invalid glTF quaternion")
				}
			}
		}
		if value, exists := node["children"]; exists {
			items, ok := value.([]any)
			if !ok {
				return fmt.Errorf("invalid glTF children")
			}
			seen := map[int]bool{}
			for index, value := range items {
				holder := map[string]any{"index": value}
				n, err := gltfIntField(holder, "index", 0, int64(len(parents))-1, false)
				if err != nil || seen[int(n)] || int(n) == i {
					return fmt.Errorf("invalid glTF child index")
				}
				seen[int(n)] = true
				parents[n]++
				if parents[n] > 1 {
					return fmt.Errorf("glTF node has multiple parents")
				}
				children[i] = append(children[i], int(n))
				items[index] = n
			}
		}
	}
	state := make([]int, len(parents))
	heights := make([]int, len(parents))
	var visit func(int, int) error
	visit = func(n, depth int) error {
		if state[n] == 1 || depth > 64 {
			return fmt.Errorf("glTF node cycle")
		}
		if state[n] == 2 {
			if depth+heights[n] > 64 {
				return fmt.Errorf("glTF scene depth outside budget")
			}
			return nil
		}
		state[n] = 1
		for _, c := range children[n] {
			if err := visit(c, depth+1); err != nil {
				return err
			}
			if heights[c]+1 > heights[n] {
				heights[n] = heights[c] + 1
			}
		}
		state[n] = 2
		return nil
	}
	for i := range parents {
		if err := visit(i, 0); err != nil {
			return err
		}
	}
	for _, scene := range a["scenes"] {
		if value, exists := scene["nodes"]; exists {
			items, ok := value.([]any)
			if !ok {
				return fmt.Errorf("invalid glTF scene nodes")
			}
			seen := map[int]bool{}
			for i, value := range items {
				holder := map[string]any{"index": value}
				n, err := gltfIntField(holder, "index", 0, int64(len(parents))-1, false)
				if err != nil || seen[int(n)] || parents[n] != 0 {
					return fmt.Errorf("invalid glTF scene root")
				}
				seen[int(n)] = true
				items[i] = n
			}
		}
	}
	if _, exists := root["scene"]; exists {
		if _, err := gltfIntField(root, "scene", 0, int64(len(a["scenes"]))-1, false); err != nil {
			return err
		}
	}
	return nil
}
