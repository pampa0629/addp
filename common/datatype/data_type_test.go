package datatype

import "testing"

func TestParseDataType(t *testing.T) {
	tests := []struct {
		name string
		in   string
		want DataType
	}{
		{name: "normalizes", in: " Table ", want: Table},
		{name: "file is no longer a data type", in: "file", want: Unknown},
		{name: "unknown value", in: "dataset", want: Unknown},
		{name: "empty", in: "", want: Unknown},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ParseDataType(tt.in); got != tt.want {
				t.Fatalf("ParseDataType(%q) = %q, want %q", tt.in, got, tt.want)
			}
		})
	}

	if IsConcreteDataType(Unknown) {
		t.Fatalf("unknown should not be a concrete data type")
	}
}
