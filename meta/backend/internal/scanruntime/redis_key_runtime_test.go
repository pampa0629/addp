package scanruntime

import (
	"context"
	"io"
	"log/slog"
	"net"
	"os"
	"strconv"

	"testing"

	"github.com/addp/common/engine/plugin"
	redisplugin "github.com/addp/common/engine/plugins/redis"
	commonModels "github.com/addp/common/models"
	"github.com/addp/meta/internal/metatest"
	"github.com/addp/meta/internal/models"
	metaRepo "github.com/addp/meta/internal/repository"
	rdb "github.com/redis/go-redis/v9"
)

func TestIntegrationRedisKeyspaceScanStableAndPrunesOldKeys(t *testing.T) {
	if os.Getenv("ADDP_REDIS_INTEGRATION") != "1" {
		t.Skip("run make test-common-redis")
	}
	host, port, err := net.SplitHostPort(os.Getenv("ADDP_REDIS_T2_ENDPOINT"))
	if err != nil {
		t.Fatal(err)
	}
	n, err := strconv.Atoi(port)
	if err != nil {
		t.Fatal(err)
	}
	db := metatest.OpenMetadataDB(t)
	repo := metaRepo.NewScanRepository(db)
	p := &redisplugin.RedisPlugin{}
	resource := &commonModels.Engine{ID: 42, Name: "Redis", EngineType: "redis", ConnectionInfo: commonModels.ConnectionInfo{"host": host, "port": n, "database": 0, "user": "addp_business_reader", "password": os.Getenv("BUSINESS_REDIS_READER_PASSWORD")}}
	runtime := NewDirectLeafRuntime(slog.New(slog.NewTextHandler(io.Discard, nil)), repo)
	ctx := context.Background()
	count, err := runtime.ScanRoot(ctx, p, resource, 1, models.ScannedDepthDeep, true)
	if err != nil || count != 1 {
		t.Fatalf("scan %d: %v", count, err)
	}
	name := "keyspace"
	item, ok, err := repo.FindItemByFullName(1, 42, name)
	if err != nil || !ok {
		t.Fatal("key identity missing", err)
	}
	attrs := item.Attributes["item"].(map[string]interface{})
	if item.Name != name || item.ItemType != "keyspace" || attrs["data_type"] != "key_value" || attrs["layout"] != "single" {
		t.Fatal(item)
	}
	if item.Attributes["type_info"] != nil || item.RowCount != nil {
		t.Fatal("native type converted into table facts")
	}
	var root models.MetaNode
	if err := db.First(&root, item.NodeID).Error; err != nil || root.NodeType != "server" {
		t.Fatal(root, err)
	}
	admin := rdb.NewClient(&rdb.Options{Addr: os.Getenv("ADDP_REDIS_T2_ENDPOINT"), Username: "addp_business_admin", Password: os.Getenv("BUSINESS_REDIS_ADMIN_PASSWORD")})
	defer admin.Close()
	if err := admin.Set(ctx, "addp:sample:changing", "x", 0).Err(); err != nil {
		t.Fatal(err)
	}
	defer admin.Del(ctx, "addp:sample:changing")
	oldName, _ := plugin.EncodeKeyName([]byte("old-key"))
	if _, err := repo.UpsertItemWithDepth(1, 42, &root, "key", oldName, oldName, models.JSONMap{"item": map[string]interface{}{"data_type": "unknown", "layout": "single"}}, nil, nil, nil, models.ScannedDepthDeep); err != nil {
		t.Fatal(err)
	}
	if count, err := runtime.ScanRoot(ctx, p, resource, 1, models.ScannedDepthDeep, true); err != nil || count != 1 {
		t.Fatal(count, err)
	}
	resource.ConnectionInfo["password"] = "wrong"
	if _, err := runtime.ScanRoot(ctx, p, resource, 1, models.ScannedDepthDeep, true); err == nil {
		t.Fatal("bad connection scan accepted")
	}
	var after models.MetaItem
	if err := db.First(&after, item.ID).Error; err != nil || after.Fingerprint != item.Fingerprint {
		t.Fatal("existing identity pruned during failed scan", err)
	}
	var visible int64
	if err := db.Model(&models.MetaItem{}).Where("engine_id = ?", 42).Count(&visible).Error; err != nil || visible != 1 {
		t.Fatal("catalog changed on listing failure", visible, err)
	}
}
