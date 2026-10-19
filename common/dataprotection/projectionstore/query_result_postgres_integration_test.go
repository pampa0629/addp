package projectionstore

import (
	"os"
	"testing"

	"gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestProjectionStoreAuthorizedQueryResultAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_PROJECTIONSTORE_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	sqlDB, err := db.DB()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = sqlDB.Close() })
	for _, identity := range []bool{false, true} {
		name := "alias"
		if identity {
			name = "direct"
		}
		t.Run(name, func(t *testing.T) {
			schema := "projection_query_result_" + name + "_it"
			dropProjectionStoreTestSchema(t, db, schema)
			t.Cleanup(func() { dropProjectionStoreTestSchema(t, db, schema) })
			store, err := Migrate(db, schema, "develop", nil)
			if err != nil {
				t.Fatal(err)
			}
			assertAuthorizedQueryResultLifecycle(t, store, identity)
			assertQueryGrantExecutionBoundary(t, store)
		})
	}
}
