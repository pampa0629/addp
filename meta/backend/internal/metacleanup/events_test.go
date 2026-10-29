package metacleanup

import "testing"

func TestToMap(t *testing.T) {
	t.Parallel()

	got := ToMap(struct {
		Name string `json:"name"`
		Size int    `json:"size"`
	}{Name: "roads", Size: 3})

	if got["name"] != "roads" || got["size"].(float64) != 3 {
		t.Fatalf("map = %#v", got)
	}
}

func TestScopeFromContextReadsEngineID(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		ctx  map[string]interface{}
		want uint
	}{
		{name: "uint", ctx: map[string]interface{}{"engine_id": uint(7)}, want: 7},
		{name: "float", ctx: map[string]interface{}{"engine_id": float64(8)}, want: 8},
		{name: "string", ctx: map[string]interface{}{"engine_id": "9"}, want: 9},
		{name: "missing", ctx: map[string]interface{}{}, want: 0},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			if got := ScopeFromContext(tt.ctx).EngineID; got != tt.want {
				t.Fatalf("EngineID = %d, want %d", got, tt.want)
			}
		})
	}
}
