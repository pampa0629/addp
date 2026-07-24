package deadletter

import (
	"encoding/base64"
	"encoding/json"
	"testing"
	"time"

	engineplugin "github.com/addp/common/engine/plugin"
)

func TestEnvelopePreservesRawKafkaBytes(t *testing.T) {
	now := time.Date(2026, 7, 23, 6, 0, 0, 123, time.UTC)
	envelope, err := NewEnvelope(EnvelopeInput{
		Identity: "a220d5ad-d86e-52ca-ad4f-5ff2d8bfad1c", TenantID: 7, TaskID: 11,
		ExecutionID: "execution-1", ApplyIdentity: "8aa1d865-8d56-4ac3-b9aa-59f50e575c37",
		SourceIdentity: "addp://engine/9/path/orders?type=topic",
		Record: engineplugin.ChangeRecord{
			Topic: "orders", Partition: "2", Offset: 41, Timestamp: now,
			Key: nil, Value: []byte{}, Headers: []engineplugin.ChangeRecordHeader{
				{Key: "nil", Value: nil},
				{Key: "binary", Value: []byte{0x00, 0xff, 0x10}},
			},
		},
		Error:      ErrorDetail{Code: "invalid_json_object", Category: "record_decode", Message: "record value must be a JSON object"},
		DetectedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	data, err := MarshalEnvelope(envelope)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Envelope
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Schema != EnvelopeSchemaV1 || decoded.Record.KeyBase64 != nil {
		t.Fatalf("decoded envelope = %#v", decoded)
	}
	if decoded.Record.ValueBase64 == nil || *decoded.Record.ValueBase64 != "" {
		t.Fatalf("empty value base64 = %#v, want pointer to empty string", decoded.Record.ValueBase64)
	}
	if decoded.Record.Headers[0].ValueBase64 != nil {
		t.Fatalf("nil header value became %#v", decoded.Record.Headers[0].ValueBase64)
	}
	wantBinary := base64.StdEncoding.EncodeToString([]byte{0x00, 0xff, 0x10})
	if decoded.Record.Headers[1].ValueBase64 == nil || *decoded.Record.Headers[1].ValueBase64 != wantBinary {
		t.Fatalf("binary header base64 = %#v, want %q", decoded.Record.Headers[1].ValueBase64, wantBinary)
	}
}
