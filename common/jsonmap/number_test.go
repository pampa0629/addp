package jsonmap

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
)

func TestLosslessJSONNumberSnapshots(t *testing.T) {
	if InterfaceInt64(json.Number("9007199254740993")) != 9007199254740993 {
		t.Fatal("snapshot int64 rounded through float")
	}
	for _, value := range []string{"1.5", "9223372036854775808", "NaN"} {
		if InterfaceInt64(json.Number(value)) != 0 {
			t.Fatal("invalid integer accepted", value)
		}
	}
	if got := InterfaceFloat64Slice([]any{json.Number("113.1"), json.Number("28"), json.Number("114"), json.Number("29")}); !reflect.DeepEqual(got, []float64{113.1, 28, 114, 29}) {
		t.Fatal("snapshot extent disappeared", got)
	}
	if got := InterfaceFloat64Slice([]any{json.Number("1e9999")}); got != nil || (len(got) > 0 && math.IsInf(got[0], 0)) {
		t.Fatal("unbounded coordinate accepted")
	}
}
