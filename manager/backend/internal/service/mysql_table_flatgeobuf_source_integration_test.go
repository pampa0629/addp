package service

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strconv"
	"testing"
	"time"

	commonModels "github.com/addp/common/models"
	_ "github.com/go-sql-driver/mysql"
	"github.com/gogama/flatgeobuf/flatgeobuf"
	"github.com/minio/minio-go/v7"
)

type recordingTemporaryFlatGeobufStore struct {
	exists  bool
	data    []byte
	object  string
	removed bool
	putErr  error
}

func (s *recordingTemporaryFlatGeobufStore) BucketExists(context.Context, string) (bool, error) {
	return s.exists, nil
}

func (s *recordingTemporaryFlatGeobufStore) MakeBucket(context.Context, string, minio.MakeBucketOptions) error {
	s.exists = true
	return nil
}

func (s *recordingTemporaryFlatGeobufStore) PutObject(_ context.Context, _, object string, reader io.Reader, _ int64, _ minio.PutObjectOptions) (minio.UploadInfo, error) {
	data, err := io.ReadAll(reader)
	s.data = data
	s.object = object
	return minio.UploadInfo{}, errors.Join(err, s.putErr)
}

func (s *recordingTemporaryFlatGeobufStore) RemoveObject(_ context.Context, _, object string, _ minio.RemoveObjectOptions) error {
	s.removed = object == s.object
	return nil
}

func TestMySQLTableFlatGeobufSourceIntegration(t *testing.T) {
	if os.Getenv("ADDP_MYSQL_INTEGRATION") != "1" {
		t.Skip("set ADDP_MYSQL_INTEGRATION=1 to run MySQL integration tests")
	}
	password := os.Getenv("ADDP_TEST_MYSQL_PASSWORD")
	if password == "" {
		t.Skip("set ADDP_TEST_MYSQL_PASSWORD to run MySQL integration tests")
	}
	host := envOrManagerDefault("ADDP_TEST_MYSQL_HOST", "127.0.0.1")
	port := envOrManagerDefault("ADDP_TEST_MYSQL_PORT", "3306")
	user := envOrManagerDefault("ADDP_TEST_MYSQL_USER", "root")
	dsn := fmt.Sprintf("%s:%s@tcp(%s:%s)/?parseTime=true", user, password, host, port)
	db, err := sql.Open("mysql", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	database := "addp_manager_mysql_fgb_it_" + strconv.FormatInt(time.Now().UnixNano(), 10)
	if _, err := db.Exec("CREATE DATABASE `" + database + "`"); err != nil {
		t.Fatal(err)
	}
	defer db.Exec("DROP DATABASE IF EXISTS `" + database + "`")
	if _, err := db.Exec("CREATE TABLE `" + database + "`.`features` (id BIGINT PRIMARY KEY, name VARCHAR(32), geom POINT SRID 4326 NOT NULL, SPATIAL INDEX idx_geom (geom)) ENGINE=InnoDB"); err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec("INSERT INTO `" + database + "`.`features` VALUES (1, 'one', ST_GeomFromText('POINT(121.5 31.2)', 4326, 'axis-order=long-lat')), (2, 'two', ST_GeomFromText('POINT(116.4 39.9)', 4326, 'axis-order=long-lat'))"); err != nil {
		t.Fatal(err)
	}

	systemServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":11,"tenant_id":7,"name":"MySQL","engine_type":"mysql","connection_info":{"host":%q,"port":%q,"user":%q,"password":%q,"database":%q},"lifecycle_state":"active"}`, host, port, user, password, database)
	}))
	defer systemServer.Close()
	store := &recordingTemporaryFlatGeobufStore{exists: true}
	executor := &ManagerVectorTileCacheWorkflowExecutor{
		systemClient: newTestSystemClient(systemServer.URL),
		objectStore:  store, minioEndpoint: "http://minio:9000", minioAccessKey: "ak", minioSecretKey: "sk", defaultBucket: "manager",
	}
	identity := tileCacheTaskTargetIdentity{EngineID: 11, SourceKind: "table", Schema: database, Table: "features", FullName: database + ".features"}
	uri, _, facts, cleanup, err := executor.prepareDatabaseTableFlatGeobufSource(context.Background(), 7, "exec-1", identity, commonModels.JSONMap{
		"geometry_column": "geom",
		"max_features":    1,
	})
	if err != nil {
		t.Fatal(err)
	}
	if uri != "/vsis3/manager/tenant_7/executions/exec-1/source.fgb" || facts["access_method"] != "temporary_flatgeobuf" {
		t.Fatalf("source uri=%q facts=%#v", uri, facts)
	}
	if len(store.data) < 12 || string(store.data[:3]) != "fgb" {
		t.Fatalf("temporary FlatGeobuf size=%d header=%q", len(store.data), store.data[:min(3, len(store.data))])
	}
	reader := flatgeobuf.NewFileReader(bytes.NewReader(store.data))
	defer reader.Close()
	if _, err := reader.Header(); err != nil {
		t.Fatalf("read temporary FlatGeobuf header: %v", err)
	}
	features, err := reader.DataRem()
	if err != nil && err != io.EOF {
		t.Fatalf("read temporary FlatGeobuf features: %v", err)
	}
	if len(features) != 2 {
		t.Fatalf("temporary FlatGeobuf feature count = %d, want 2; max_features must remain a per-tile option", len(features))
	}
	if err := cleanup(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !store.removed {
		t.Fatal("temporary FlatGeobuf object was not removed")
	}

	failedStore := &recordingTemporaryFlatGeobufStore{exists: true, putErr: errors.New("injected upload failure")}
	executor.objectStore = failedStore
	_, _, _, _, err = executor.prepareDatabaseTableFlatGeobufSource(context.Background(), 7, "exec-failed", identity, commonModels.JSONMap{"geometry_column": "geom"})
	if err == nil {
		t.Fatal("failed temporary FlatGeobuf upload unexpectedly succeeded")
	}
	if !failedStore.removed {
		t.Fatal("failed temporary FlatGeobuf upload was not cleaned")
	}
}

func envOrManagerDefault(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}
