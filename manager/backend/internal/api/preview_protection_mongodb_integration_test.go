package api

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/addp/common/dataprotection"
	"github.com/addp/common/dataprotection/projectionstore"
	"github.com/addp/common/datatype"
	"github.com/addp/common/engine/plugin"
	_ "github.com/addp/common/engine/plugins/mongodb"
	commonModels "github.com/addp/common/models"
	"github.com/addp/manager/internal/dataprofile"
	"github.com/addp/manager/internal/models"
	"github.com/addp/manager/internal/preview"
	managerprotection "github.com/addp/manager/internal/protection"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func TestIntegrationManagerMongoOutdoorPersonsPreviewMasksPhone(t *testing.T) {
	if os.Getenv("ADDP_MONGODB_SECURITY_E2E") != "1" {
		t.Skip("set ADDP_MONGODB_SECURITY_E2E=1 to run the MongoDB protection integration gate")
	}
	connInfo := ensureMongoOutdoorPersonsFixture(t)
	provider := preview.NewDynamicSchemaCollectionPreviewProvider()
	table, err := provider.Preview(t.Context(), &preview.PreviewRequest{
		Engine: &models.Engine{
			ID: 11, EngineType: "mongodb",
			ConnectionInfo: models.ConnectionInfo(connInfo),
		},
		Schema: "Outdoor", Table: "Persons", Page: 1, PageSize: 50,
		ProviderPath: plugin.EngineCatalogPath{
			Version: plugin.EngineCatalogPathVersion, EngineID: 11,
			Segments: []plugin.EngineCatalogSegment{
				{Term: plugin.EngineCatalogTermServer, Kind: plugin.EngineCatalogTermServer},
				{Term: plugin.EngineCatalogTermDatabase, Kind: plugin.EngineCatalogKindNamespace, Name: "Outdoor"},
				{Term: plugin.EngineCatalogTermCollection, Kind: plugin.EngineCatalogKindCollection, Name: "Persons"},
			},
		},
	})
	if err != nil {
		t.Fatalf("MongoDB preview failed: %v", err)
	}
	if len(table.Rows) == 0 || len(table.Fields) == 0 {
		t.Fatal("MongoDB preview returned no rows or schema fields")
	}

	req := &preview.PreviewResolverRequest{
		ItemName: "Persons", ItemFingerprint: "sha256:mongodb-outdoor-persons",
		Metadata: &commonModels.MetaNode{Attributes: map[string]interface{}{
			"type_info": map[string]interface{}{
				"table": datatype.TableInfoPayload(&datatype.TableInfo{Name: "Persons", Fields: table.Fields}),
			},
		}},
	}
	now := time.Now().UTC()
	projection := activeOutdoorPhoneProjection(t, req, now)
	profileRules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive,
		Projections: []dataprotection.Projection{projection},
	}, managerprotection.ActionProfile, now)
	if err != nil {
		t.Fatalf("active profile projection validation failed: %v", err)
	}
	profile := dataprofile.Build(table.Rows, table.Fields, dataprofile.BuildOptions{
		Mode: dataprofile.ModeSample, DataScope: dataprofile.DataScope{Kind: dataprofile.DataScopeKindAll},
		SampleMethod: "integration_fixture", RowsScanned: int64(len(table.Rows)), TopN: 10, HistogramBins: 10, ProfiledAt: now,
	})
	protectedProfile, err := managerprotection.ProtectProfile(&profile, profileRules)
	if err != nil {
		t.Fatalf("MongoDB profile protection failed: %v", err)
	}
	profilePayload, _ := json.Marshal(protectedProfile)
	if strings.Contains(string(profilePayload), "13661384499") {
		t.Fatalf("protected MongoDB profile leaked a phone value: %s", profilePayload)
	}
	for _, field := range protectedProfile.Fields {
		if field.Name == "userInfo.phone" {
			t.Fatalf("protected MongoDB profile retained the sensitive field: %#v", field)
		}
	}
	rules, err := managerprotection.TableRules(req.ItemFingerprint, req.TableFields(), projectionstore.GateResult{
		Managed: true, State: dataprotection.ProjectionStateActive,
		Projections: []dataprotection.Projection{projection},
	}, managerprotection.ActionPreview, now)
	if err != nil {
		t.Fatalf("active projection validation failed: %v", err)
	}
	result := &preview.PreviewResult{PreviewType: "table", Data: table}
	if err := applyPreviewProtection(result, rules, dataprotection.SubjectReference{}); err != nil {
		t.Fatalf("MongoDB preview protection failed: %v", err)
	}

	maskedPhones := 0
	for _, row := range table.Rows {
		userInfo, ok := row["userInfo"].(map[string]interface{})
		if !ok {
			continue
		}
		phone, ok := userInfo["phone"].(string)
		if !ok {
			continue
		}
		if isMaskedMainlandPhone(phone) {
			maskedPhones++
			continue
		}
		t.Fatal("protected MongoDB preview returned a phone value outside the masking contract")
	}
	if maskedPhones == 0 {
		t.Fatal("protected MongoDB preview contained no masked phone")
	}
}

