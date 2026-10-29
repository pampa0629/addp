package skp

import (
	"context"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"testing"
)

func vffRecord(tag string, raw ...[]byte) []byte {
	b, _ := hex.DecodeString(tag)
	size := 0
	for _, v := range raw {
		size += len(v)
	}
	out := make([]byte, 6, 6+size)
	copy(out, b)
	binary.LittleEndian.PutUint32(out[2:], uint32(size))
	for _, v := range raw {
		out = append(out, v...)
	}
	return out
}
func vffEntity(id byte) []byte { return vffRecord("DC05", vffRecord("DE05", []byte{id})) }
func vffRegistration(id byte, token string, flags ...byte) []byte {
	flag := byte(0)
	if len(flags) != 0 {
		flag = flags[0]
	}
	return vffRecord("C832", vffEntity(id), vffRecord("CC32", []byte(token)), vffRecord("CA32", []byte{flag}), vffRecord("CB32", nil))
}
func vffBindingTestLimits() vffBindingLimits {
	return vffBindingLimits{bytes: 1 << 20, records: 10000, references: 1000, depth: 32}
}
func vffBindingControl(t *testing.T, mutate func(map[string][]byte, *vffContainer)) (*vffContainer, []vffMaterialFact) {
	t.Helper()
	c, _ := vffMaterialControl()
	c.members["materials/LayerOnly/material.xml"] = []byte(`<materialDocument xmlns="` + vffMaterialNS + `"><material name="LayerOnly"/></materialDocument>`)
	p := map[string][]byte{
		"registration":        vffRegistration(1, "Front"),
		"layer-registration":  vffRegistration(2, "LayerOnly", 1),
		"layer-id":            vffEntity(10),
		"face-properties":     vffRecord("D007", vffRecord("D107", []byte{1})),
		"back":                vffRecord("AF0D", []byte{1}),
		"edge-properties":     vffRecord("D007", vffRecord("D107", []byte{1}), vffRecord("D207", []byte{10})),
		"instance-properties": vffRecord("D007", vffRecord("D107", []byte{1})),
		"opaque":              vffRecord("0902", vffRegistration(99, "UnprovedInsideAttribute")),
	}
	if mutate != nil {
		mutate(p, c)
	}
	c.members["model.dat"] = vffRecord("F401",
		vffRecord("F701", vffRecord("D430", vffRecord("D530", p["registration"]))),
		vffRecord("F801", vffRecord("983A", vffRecord("993A", vffRecord("8C3C", p["layer-id"], vffRecord("8D3C", []byte("Layer0")), vffRecord("8F3C", p["layer-registration"]))))),
		vffRecord("F601", vffRecord("8813", vffRecord("8B13", vffRecord("AC0D", p["face-properties"], p["back"])), vffRecord("8A13", vffRecord("B80B", p["edge-properties"])), vffRecord("8C13", vffRecord("6419", p["instance-properties"])))), p["opaque"])
	mats, err := readVFFMaterials(context.Background(), c, vffMaterialLimits())
	if err != nil {
		t.Fatal(err)
	}
	return c, mats
}
func TestVFFKnownMaterialRolesBindDeclarationsAndRetainOpaqueRanges(t *testing.T) {
	c, mats := vffBindingControl(t, nil)
	got, err := readVFFMaterialBindings(context.Background(), c, mats, vffBindingTestLimits())
	if err != nil {
		t.Fatal(err)
	}
	if len(got.materials) != 2 || got.materials[1].member != "materials/Front/material.xml" || got.materials[2].role != "layer" || got.layers[10] != 2 || len(got.references) != 5 {
		t.Fatalf("binding facts: %#v", got)
	}
	if len(got.opaque) != 1 || got.opaque[0].location != "F401/0902" || got.opaque[0].bytes == 0 {
		t.Fatal("unproved model content disappeared")
	}
	if _, ok := got.materials[99]; ok {
		t.Fatal("searched for tags inside unproved payload")
	}
}
func TestVFFBindingRefusalsAreAtomic(t *testing.T) {
	cases := map[string]func(map[string][]byte, *vffContainer){
		"cross-role-duplicate-id": func(p map[string][]byte, c *vffContainer) {
			p["layer-registration"] = vffRegistration(1, "LayerOnly", 1)
		},
		"duplicate-ordinary-id": func(p map[string][]byte, c *vffContainer) {
			p["registration"] = append(p["registration"], vffRegistration(1, "Front")...)
		},
		"edge-dangling": func(p map[string][]byte, c *vffContainer) {
			p["edge-properties"] = vffRecord("D007", vffRecord("D107", []byte{77}))
		},
		"face-dangling": func(p map[string][]byte, c *vffContainer) {
			p["face-properties"] = vffRecord("D007", vffRecord("D107", []byte{77}))
		},
		"instance-dangling": func(p map[string][]byte, c *vffContainer) {
			p["instance-properties"] = vffRecord("D007", vffRecord("D107", []byte{77}))
		},
		"back-dangling":                func(p map[string][]byte, c *vffContainer) { p["back"] = vffRecord("AF0D", []byte{77}) },
		"layer-owned-material-on-face": func(p map[string][]byte, c *vffContainer) { p["back"] = vffRecord("AF0D", []byte{2}) },
		"wrong-registration-role":      func(p map[string][]byte, c *vffContainer) { p["registration"] = vffRegistration(1, "Front", 1) },
		"reference-at-layer-role": func(p map[string][]byte, c *vffContainer) {
			p["layer-id"] = append(p["layer-id"], vffRecord("D107", []byte{1})...)
		},
		"duplicate-front-reference": func(p map[string][]byte, c *vffContainer) {
			p["face-properties"] = vffRecord("D007", vffRecord("D107", []byte{1}), vffRecord("D107", []byte{1}))
		},
		"duplicate-back-reference": func(p map[string][]byte, c *vffContainer) { p["back"] = append(p["back"], p["back"]...) },
		"duplicate-property-wrapper": func(p map[string][]byte, c *vffContainer) {
			p["face-properties"] = append(p["face-properties"], p["face-properties"]...)
		},
		"layer-dangling": func(p map[string][]byte, c *vffContainer) {
			p["edge-properties"] = vffRecord("D007", vffRecord("D207", []byte{77}))
		},
		"unbound-xml": func(p map[string][]byte, c *vffContainer) {
			c.members["materials/Unused/material.xml"] = []byte(`<materialDocument xmlns="` + vffMaterialNS + `"><material name="Unused"/></materialDocument>`)
		},
		"name-folder-collision": func(p map[string][]byte, c *vffContainer) {
			c.members["materials/LayerOnly/material.xml"] = []byte(`<materialDocument xmlns="` + vffMaterialNS + `"><material name="Front"/></materialDocument>`)
		},
		"unknown-registry-field": func(p map[string][]byte, c *vffContainer) {
			p["registration"] = vffRecord("C832", vffEntity(1), vffRecord("CC32", []byte("Front")), vffRecord("CA32", []byte{0}), vffRecord("CB32", nil), vffRecord("FF00", []byte("unknown")))
		},
		"duplicate-registry-field": func(p map[string][]byte, c *vffContainer) {
			p["registration"] = vffRecord("C832", vffEntity(1), vffRecord("CC32", []byte("Front")), vffRecord("CC32", []byte("Front")), vffRecord("CA32", []byte{0}), vffRecord("CB32", nil))
		},
		"nonzero-extension": func(p map[string][]byte, c *vffContainer) {
			p["registration"] = vffRecord("C832", vffEntity(1), vffRecord("CC32", []byte("Front")), vffRecord("CA32", []byte{0}), vffRecord("CB32", []byte{1}))
		},
		"unrecognized-registration-role": func(p map[string][]byte, c *vffContainer) { p["opaque"] = vffRegistration(3, "Front") },
		"empty-layer-material":           func(p map[string][]byte, c *vffContainer) { p["layer-registration"] = nil },
		"multiple-layer-materials": func(p map[string][]byte, c *vffContainer) {
			p["layer-registration"] = append(p["layer-registration"], vffRegistration(3, "Front")...)
		},
		"zero-reference":      func(p map[string][]byte, c *vffContainer) { p["back"] = vffRecord("AF0D", []byte{0}) },
		"oversized-reference": func(p map[string][]byte, c *vffContainer) { p["back"] = vffRecord("AF0D", make([]byte, 9)) },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			c, mats := vffBindingControl(t, mutate)
			got, err := readVFFMaterialBindings(context.Background(), c, mats, vffBindingTestLimits())
			if err == nil || got != nil {
				t.Fatalf("expected atomic refusal: %#v %v", got, err)
			}
		})
	}
}
func TestVFFBindingSharedMaterialAndBudgets(t *testing.T) {
	c, mats := vffBindingControl(t, func(p map[string][]byte, c *vffContainer) {
		p["registration"] = append(p["registration"], vffRegistration(3, "Front")...)
	})
	got, err := readVFFMaterialBindings(context.Background(), c, mats, vffBindingTestLimits())
	if err != nil || len(got.materials) != 3 || got.materials[3].member != got.materials[1].member {
		t.Fatalf("valid shared material: %#v %v", got, err)
	}
	for name, change := range map[string]func(*vffBindingLimits){"bytes": func(l *vffBindingLimits) { l.bytes = 10 }, "records": func(l *vffBindingLimits) { l.records = 8 }, "references": func(l *vffBindingLimits) { l.references = 2 }, "depth": func(l *vffBindingLimits) { l.depth = 3 }} {
		t.Run(name, func(t *testing.T) {
			limits := vffBindingTestLimits()
			change(&limits)
			if v, e := readVFFMaterialBindings(context.Background(), c, mats, limits); e == nil || v != nil {
				t.Fatal("binding budget not enforced")
			}
		})
	}
	for _, extra := range []bool{false, true} {
		copyC := *c
		copyC.members = map[string][]byte{}
		for name, data := range c.members {
			copyC.members[name] = append([]byte(nil), data...)
		}
		if extra {
			copyC.members["model.dat"] = append(copyC.members["model.dat"], 0)
		} else {
			copyC.members["model.dat"] = copyC.members["model.dat"][:len(copyC.members["model.dat"])-1]
		}
		if v, e := readVFFMaterialBindings(context.Background(), &copyC, mats, vffBindingTestLimits()); e == nil || v != nil {
			t.Fatal("incomplete model range accepted")
		}
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	controlled := &vffMaterialCancelContext{Context: ctx, cancel: cancel, remaining: 12}
	if v, e := readVFFMaterialBindings(controlled, c, mats, vffBindingTestLimits()); v != nil || !errors.Is(e, context.Canceled) {
		t.Fatalf("cancellation lost: %#v %v", v, e)
	}
}

func TestVFFBindingDuplicateManagersAndLayerIdentities(t *testing.T) {
	for _, scenario := range []string{"manager", "layer"} {
		t.Run(scenario, func(t *testing.T) {
			c, mats := vffBindingControl(t, nil)
			if scenario == "manager" {
				extra := vffRecord("F701", vffRecord("D430", vffRecord("D530", vffRegistration(3, "Front"))))
				c.members["model.dat"] = vffRecord("F401", c.members["model.dat"][6:], extra)
			} else {
				layer := func(material byte) []byte {
					return vffRecord("8C3C", vffEntity(10), vffRecord("8D3C", []byte("Layer0")), vffRecord("8F3C", vffRegistration(material, "LayerOnly", 1)))
				}
				c.members["model.dat"] = vffRecord("F401", vffRecord("F701", vffRecord("D430", vffRecord("D530", vffRegistration(1, "Front")))), vffRecord("F801", vffRecord("983A", vffRecord("993A", layer(2), layer(3)))))
			}
			if v, e := readVFFMaterialBindings(context.Background(), c, mats, vffBindingTestLimits()); e == nil || v != nil {
				t.Fatal("ambiguous repeated manager or layer accepted")
			}
		})
	}
}

func TestVFFBindingRefusesStaleOrMissingXMLFacts(t *testing.T) {
	for _, scenario := range []string{"changed", "missing"} {
		t.Run(scenario, func(t *testing.T) {
			c, mats := vffBindingControl(t, nil)
			if scenario == "changed" {
				c.members[mats[0].member] = append(c.members[mats[0].member], []byte("<!-- changed source -->")...)
			} else {
				delete(c.members, mats[0].member)
			}
			if got, err := readVFFMaterialBindings(context.Background(), c, mats, vffBindingTestLimits()); err == nil || got != nil {
				t.Fatal("reused declaration facts from different source bytes")
			}
		})
	}
}
