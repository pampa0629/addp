package skp

// Private XML declaration facts only. Neither image decoding nor model ID
// binding nor ownership of non-material members is proved by this layer.

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"math"
	"path"
	"sort"
	"strconv"
	"strings"
)

const vffMaterialNS = "http://sketchup.google.com/schemas/sketchup/1.0/material"

type vffMaterialFact struct {
	member, name, image string
	imageSHA256         [32]byte
	xmlSHA256           [32]byte
}

func readVFFMaterials(ctx context.Context, container *vffContainer, limits vffXMLLimits) ([]vffMaterialFact, error) {
	if container == nil || limits.bytes <= 0 || limits.nodes <= 0 || limits.depth <= 0 || limits.attributes <= 0 || limits.text <= 0 {
		return nil, fmt.Errorf("VFF material declarations require explicit positive XML budgets")
	}
	budget := &vffXMLBudget{limits: limits}
	var members []string
	for name := range container.members {
		if strings.HasPrefix(name, "materials/") && strings.HasSuffix(name, "material.xml") {
			members = append(members, name)
		}
	}
	sort.Strings(members)
	facts := make([]vffMaterialFact, 0, len(members))
	names := map[string]bool{}
	for _, member := range members {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		parts := strings.Split(member, "/")
		if len(parts) != 3 || parts[2] != "material.xml" {
			return nil, fmt.Errorf("unsupported VFF material member layout")
		}
		root, err := readVFFXML(ctx, container.members[member], budget, map[string]bool{vffMaterialNS: true})
		if err != nil {
			return nil, err
		}
		fact, err := vffMaterialDeclaration(root, member, container.members)
		if err != nil {
			return nil, err
		}
		if names[fact.name] {
			return nil, fmt.Errorf("duplicate VFF material name")
		}
		names[fact.name] = true
		facts = append(facts, fact)
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return facts, nil
}

func vffXMLShape(n *vffXMLNode, tag, attributes, children string, numericText bool) error {
	if n.name.Local != tag {
		return fmt.Errorf("unexpected VFF material node")
	}
	allowed := strings.Fields(attributes)
	for key := range n.attrs {
		found := false
		for _, a := range allowed {
			if a == key {
				found = true
				break
			}
		}
		if !found {
			return fmt.Errorf("unknown VFF material attribute")
		}
	}
	if !numericText && strings.TrimSpace(n.text) != "" {
		return fmt.Errorf("unsupported VFF material mixed content")
	}
	counts := map[string]bool{}
	for _, c := range n.children {
		found := false
		for _, name := range strings.Fields(children) {
			if c.name.Local == name {
				found = true
				break
			}
		}
		if !found || counts[c.name.Local] {
			return fmt.Errorf("unknown or duplicate VFF material child")
		}
		counts[c.name.Local] = true
	}
	return nil
}
func (n *vffXMLNode) value(key, fallback string) string {
	if v, ok := n.attrs[key]; ok {
		return v
	}
	return fallback
}
func (n *vffXMLNode) child(tag string) *vffXMLNode {
	for _, c := range n.children {
		if c.name.Local == tag {
			return c
		}
	}
	return nil
}
func vffNumber(value string) (float64, error) {
	x, err := strconv.ParseFloat(value, 64)
	if err != nil || math.IsNaN(x) || math.IsInf(x, 0) {
		return 0, fmt.Errorf("invalid VFF material number")
	}
	return x, nil
}
func vffMaterialDeclaration(root *vffXMLNode, member string, members map[string][]byte) (vffMaterialFact, error) {
	fail := func(err error) (vffMaterialFact, error) { return vffMaterialFact{}, err }
	if err := vffXMLShape(root, "materialDocument", "", "material", false); err != nil {
		return fail(err)
	}
	if len(root.children) != 1 {
		return fail(fmt.Errorf("VFF requires one material declaration"))
	}
	m := root.children[0]
	if err := vffXMLShape(m, "material", "name type workflow colorRed colorGreen colorBlue colorizeType trans useTrans pbrPromoState hasTexture", "texture pbrMR", false); err != nil {
		return fail(err)
	}
	if m.value("name", "") == "" {
		return fail(fmt.Errorf("missing VFF material name"))
	}
	for _, key := range []string{"workflow", "colorizeType", "pbrPromoState"} {
		if m.value(key, "0") != "0" {
			return fail(fmt.Errorf("unsupported VFF material mode"))
		}
	}
	for _, key := range []string{"colorRed", "colorGreen", "colorBlue"} {
		v, e := strconv.Atoi(m.value(key, "128"))
		if e != nil || v < 0 || v > 255 {
			return fail(fmt.Errorf("invalid VFF material color"))
		}
	}
	trans, e := vffNumber(m.value("trans", "0"))
	if e != nil || trans < 0 || trans > 1 || m.value("useTrans", "0") != "0" && m.value("useTrans", "0") != "1" {
		return fail(fmt.Errorf("invalid VFF material transparency"))
	}
	texture := m.child("texture")
	kind, flag := "0", "0"
	if texture != nil {
		kind, flag = "1", "1"
	}
	if m.value("type", "0") != kind || m.value("hasTexture", flag) != flag {
		return fail(fmt.Errorf("VFF material texture flags disagree"))
	}
	if pbr := m.child("pbrMR"); pbr != nil {
		flags := []string{"enable_metalness", "enable_roughness", "enable_normal", "enable_occlusion"}
		defaults := map[string]float64{"roughness_texture_invert": 0, "metallicFactor": 1, "roughnessFactor": 1, "normalMapStyle": 1, "normalScale": 1, "occlusionStrength": 1, "baseColorFactor": 4294967295}
		known := strings.Join(flags, " ")
		for key := range defaults {
			known += " " + key
		}
		if err := vffXMLShape(pbr, "pbrMR", "xScale yScale", known, false); err != nil {
			return fail(err)
		}
		for _, value := range pbr.attrs {
			if _, err := vffNumber(value); err != nil {
				return fail(err)
			}
		}
		for _, key := range flags {
			n := pbr.child(key)
			if n == nil || strings.TrimSpace(n.text) != "0" {
				return fail(fmt.Errorf("unproven VFF PBR activation"))
			}
		}
		for _, n := range pbr.children {
			if err := vffXMLShape(n, n.name.Local, "", "", true); err != nil {
				return fail(err)
			}
			if expected, ok := defaults[n.name.Local]; ok {
				v, err := vffNumber(strings.TrimSpace(n.text))
				if err != nil || v != expected {
					return fail(fmt.Errorf("unsupported VFF PBR value"))
				}
			}
		}
	}
	fact := vffMaterialFact{member: member, name: m.attrs["name"], xmlSHA256: sha256.Sum256(members[member])}
	if texture == nil {
		return fact, nil
	}
	if err := vffXMLShape(texture, "texture", "textureFilename xScale yScale avgColor", "images", false); err != nil {
		return fail(err)
	}
	for _, key := range []string{"xScale", "yScale"} {
		v, err := vffNumber(texture.value(key, "0"))
		if err != nil || v < 0 {
			return fail(fmt.Errorf("invalid VFF texture scale"))
		}
	}
	reference := texture.value("textureFilename", "")
	if images := texture.child("images"); images != nil {
		if err := vffXMLShape(images, "images", "", "image", false); err != nil {
			return fail(err)
		}
		if len(images.children) != 1 {
			return fail(fmt.Errorf("VFF requires one image declaration"))
		}
		image := images.children[0]
		if err := vffXMLShape(image, "image", "id path file_name", "", false); err != nil {
			return fail(err)
		}
		reference = image.value("path", "")
	}
	reference = strings.TrimPrefix(reference, "./")
	if err := vffMemberName([]byte(reference), 0x800); err != nil {
		return fail(err)
	}
	candidate := reference
	if !strings.HasPrefix(reference, "materials/") {
		candidate = path.Dir(member) + "/" + reference
	}
	data, ok := members[candidate]
	if !ok {
		return fail(fmt.Errorf("missing exact VFF embedded image"))
	}
	if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) && !bytes.HasPrefix(data, []byte("\xff\xd8\xff")) {
		return fail(fmt.Errorf("unsupported VFF image declaration MIME"))
	}
	fact.image = candidate
	fact.imageSHA256 = sha256.Sum256(data)
	return fact, nil
}
