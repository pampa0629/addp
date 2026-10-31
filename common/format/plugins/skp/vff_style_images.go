package skp

import (
	"context"
	"crypto/sha256"
	"fmt"
	"path"
	"strconv"
	"strings"
)

const (
	vffStyleNS     = "http://sketchup.google.com/schemas/sketchup/1.0/style"
	vffTypesNS     = "http://sketchup.google.com/schemas/1.0/types"
	vffWatermarkNS = "http://sketchup.google.com/schemas/sketchup/1.0/wmlist"
)

type vffStyleImageReference struct {
	styleMember, reference, imageID string
	offset                          int // image XML start, not a TLV header
	vffImageReference
}
type vffXMLRange struct {
	member, location string
	begin, end       int
	sha256           [32]byte
}
type vffStyleImageFacts struct {
	images   []vffStyleImageReference
	unproved []vffXMLRange
}

func readVFFStyleImages(ctx context.Context, c *vffContainer, catalog *vffStyleFacts, limits vffXMLLimits, referenceBytes int) (*vffStyleImageFacts, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c == nil || catalog == nil || len(catalog.entries) == 0 || referenceBytes <= 0 || limits.bytes <= 0 || limits.nodes <= 0 || limits.depth <= 0 || limits.attributes <= 0 || limits.text <= 0 {
		return nil, fmt.Errorf("VFF style images require current catalog facts and explicit positive budgets")
	}
	model := c.members["model.dat"]
	if len(model) == 0 || len(model) > referenceBytes || sha256.Sum256(model) != catalog.modelSHA256 {
		return nil, fmt.Errorf("VFF style model facts are stale or exceed budget")
	}
	budget := &vffReferenceBudget{container: c, used: len(model), limit: referenceBytes, digests: map[string][32]byte{}}
	xmlBudget := &vffXMLBudget{limits: limits}
	seen := map[string]bool{}
	result := &vffStyleImageFacts{}
	for _, entry := range catalog.entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if err := vffMemberName([]byte(entry.folder), 0x800); err != nil {
			return nil, err
		}
		if strings.Contains(entry.folder, "/") || entry.member != "styles/"+entry.folder+"/style.xml" || seen[entry.member] {
			return nil, fmt.Errorf("ambiguous VFF style member facts")
		}
		seen[entry.member] = true
		digest, err := budget.digest(entry.member)
		if err != nil {
			return nil, err
		}
		if digest != entry.xmlSHA256 {
			return nil, fmt.Errorf("VFF style XML facts are stale")
		}
		raw := c.members[entry.member]
		root, err := readVFFXML(ctx, raw, xmlBudget, map[string]bool{vffStyleNS: true, vffTypesNS: true, vffWatermarkNS: true}, nil)
		if err != nil {
			return nil, err
		}
		if err = styleXMLNode(root, vffStyleNS, "styleDocument", ""); err != nil {
			return nil, err
		}
		if len(root.children) != 1 {
			return nil, fmt.Errorf("VFF style requires one style node")
		}
		style := root.children[0]
		if err = styleXMLNode(style, vffStyleNS, "style", "name desc"); err != nil {
			return nil, err
		}
		if style.attrs["name"] == "" {
			return nil, fmt.Errorf("missing VFF XML style name")
		}
		ids := map[string]bool{}
		watermark := false
		for _, item := range style.children {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if err = styleXMLNode(item, vffStyleNS, "item", "id"); err != nil {
				return nil, err
			}
			if _, err = styleXMLUint(item.attrs["id"]); err != nil {
				return nil, err
			}
			if ids[item.attrs["id"]] {
				return nil, fmt.Errorf("duplicate VFF style item ID")
			}
			ids[item.attrs["id"]] = true
			if item.attrs["id"] != "5001" {
				result.retain(entry.member, "style/item/"+item.attrs["id"], raw, item)
				continue
			}
			watermark = true
			if len(item.children) != 1 {
				return nil, fmt.Errorf("VFF watermark item requires one variant")
			}
			variant := item.children[0]
			if err = styleXMLNode(variant, vffTypesNS, "variant", "type"); err != nil {
				return nil, err
			}
			if variant.attrs["type"] != "13" || len(variant.children) != 1 {
				return nil, fmt.Errorf("unsupported VFF watermark XML variant")
			}
			if err = result.watermarkXML(ctx, variant.children[0], entry.member, raw, budget); err != nil {
				return nil, err
			}
		}
		if !watermark {
			return nil, fmt.Errorf("missing explicit VFF style watermark list")
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return result, nil
}

func styleXMLNode(n *vffXMLNode, ns, name, attributes string) error {
	if n.name.Space != ns || n.name.Local != name || strings.TrimSpace(n.text) != "" {
		return fmt.Errorf("unsupported VFF style XML node or mixed content")
	}
	for key := range n.attrs {
		allowed := false
		for _, a := range strings.Fields(attributes) {
			if key == a {
				allowed = true
				break
			}
		}
		if !allowed {
			return fmt.Errorf("unsupported VFF style XML attribute")
		}
	}
	return nil
}
func styleXMLUint(value string) (uint64, error) {
	n, err := strconv.ParseUint(value, 10, 64)
	if err != nil || strconv.FormatUint(n, 10) != value {
		return 0, fmt.Errorf("invalid VFF style XML integer")
	}
	return n, nil
}
func (r *vffStyleImageFacts) retain(member, location string, raw []byte, node *vffXMLNode) {
	r.unproved = append(r.unproved, vffXMLRange{member: member, location: location, begin: node.begin, end: node.end, sha256: sha256.Sum256(raw[node.begin:node.end])})
}
func (r *vffStyleImageFacts) watermarkXML(ctx context.Context, list *vffXMLNode, member string, raw []byte, budget *vffReferenceBudget) error {
	if err := styleXMLNode(list, vffWatermarkNS, "wmlist", "count"); err != nil {
		return err
	}
	count, err := styleXMLUint(list.attrs["count"])
	if err != nil {
		return err
	}
	if len(list.children) == 0 {
		if count != 0 {
			return fmt.Errorf("incomplete VFF watermark list")
		}
		return nil
	}
	if len(list.children) != 1 {
		return fmt.Errorf("duplicate VFF screenimages wrapper")
	}
	screens := list.children[0]
	if err = styleXMLNode(screens, vffWatermarkNS, "screenimages", ""); err != nil {
		return err
	}
	if count != uint64(len(screens.children)) {
		return fmt.Errorf("VFF watermark XML count differs")
	}
	separator := false
	for _, screen := range screens.children {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err = styleXMLNode(screen, vffWatermarkNS, "screenimage", "name info_found info_filename info_time position tiled stretched maintainAR fittingType stretchType background intensForAlpha alphaScale scale"); err != nil {
			return err
		}
		if screen.attrs["name"] == "" {
			return fmt.Errorf("missing VFF screenimage name")
		}
		if screen.attrs["name"] == "<MODEL SPACE>" {
			if separator || len(screen.attrs) != 1 || len(screen.children) != 0 {
				return fmt.Errorf("unsupported VFF watermark separator")
			}
			separator = true
			continue
		}
		// Retain all settings verbatim; this layer does not interpret rendering.
		r.retain(member, "style/item/5001/screenimage", raw, screen)
		if len(screen.children) != 1 {
			return fmt.Errorf("VFF watermark requires one images wrapper")
		}
		images := screen.children[0]
		if err = styleXMLNode(images, vffWatermarkNS, "images", ""); err != nil {
			return err
		}
		if len(images.children) != 1 {
			return fmt.Errorf("unsupported VFF watermark image alternatives")
		}
		image := images.children[0]
		if err = styleXMLNode(image, vffWatermarkNS, "image", "id path file_name"); err != nil {
			return err
		}
		if len(image.children) != 0 {
			return fmt.Errorf("unexpected VFF watermark image children")
		}
		if id, err := styleXMLUint(image.attrs["id"]); err != nil || id == 0 {
			return fmt.Errorf("invalid VFF watermark image ID")
		}
		reference := image.attrs["path"]
		target := reference
		if strings.HasPrefix(reference, "./") {
			relative := strings.TrimPrefix(reference, "./")
			if err = vffMemberName([]byte(relative), 0x800); err != nil {
				return err
			}
			target = path.Dir(member) + "/" + relative
		} else if err = vffMemberName([]byte(reference), 0x800); err != nil {
			return err
		}
		if target == member || target == "model.dat" {
			return fmt.Errorf("VFF style image aliases its XML input")
		}
		digest, err := budget.digest(target)
		if err != nil {
			return err
		}
		r.images = append(r.images, vffStyleImageReference{styleMember: member, reference: reference, imageID: image.attrs["id"], offset: image.begin, vffImageReference: vffImageReference{member: target, sha256: digest}})
	}
	return nil
}
