package skp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"testing"
)

func TestVFFSceneThumbnailUsesDeclaredMemberWithoutFlagOrNameInference(t *testing.T) {
	member := "scene_thumbnails/ 首%20图.png"
	image := vffAuthoredPNG(8, []byte{11, 22, 33, 0}, 1)
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
		p["flags"] = vffRecord("4971", []byte{0, 0, 0, 0})
		p["thumbnail"] = vffRecord("5871", vffRecord("3421", []byte(member)))
		c.members[member] = image
		c.members["scene_thumbnails/Scene _ 名称.png"] = []byte("name-derived decoy")
		c.members["scene_thumbnails/ 首 图.png"] = []byte("URI-decoded decoy")
	})
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil || len(got.scenes) != 1 || got.scenes[0].thumbnail == nil {
		t.Fatalf("declared thumbnail: %#v %v", got, err)
	}
	ref := got.scenes[0].thumbnail
	if ref.member != member || ref.sha256 != sha256.Sum256(image) || got.scenes[0].flags != 0 {
		t.Fatal("inferred thumbnail identity or ignored disabled flags")
	}
	model := c.members["model.dat"]
	if !bytes.Equal(model[ref.fieldOffset:ref.fieldOffset+2], []byte{0x58, 0x71}) || !bytes.Equal(model[ref.pathOffset:ref.pathOffset+2], []byte{0x34, 0x21}) {
		t.Fatal("thumbnail declaration offsets")
	}
	facts, err := readVFFImages(context.Background(), c, []vffImageReference{ref.vffImageReference}, vffImageTestLimits())
	if err != nil || len(facts) != 1 || facts[0].minAlpha != 0 || facts[0].pixelsSHA256 != sha256.Sum256([]byte{11, 11, 22, 22, 33, 33, 0, 0}) {
		t.Fatalf("separate image proof: %#v %v", facts, err)
	}
	delete(c.members, member)
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
		t.Fatal("must not use name-derived or URI-decoded decoys")
	}
	c, catalog = vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
		c.members["scene_thumbnails/Scene _ 名称.png"] = image
	})
	got, err = readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil || got.scenes[0].thumbnail != nil {
		t.Fatal("an undeclared member must not become a scene reference")
	}
}

func TestVFFSceneThumbnailRefusalsAndIndependentImageValidation(t *testing.T) {
	for name, field := range map[string][]byte{
		"empty-holder":   vffRecord("5871", nil),
		"unknown-value":  vffRecord("5871", vffRecord("3521", []byte("a.png"))),
		"empty-path":     vffRecord("5871", vffRecord("3421", nil)),
		"duplicate-path": vffRecord("5871", vffRecord("3421", []byte("a.png")), vffRecord("3421", []byte("a.png"))),
		"duplicate-holder": append(vffRecord("5871", vffRecord("3421", []byte("a.png"))),
			vffRecord("5871", vffRecord("3421", []byte("a.png")))...),
		"missing-member": vffRecord("5871", vffRecord("3421", []byte("missing.png"))),
		"self":           vffRecord("5871", vffRecord("3421", []byte("model.dat"))),
		"absolute":       vffRecord("5871", vffRecord("3421", []byte("/a.png"))),
		"traversal":      vffRecord("5871", vffRecord("3421", []byte("../a.png"))),
		"dot":            vffRecord("5871", vffRecord("3421", []byte("./a.png"))),
		"network":        vffRecord("5871", vffRecord("3421", []byte("https://example.test/a.png"))),
		"backslash":      vffRecord("5871", vffRecord("3421", []byte("dir\\a.png"))),
		"bad-utf8":       vffRecord("5871", vffRecord("3421", []byte{255})),
		"truncated":      vffRecord("5871", []byte{0x34, 0x21, 255, 255, 255, 255}),
	} {
		t.Run(name, func(t *testing.T) {
			c, catalog := vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
				p["thumbnail"] = field
				c.members["a.png"] = []byte("present")
			})
			if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
				t.Fatal("invalid partial reference facts")
			}
		})
	}
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
		p["thumbnail"] = vffRecord("5871", vffRecord("3421", []byte("not-an-image.png")))
		c.members["not-an-image.png"] = []byte("exact path is not proof of valid image")
	})
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if facts, err := readVFFImages(context.Background(), c, []vffImageReference{got.scenes[0].thumbnail.vffImageReference}, vffImageTestLimits()); err == nil || facts != nil {
		t.Fatal("reference success must not imply image validity")
	}
}

