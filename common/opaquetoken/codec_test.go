package opaquetoken

import (
	"encoding/json"
	"testing"
)

func TestCodecPurposeNamespaceAndExactJSON(t *testing.T) {
	codec := New([]byte("test-key"), "owner/v1")
	payload := map[string]any{"seq": uint64(18446744073709551615)}
	token, err := codec.Encode("cursor", payload)
	if err != nil {
		t.Fatal(err)
	}
	var decoded map[string]any
	if err := codec.Decode("cursor", token, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded["seq"] != json.Number("18446744073709551615") {
		t.Fatalf("lost precision: %v", decoded)
	}
	for _, tc := range []struct {
		codec          *Codec
		purpose, token string
	}{{codec, "feature", token}, {New([]byte("test-key"), "other/v1"), "cursor", token}, {New(nil, "owner/v1"), "cursor", token}, {codec, "cursor", "x" + token}, {codec, "cursor", "AA"}} {
		if err := tc.codec.Decode(tc.purpose, tc.token, &decoded); err == nil {
			t.Fatal("invalid token accepted")
		}
	}
	again, err := codec.Encode("cursor", payload)
	if err != nil || again == token {
		t.Fatal("nonce reused")
	}
	if _, err := New(nil, "owner/v1").Encode("cursor", payload); err == nil {
		t.Fatal("unconfigured codec accepted")
	}
}
