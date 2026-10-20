package jsonmap

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestDecodeObjectStrictPreservesValuesAndRejectsAmbiguity(t *testing.T) {
	value, err := DecodeObjectStrict([]byte(`{"n":9007199254740993,"refs":[{"name":" Case "}],"empty":null}`), 1024, 8)
	if err != nil || value["n"] != json.Number("9007199254740993") || value["empty"] != nil {
		t.Fatal("JSON values changed", err)
	}
	for _, raw := range []string{"", "null", "[]", "1", `{} {}`, `{"x":1,"x":1}`, `{"refs":[{"x":1,"x":2}]}`, `{"x":` + strings.Repeat("[", 10) + `0` + strings.Repeat("]", 10) + `}`, "{\"x\":\"\xff\"}"} {
		if _, err := DecodeObjectStrict([]byte(raw), 1024, 8); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
	for _, limits := range [][2]int{{1, 8}, {1024, 0}, {0, 8}} {
		if _, err := DecodeObjectStrict([]byte(`{}`), limits[0], limits[1]); err == nil {
			t.Fatal("invalid JSON budget accepted")
		}
	}
}
