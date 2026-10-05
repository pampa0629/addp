package plugin

import (
	"bytes"
	"context"
	"strings"
	"testing"
)

type keyCapabilityOnly struct {
	MockPlugin
	enabled bool
}

func (p *keyCapabilityOnly) Capabilities() EngineCapabilities {
	c := p.MockPlugin.Capabilities()
	c.Storage = &StorageCapabilities{Store: &StoreCapability{KeyValueRead: p.enabled}}
	return c
}
func (p *keyCapabilityOnly) StoreSemantics() StoreSemantics {
	return StoreSemanticsFromCapabilities(p.Capabilities())
}

type keyCapabilityReader struct{ keyCapabilityOnly }

func (*keyCapabilityReader) ReadKeyValue(context.Context, ConnectionInfo, EngineCatalogPath, KeyValueReadOptions) (*KeyValuePreview, error) {
	return nil, nil
}

func (*keyCapabilityReader) ListKeyValues(context.Context, ConnectionInfo, EngineCatalogPath, KeyValueReadOptions) (*KeyValueDatasetPreview, error) {
	return nil, nil
}

func TestKeyValueCapabilityRequiresMatchingProvider(t *testing.T) {
	base := keyCapabilityOnly{MockPlugin: MockPlugin{TypeValue: "key_test"}, enabled: true}
	if err := ValidatePluginCapabilities(&base); err == nil {
		t.Fatal("declaration without provider accepted")
	}
	reader := keyCapabilityReader{base}
	if err := ValidatePluginCapabilities(&reader); err != nil {
		t.Fatal(err)
	}
	reader.enabled = false
	if err := ValidatePluginCapabilities(&reader); err == nil {
		t.Fatal("provider without declaration accepted")
	}
}

func TestKeyNameCanonicalBytes(t *testing.T) {
	for _, raw := range [][]byte{nil, []byte("a:b/c.d \n"), {0, 255, 128}, []byte("中文"), bytes.Repeat([]byte("a"), MaxKeyNameBytes)} {
		name, err := EncodeKeyName(raw)
		if err != nil {
			t.Fatal(err)
		}
		decoded, err := DecodeKeyName(name)
		if err != nil || !bytes.Equal(raw, decoded) {
			t.Fatalf("roundtrip %q: %v", name, err)
		}
	}
	for _, bad := range []string{"", "a", "k:Zg==", "k:Zh", "k:_", "k:/w", "k:" + strings.Repeat("YQ", MaxKeyNameBytes)} {
		if _, err := DecodeKeyName(bad); err == nil {
			t.Fatalf("accepted %q", bad)
		}
	}
	if _, err := EncodeKeyName(bytes.Repeat([]byte("a"), MaxKeyNameBytes+1)); err == nil {
		t.Fatal("overlong key accepted")
	}
	if got := NewByteValue("9223372036854775807"); got.Value != "9223372036854775807" || got.Encoding != "utf8" {
		t.Fatal(got)
	}
	if got := NewByteValue("\x00\xff"); got.Encoding != "base64" || got.Value != "AP8=" {
		t.Fatal(got)
	}

}