func isMaskedMainlandPhone(value string) bool {
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

func ensureMongoOutdoorPersonsFixture(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	host := envOrDefault("ADDP_TEST_MONGODB_HOST", "127.0.0.1")
	portText := envOrDefault("ADDP_TEST_MONGODB_PORT", "27017")
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 {
		t.Fatalf("invalid ADDP_TEST_MONGODB_PORT %q", portText)
	}
	user := envOrDefault("ADDP_TEST_MONGODB_USER", "admin")
	password := envOrDefault("ADDP_TEST_MONGODB_PASSWORD", "admin_password")
	authSource := envOrDefault("ADDP_TEST_MONGODB_AUTH_SOURCE", "admin")
	credentials := url.UserPassword(user, password).String()
	uri := fmt.Sprintf("mongodb://%s@%s:%d/?authSource=%s", credentials, host, port, url.QueryEscape(authSource))
	client, err := mongo.Connect(t.Context(), options.Client().ApplyURI(uri))
	if err != nil {
		t.Fatalf("connect MongoDB fixture: %v", err)
	}
	if err := client.Ping(t.Context(), nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("ping MongoDB fixture: %v", err)
	}

	databaseNames, err := client.ListDatabaseNames(t.Context(), bson.M{"name": "Outdoor"})
	if err != nil {
		t.Fatalf("inspect MongoDB fixture database: %v", err)
	}
	databaseExisted := len(databaseNames) > 0
	database := client.Database("Outdoor")
	collectionNames, err := database.ListCollectionNames(t.Context(), bson.M{"name": "Persons"})
	if err != nil {
		t.Fatalf("inspect MongoDB fixture collection: %v", err)
	}
	collectionExisted := len(collectionNames) > 0
	collection := database.Collection("Persons")
	count, err := collection.CountDocuments(t.Context(), bson.M{})
	if err != nil {
		t.Fatalf("count MongoDB fixture documents: %v", err)
	}
	fixtureID := "addp-security-manager-mongodb-integration"
	fixtureInserted := false
	if count == 0 {
		_, err = collection.InsertOne(t.Context(), bson.M{
			"_id":      fixtureID,
			"userInfo": bson.M{"phone": "13661384499", "nickName": "security-e2e"},
		})
		if err != nil {
			t.Fatalf("insert MongoDB protection fixture: %v", err)
		}
		fixtureInserted = true
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if fixtureInserted {
			_, _ = collection.DeleteOne(cleanupContext, bson.M{"_id": fixtureID})
			if !databaseExisted {
				_ = database.Drop(cleanupContext)
			} else if !collectionExisted {
				_ = collection.Drop(cleanupContext)
			}
		}
		_ = client.Disconnect(cleanupContext)
	})

	return plugin.ConnectionInfo{
		"host": host, "port": port, "user": user, "password": password,
		"auth_source": authSource,
	}
}

func envOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
