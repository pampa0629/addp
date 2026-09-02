package protection

import (
	"bytes"
	"encoding/json"
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	"github.com/addp/common/engine/plugins/mongodb"
	"github.com/addp/common/format"
	commonmodels "github.com/addp/common/models"
)

func TestIntegrationMongoEncodedRecordExportMasksOutdoorPersonsBeforeCanonicalExtendedJSON(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SECURITY_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SECURITY_E2E=1 to run the MongoDB protection integration gate")
	}

	model := plugin.DynamicSchemaCatalogModel()
	path := plugin.EngineCatalogBranchLeafPath(model, 11, plugin.EngineCatalogTermDatabase, "Outdoor", plugin.EngineCatalogTermCollection, plugin.EngineCatalogKindCollection, "Persons")
	fields := []datatype.FieldInfo{
		{Name: "userInfo", Path: []string{"userInfo"}, Type: datatype.FieldTypeJSON, Nullable: true},
		{Name: "userInfo.phone", Path: []string{"userInfo", "phone"}, Type: datatype.FieldTypeString, Nullable: true},
	}
	store := transferProjectionStore(t)
	installActiveTransferProjectionForComponent(t, store, model, path, fields, dataprotection.Component{
		Key: "userInfo.phone", Path: []dataprotection.PathSegment{{Name: "userInfo", Container: "object"}, {Name: "phone", Container: "scalar"}}, ValueType: string(datatype.FieldTypeString),
	})
	gate := NewGate(store, fakeEngineGetter{engine: &commonmodels.Engine{ID: 11, EngineType: "mongodb"}})
	protect, err := gate.PrepareBoundedEncodedRecordProtection(t.Context(), 7, map[string]interface{}{
		"source": map[string]interface{}{"locator": "addp://engine/11/path/Outdoor/Persons?type=collection&item_id=51657"},
	}, fields)
	if err != nil {
		t.Fatalf("prepare encoded record protection: %v", err)
	}

	port, err := strconv.Atoi(transferMongoEnvOrDefault("ADDP_TEST_MONGODB_PORT", "27017"))
	if err != nil || port <= 0 {
		t.Fatal("ADDP_TEST_MONGODB_PORT must be a positive integer")
	}
	provider := &mongodb.MongoDBPlugin{}
	session, err := provider.OpenEncodedRecordReadSession(t.Context(), plugin.ConnectionInfo{
		"host":        transferMongoEnvOrDefault("ADDP_TEST_MONGODB_HOST", "127.0.0.1"),
		"port":        port,
		"user":        transferMongoEnvOrDefault("ADDP_TEST_MONGODB_USER", "admin"),
		"password":    transferMongoEnvOrDefault("ADDP_TEST_MONGODB_PASSWORD", "admin_password"),
		"auth_source": transferMongoEnvOrDefault("ADDP_TEST_MONGODB_AUTH_SOURCE", "admin"),
	}, path, plugin.EncodedRecordReadSessionOptions{
		Format: string(format.FormatMongoDBExtendedJSONL), BeforeEncode: protect,
	})
	if err != nil {
		t.Fatalf("open protected MongoDB encoded record session: %v", err)
	}
	defer session.Close(t.Context()) //nolint:errcheck

	maskedPhones := 0
	for batchIndex := 0; batchIndex < 5; batchIndex++ {
		batch, readErr := session.ReadBatch(t.Context(), 50)
		if readErr != nil {
			t.Fatalf("read protected MongoDB encoded record batch: %v", readErr)
		}
		if batch.Records == 0 {
			break
		}
		lines := bytes.Split(bytes.TrimSuffix(batch.Content, []byte{'\n'}), []byte{'\n'})
		if int64(len(lines)) != batch.Records {
			t.Fatalf("encoded record count = %d, line count = %d", batch.Records, len(lines))
		}
		for _, line := range lines {
			var document map[string]interface{}
			if err := json.Unmarshal(line, &document); err != nil {
				t.Fatalf("protected output is not valid Extended JSON: %v", err)
			}
			userInfo, ok := document["userInfo"].(map[string]interface{})
			if !ok {
				continue
			}
			phone, ok := userInfo["phone"].(string)
			if !ok {
				continue
			}
			if !isTransferMaskedMainlandPhone(phone) {
				t.Fatal("protected MongoDB export emitted a phone outside the masking contract")
			}
			maskedPhones++
		}
		if maskedPhones > 0 {
			break
		}
	}
	if maskedPhones == 0 {
		t.Fatal("protected MongoDB export contained no masked phone")
	}
}

func transferMongoEnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func isTransferMaskedMainlandPhone(value string) bool {
	runes := []rune(value)
	if len(runes) != 11 {
		return false
	}
	for index, current := range runes {
		if index >= 3 && index < 7 {
			if current != '*' {
				return false
			}
			continue
		}
		if current < '0' || current > '9' {
			return false
		}
	}
	return true
}
