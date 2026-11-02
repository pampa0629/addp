package builtin_test

import (
	"encoding/hex"
	"testing"

	"github.com/addp/common/format"
	_ "github.com/addp/common/format/builtin"
)

func TestTIFFContentIdentityCannotBeClaimedAsHeaderLabelledRawData(t *testing.T) {
	for _, signature := range []string{"49492a00", "4d4d002a", "49492b0008000000", "4d4d002b00080000"} {
		data, err := hex.DecodeString(signature)
		if err != nil {
			t.Fatal(err)
		}
		if got := format.DetectFormat("", data); got != format.FormatTIFF {
			t.Fatalf("lost TIFF storage identity %s: %s", signature, got)
		}
	}
}
