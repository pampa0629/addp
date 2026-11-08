package avro

import (
	"bytes"
	"encoding/json"
	"testing"

	goavro "github.com/linkedin/goavro/v2"
)

func TestNullableEmptyBytesRemainDistinctFromNullAcrossCodecs(t *testing.T) {
	for _, compression := range []string{"null", "deflate", "snappy"} {
		t.Run(compression, func(t *testing.T) {
			var encoded bytes.Buffer
			writer, err := goavro.NewOCFWriter(goavro.OCFConfig{W: &encoded, Schema: primitiveSchema, CompressionName: compression})
			if err != nil {
				t.Fatal(err)
			}
			var values []interface{}
			for i, value := range []interface{}{goavro.Union("bytes", []byte{}), nil, goavro.Union("bytes", []byte{0, 255})} {
				values = append(values, map[string]interface{}{"i": int32(i), "id": int64(-9007199254740993 - i), "label": nil,
					"flag": false, "f": float32(1.25), "d": -2.5, "blob": value})
			}
			if err := writer.Append(values); err != nil {
				t.Fatal(err)
			}
			rows, err := NewPlugin().SampleTable(t.Context(), bytes.NewReader(encoded.Bytes()), 0, 3, nil)
			if err != nil || len(rows) != 3 {
				t.Fatalf("read: %#v %v", rows, err)
			}
			value, ok := rows[0]["blob"].([]byte)
			if !ok || value == nil || len(value) != 0 || rows[1]["blob"] != nil || !bytes.Equal(rows[2]["blob"].([]byte), []byte{0, 255}) {
				t.Fatalf("empty/null/binary values were conflated: %#v", rows)
			}
			public, err := json.Marshal(rows[0]["blob"])
			if err != nil || string(public) != `""` {
				t.Fatalf("empty bytes public value: %s %v", public, err)
			}
			if rows[2]["id"] != int64(-9007199254740995) {
				t.Fatal("negative bigint precision changed")
			}
		})
	}
}
