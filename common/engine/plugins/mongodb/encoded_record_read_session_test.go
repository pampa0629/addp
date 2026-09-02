package mongodb

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/format"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/bson/primitive"
)

func TestIntegrationEncodedRecordReadSessionExportsOutdoorPersonsCanonicalExtendedJSON(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SCHEMA_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SCHEMA_E2E=1 to run against Business MongoDB")
	}
	provider := &MongoDBPlugin{}
	session, err := provider.OpenEncodedRecordReadSession(t.Context(), plugin.ConnectionInfo{
		"host": "localhost", "port": 27017, "user": "admin", "password": "admin_password", "auth_source": "admin",
	}, plugin.EngineCatalogPath{
		Version: "v1", EngineID: 11,
		Segments: []plugin.EngineCatalogSegment{
			{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
			{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "Persons"},
		},
	}, plugin.EncodedRecordReadSessionOptions{Format: string(format.FormatMongoDBExtendedJSONL)})
	if err != nil {
		t.Fatalf("OpenEncodedRecordReadSession() error = %v", err)
	}
	defer session.Close(t.Context()) //nolint:errcheck
	batch, err := session.ReadBatch(t.Context(), 2)
	if err != nil {
		t.Fatalf("ReadBatch() error = %v", err)
	}
	if batch.Records == 0 || batch.Records > 2 || int64(bytes.Count(batch.Content, []byte{'\n'})) != batch.Records {
		t.Fatalf("batch = records:%d content:%q", batch.Records, batch.Content)
	}
	for _, line := range bytes.Split(bytes.TrimSuffix(batch.Content, []byte{'\n'}), []byte{'\n'}) {
		if !json.Valid(line) {
			t.Fatalf("content line is not valid JSON: %s", line)
		}
	}
}

func TestMarshalMongoCanonicalExtendedJSONLinePreservesBSONTypes(t *testing.T) {
	objectID := primitive.NewObjectID()
	decimal, err := primitive.ParseDecimal128("123.4500")
	if err != nil {
		t.Fatalf("ParseDecimal128() error = %v", err)
	}
	raw, err := bson.Marshal(bson.D{
		{Key: "_id", Value: objectID},
		{Key: "count", Value: int32(7)},
		{Key: "created_at", Value: primitive.NewDateTimeFromTime(time.Unix(1700000000, 0).UTC())},
		{Key: "amount", Value: decimal},
		{Key: "payload", Value: primitive.Binary{Subtype: 0x80, Data: []byte{1, 2, 3}}},
	})
	if err != nil {
		t.Fatalf("bson.Marshal() error = %v", err)
	}

	line, err := marshalMongoCanonicalExtendedJSONLine(raw, nil)
	if err != nil {
		t.Fatalf("marshalMongoCanonicalExtendedJSONLine() error = %v", err)
	}
	if len(line) == 0 || line[len(line)-1] != '\n' {
		t.Fatalf("line = %q, want newline terminated", line)
	}
	if !json.Valid(line[:len(line)-1]) {
		t.Fatalf("line is not valid JSON: %s", line)
	}
	for _, marker := range []string{"$oid", "$numberInt", "$date", "$numberDecimal", "$binary"} {
		if !strings.Contains(string(line), marker) {
			t.Fatalf("line = %s, want canonical marker %s", line, marker)
		}
	}
}

func TestMarshalMongoCanonicalExtendedJSONLineTransformsBeforeEncodingAndPreservesBSONTypes(t *testing.T) {
	objectID := primitive.NewObjectID()
	raw, err := bson.Marshal(bson.D{
		{Key: "_id", Value: objectID},
		{Key: "created_at", Value: primitive.NewDateTimeFromTime(time.Unix(1700000000, 0).UTC())},
		{Key: "userInfo", Value: bson.D{{Key: "phone", Value: "13661384499"}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	line, err := marshalMongoCanonicalExtendedJSONLine(raw, func(document map[string]interface{}) error {
		userInfo, ok := document["userInfo"].(map[string]interface{})
		if !ok {
			return errors.New("userInfo is not a document")
		}
		userInfo["phone"] = "136****4499"
		return nil
	})
	if err != nil {
		t.Fatalf("marshalMongoCanonicalExtendedJSONLine() error = %v", err)
	}
	content := string(line)
	if strings.Contains(content, "13661384499") || !strings.Contains(content, "136****4499") {
		t.Fatalf("protected line = %s", content)
	}
	for _, marker := range []string{"$oid", "$date"} {
		if !strings.Contains(content, marker) {
			t.Fatalf("protected line = %s, want canonical marker %s", content, marker)
		}
	}
}
