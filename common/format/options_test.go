package format

import "testing"

func TestHeaderModeFromOptionsStrictBoolean(t *testing.T) {
	for _, tc := range []struct {
		name    string
		options map[string]interface{}
		known   bool
		value   bool
		invalid bool
	}{
		{name: "absent"},
		{name: "other options", options: map[string]interface{}{"encoding": "gbk"}},
		{name: "task key is not an alias", options: map[string]interface{}{"header": false}},
		{name: "header", options: map[string]interface{}{"has_header": true}, known: true, value: true},
		{name: "data", options: map[string]interface{}{"has_header": false}, known: true},
		{name: "string", options: map[string]interface{}{"has_header": "false"}, invalid: true},
		{name: "number", options: map[string]interface{}{"has_header": 0}, invalid: true},
		{name: "null", options: map[string]interface{}{"has_header": nil}, invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			mode, err := HeaderModeFromOptions(tc.options)
			if (err != nil) != tc.invalid || (mode != nil) != tc.known {
				t.Fatalf("mode=%v error=%v", mode, err)
			}
			if mode != nil && *mode != tc.value {
				t.Fatalf("mode=%v, want %v", *mode, tc.value)
			}
		})
	}
}

func TestHeaderModeFromOptionsDoesNotAliasInput(t *testing.T) {
	options := map[string]interface{}{"has_header": true}
	mode, err := HeaderModeFromOptions(options)
	if err != nil || mode == nil {
		t.Fatal(err)
	}
	*mode = false
	if options["has_header"] != true {
		t.Fatal("changing the returned mode mutated the frozen/native input")
	}
}