func TestVFFSceneThumbnailBudgetIncludesEachDistinctTargetOnce(t *testing.T) {
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, c *vffContainer) {
		c.members["a.png"], c.members["b.png"] = []byte("aaa"), []byte("bbbbb")
		var scenes []byte
		for i, target := range []string{"a.png", "b.png", "a.png"} {
			scenes = append(scenes, vffRecord("4871", vffRecord("546F", vffEntity(byte(10+i)), p["name"]), p["flags"], p["reference"], vffRecord("5871", vffRecord("3421", []byte(target))))...)
		}
		p["list"] = vffRecord("616D", scenes)
	})
	limits := vffSceneStyleTestLimits()
	limits.bytes = len(c.members["model.dat"]) + len(c.members["styles/Scene/style.xml"]) + len(c.members["a.png"]) + len(c.members["b.png"])
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, limits)
	if err != nil || len(got.scenes) != 3 || got.scenes[0].thumbnail == got.scenes[2].thumbnail {
		t.Fatalf("shared target has separate declarations: %#v %v", got, err)
	}
	limits.bytes--
	if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, limits); err == nil || got != nil {
		t.Fatal("all distinct image and XML bytes must fit")
	}
}

func TestVFFSceneRenderingKeepsAllValuesOpaqueRegardlessOfFlags(t *testing.T) {
	c, catalog := vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) {
		p["flags"] = vffRecord("4971", []byte{0, 0, 0, 0})
		p["render"] = vffRecord("4D71", vffRecord("3C73", vffRecord("3D73", []byte{2, 0, 0, 0}), vffRecord("FEFF", []byte("https://unproved.test/image.png"))))
	})
	got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil || got.scenes[0].rendering == nil || len(got.scenes[0].rendering.values) != 2 {
		t.Fatalf("opaque values: %#v %v", got, err)
	}
	for _, v := range got.scenes[0].rendering.values {
		if sha256.Sum256(c.members["model.dat"][v.offset+6:v.offset+6+v.bytes]) != v.sha256 {
			t.Fatal("rendering bytes lost")
		}
	}
	c, catalog = vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) { p["render"] = vffRecord("4D71", vffRecord("3C73", nil)) })
	got, err = readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits())
	if err != nil || got.scenes[0].rendering == nil || len(got.scenes[0].rendering.values) != 0 {
		t.Fatal("explicit empty rendering is distinct from absent")
	}
	for name, render := range map[string][]byte{
		"no-wrapper":        vffRecord("4D71", nil),
		"unknown-wrapper":   vffRecord("4D71", vffRecord("3D73", nil)),
		"duplicate-wrapper": vffRecord("4D71", vffRecord("3C73", nil), vffRecord("3C73", nil)),
		"duplicate-value":   vffRecord("4D71", vffRecord("3C73", vffRecord("3D73", nil), vffRecord("3D73", nil))),
		"truncated-value":   vffRecord("4D71", vffRecord("3C73", []byte{0x3d, 0x73, 255, 255, 255, 255})),
		"duplicate-holder":  append(vffRecord("4D71", vffRecord("3C73", nil)), vffRecord("4D71", vffRecord("3C73", nil))...),
	} {
		t.Run(name, func(t *testing.T) {
			c, catalog := vffSceneStyleControl(func(p map[string][]byte, _ *vffContainer) { p["render"] = render })
			if got, err := readVFFSceneStyleCandidates(context.Background(), c, catalog, vffSceneStyleTestLimits()); err == nil || got != nil {
				t.Fatal("invalid render framing must not return facts")
			}
		})
	}
}
