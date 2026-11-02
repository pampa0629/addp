package envi

import "testing"

func TestPairUsesExactNamesAndCompleteObservedSizes(t *testing.T) {
	header, err := ParseHeader(t.Context(), []byte(basicHeader))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"imagery/field.img", "imagery/field.dat", "imagery/field.bin", "imagery/field"} {
		for _, hdr := range []string{"imagery/field.HDR", name + ".hdr"} {
			pair, err := ResolvePair(hdr, header, []File{{name, 64}, {hdr, int64(len(basicHeader))}, {"imagery/unrelated.txt", 17}})
			if err != nil || pair.Raster.Path != name || pair.Header.Path != hdr {
				t.Fatalf("%s %s: %#v %v", name, hdr, pair, err)
			}
		}
	}
}

func TestPairRejectsAmbiguityDependenciesAndSourceDrift(t *testing.T) {
	header, err := ParseHeader(t.Context(), []byte(basicHeader))
	if err != nil {
		t.Fatal(err)
	}
	base := []File{{"imagery/field.img", 64}, {"imagery/field.hdr", int64(len(basicHeader))}}
	cases := map[string][]File{
		"missing data":             {base[1]},
		"missing header":           {base[0]},
		"truncated":                {{base[0].Path, 63}, base[1]},
		"unclaimed trailing bytes": {{base[0].Path, 65}, base[1]},
		"header size drift":        {base[0], {base[1].Path, base[1].Size + 1}},
		"two data":                 {base[0], base[1], {"imagery/field.bsq", 64}},
		"two headers":              {base[0], base[1], {"imagery/field.img.hdr", base[1].Size}},
		"case ambiguity":           {base[0], base[1], {"imagery/Field.HDR", base[1].Size}},
		"statistics":               {base[0], base[1], {"imagery/field.sta", 40}},
		"PAM":                      {base[0], base[1], {"imagery/field.img.aux.xml", 40}},
		"overview":                 {base[0], base[1], {"imagery/field.img.ovr", 40}},
		"duplicate observation":    {base[0], base[1], base[1]},
		"traversal":                {base[0], base[1], {"imagery/../secret", 40}},
		"URI":                      {base[0], base[1], {"https://secret/x", 40}},
	}
	for name, files := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ResolvePair(base[1].Path, header, files); err == nil {
				t.Fatal("incomplete composition accepted")
			}
		})
	}
}
