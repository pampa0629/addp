// Package conformance contains engine-independent analytical compiler test
// cases. Engines supply real SQL evaluation through their standard test gates.
package conformance

import "testing"

// LosslessInteger checks both halves of a compiled scalar: safe output and
// its error predicate. Invalid input must never become a successful rounded,
// truncated or clamped integer. NULL is valid and stays NULL.
func LosslessInteger(t *testing.T, evaluate func(*string) (*int64, bool, error)) {
	losslessInteger(t, evaluate, true)
}

func losslessInteger(t *testing.T, evaluate func(*string) (*int64, bool, error), checkValue bool) {
	t.Helper()
	cases := []struct {
		name    string
		input   string
		want    int64
		invalid bool
	}{
		{"zero", "0", 0, false},
		{"negative zero", "-0.000", 0, false},
		{"whole decimal", "2.000000000000000000", 2, false},
		{"negative whole decimal", "-2.0", -2, false},
		{"positive fraction", "2.7", 0, true},
		{"negative fraction", "-2.7", 0, true},
		{"smallest fraction", "0.000000000000000001", 0, true},
		{"nearly integral", "2.000000000000000001", 0, true},
		{"beyond float precision", "9007199254740993.0", 9007199254740993, false},
		{"maximum integer", "9223372036854775807.0", 9223372036854775807, false},
		{"minimum integer", "-9223372036854775808.0", -9223372036854775808, false},
		{"above maximum fraction", "9223372036854775807.000000000000000001", 0, true},
		{"below minimum fraction", "-9223372036854775808.000000000000000001", 0, true},
		{"positive overflow", "9223372036854775808", 0, true},
		{"negative overflow", "-9223372036854775809", 0, true},
		{"decimal maximum", "99999999999999999999.999999999999999999", 0, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			value, invalid, err := evaluate(&tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if invalid != tc.invalid {
				t.Fatalf("invalid = %v, want %v", invalid, tc.invalid)
			}
			if tc.invalid {
				if value != nil {
					t.Fatalf("invalid input produced integer %d", *value)
				}
			} else if checkValue && (value == nil || *value != tc.want) {
				t.Fatalf("integer = %v, want %d", value, tc.want)
			}
		})
	}
	t.Run("null", func(t *testing.T) {
		value, invalid, err := evaluate(nil)
		if err != nil || invalid || value != nil {
			t.Fatalf("NULL: value=%v invalid=%v err=%v", value, invalid, err)
		}
	})
}
