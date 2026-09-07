package mongodb

import (
	"context"
	"errors"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/addp/common/engine/plugin"
	"go.mongodb.org/mongo-driver/bson"
	"go.mongodb.org/mongo-driver/mongo"
	"go.mongodb.org/mongo-driver/mongo/options"
)

func mongoIntegrationConnectionInfo(t *testing.T, database string) plugin.ConnectionInfo {
	t.Helper()
	portText := mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_PORT", "27017")
	port, err := strconv.Atoi(portText)
	if err != nil || port <= 0 || port > 65535 {
		t.Fatalf("invalid ADDP_TEST_MONGODB_PORT %q", portText)
	}
	connectionInfo := plugin.ConnectionInfo{
		"host":        mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_HOST", "127.0.0.1"),
		"port":        port,
		"user":        mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_USER", "admin"),
		"password":    mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_PASSWORD", "admin_password"),
		"auth_source": mongoIntegrationEnvOrDefault("ADDP_TEST_MONGODB_AUTH_SOURCE", "admin"),
	}
	if database != "" {
		connectionInfo["database"] = database
	}
	return connectionInfo
}

func mongoIntegrationEnvOrDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

func ensureMongoIntegrationOutdoorPersonsFixture(t *testing.T) plugin.ConnectionInfo {
	t.Helper()
	connectionInfo := mongoIntegrationConnectionInfo(t, "Outdoor")
	dsn, err := (&MongoDBPlugin{}).BuildDSN(connectionInfo)
	if err != nil {
		t.Fatalf("build MongoDB integration fixture DSN: %v", err)
	}
	client, err := mongo.Connect(t.Context(), options.Client().ApplyURI(dsn))
	if err != nil {
		t.Fatalf("connect MongoDB integration fixture: %v", err)
	}
	if err := client.Ping(t.Context(), nil); err != nil {
		_ = client.Disconnect(context.Background())
		t.Fatalf("ping MongoDB integration fixture: %v", err)
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = client.Disconnect(cleanupContext)
	})

	databaseNames, err := client.ListDatabaseNames(t.Context(), bson.M{"name": "Outdoor"})
	if err != nil {
		t.Fatalf("inspect MongoDB integration fixture database: %v", err)
	}
	databaseExisted := len(databaseNames) > 0
	database := client.Database("Outdoor")
	collectionNames, err := database.ListCollectionNames(t.Context(), bson.M{"name": "Persons"})
	if err != nil {
		t.Fatalf("inspect MongoDB integration fixture collection: %v", err)
	}
	collectionExisted := len(collectionNames) > 0
	collection := database.Collection("Persons")
	fixtureID := "addp-common-mongodb-integration"
	filter := bson.M{"_id": fixtureID}
	var previous bson.M
	findErr := collection.FindOne(t.Context(), filter).Decode(&previous)
	fixture := bson.M{
		"_id":      fixtureID,
		"_openid":  fixtureID,
		"userInfo": bson.M{"phone": "13661384499", "nickName": "common-e2e"},
		"entriedOutdoors": bson.A{
			bson.M{"title": "MongoDB integration fixture"},
		},
	}
	switch {
	case errors.Is(findErr, mongo.ErrNoDocuments):
		if _, err := collection.InsertOne(t.Context(), fixture); err != nil {
			t.Fatalf("insert MongoDB integration fixture: %v", err)
		}
	case findErr != nil:
		t.Fatalf("inspect MongoDB integration fixture: %v", findErr)
	default:
		if _, err := collection.ReplaceOne(t.Context(), filter, fixture); err != nil {
			t.Fatalf("replace MongoDB integration fixture: %v", err)
		}
	}
	t.Cleanup(func() {
		cleanupContext, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		if errors.Is(findErr, mongo.ErrNoDocuments) {
			_, _ = collection.DeleteOne(cleanupContext, filter)
			if !databaseExisted {
				_ = database.Drop(cleanupContext)
			} else if !collectionExisted {
				_ = collection.Drop(cleanupContext)
			}
			return
		}
		_, _ = collection.ReplaceOne(cleanupContext, filter, previous)
	})

	return connectionInfo
}

func TestMongoIntegrationConnectionInfoUsesTestEnvironment(t *testing.T) {
	t.Setenv("ADDP_TEST_MONGODB_HOST", "mongo-ci")
	t.Setenv("ADDP_TEST_MONGODB_PORT", "37017")
	t.Setenv("ADDP_TEST_MONGODB_USER", "addp_ci")
	t.Setenv("ADDP_TEST_MONGODB_PASSWORD", "addp_ci_password")
	t.Setenv("ADDP_TEST_MONGODB_AUTH_SOURCE", "ci_admin")

	got := mongoIntegrationConnectionInfo(t, "Outdoor")
	want := plugin.ConnectionInfo{
		"host":        "mongo-ci",
		"port":        37017,
		"user":        "addp_ci",
		"password":    "addp_ci_password",
		"auth_source": "ci_admin",
		"database":    "Outdoor",
	}
	for key, wantValue := range want {
		if got[key] != wantValue {
			t.Fatalf("connection info[%q] = %#v, want %#v", key, got[key], wantValue)
		}
	}
}
