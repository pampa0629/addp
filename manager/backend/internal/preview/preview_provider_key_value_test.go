package preview

import (
	"context"
	"net"
	"os"
	"strconv"
	"testing"

	"github.com/addp/common/engine/plugin"
	redisplugin "github.com/addp/common/engine/plugins/redis"
	commonModels "github.com/addp/common/models"
	"github.com/addp/common/resourcetree"
	"github.com/addp/manager/internal/models"
)

func TestIntegrationRedisPreview(t *testing.T) {
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
	c := models.ConnectionInfo{"host": host, "port": n, "database": 0, "user": "addp_business_reader", "password": os.Getenv("BUSINESS_REDIS_READER_PASSWORD")}
	p := &redisplugin.RedisPlugin{}
	provider := NewKeyValuePreviewProvider()
	ctx := context.Background()
	loc := &resourcetree.ResourceLocator{EngineID: 42, Type: resourcetree.TypeKeyspace, Path: []string{"keyspace"}}
	path, err := resourcetree.EngineCatalogPathFromLocator(p.EngineCatalogModel(), loc)
	if err != nil {
		t.Fatal(err)
	}
	listReq := &PreviewRequest{Engine: &models.Engine{ID: 42, EngineType: "redis", ConnectionInfo: c}, EnginePlugin: p, ProviderPath: path, Page: 1, PageSize: 100, ItemType: "keyspace", KeyValueOptions: plugin.KeyValueReadOptions{Prefix: "addp:sample:counter"}}
	list, err := provider.Preview(ctx, listReq)
	if err != nil || list.Keyspace == nil || len(list.Keyspace.Keys) != 1 || list.Keyspace.Keys[0].Name.Value != "addp:sample:counter" {
		t.Fatalf("Manager prefix was not passed to Redis: %v %v", list, err)
	}
	if list.Keyspace.Keys[0].Value == nil || list.Keyspace.Keys[0].Value.Value != "9007199254740993" || list.Keyspace.Keys[0].Truncated {
		t.Fatal("Manager list omitted the inline native value", list.Keyspace)
	}
	for _, key := range []string{"addp:sample:counter", "addp:sample:hash", "addp:sample:list", "addp:sample:set", "addp:sample:zset", "addp:sample:stream", "addp:sample:binary\x00\xff"} {
		name, err := plugin.EncodeKeyName([]byte(key))
		if err != nil {
			t.Fatal(err)
		}
		req := &PreviewRequest{Engine: &models.Engine{ID: 42, EngineType: "redis", ConnectionInfo: c}, EnginePlugin: p, ProviderPath: path, Page: 1, PageSize: 1, ItemType: "keyspace", KeyValueOptions: plugin.KeyValueReadOptions{Key: name}}
		value, err := provider.Preview(ctx, req)
		if err != nil || value.Mode != PreviewModeKeyValue || value.KeyValue == nil || len(value.Fields) != 0 || len(value.Columns) != 0 {
			t.Fatalf("native Manager result %s: %v", key, err)
		}
		if key == "addp:sample:counter" && value.KeyValue.Value.Value != "9007199254740993" {
			t.Fatal("integer precision lost")
		}
		if key == "addp:sample:binary\x00\xff" && value.KeyValue.Value.Encoding != "base64" {
			t.Fatal("binary content lost")
		}
		req.Page = 2
		if _, err := provider.Preview(ctx, req); err == nil {
			t.Fatal("unstable pagination accepted")
		}
	}
}

func TestKeyValueRoutingRequiresScannedIdentity(t *testing.T) {
	req := &PreviewResolverRequest{ItemType: "keyspace", Locator: &resourcetree.ResourceLocator{EngineID: 42, Type: resourcetree.TypeKeyspace, Path: []string{"keyspace"}}, Engine: &commonModels.Engine{ID: 42, EngineType: "redis"}}
	if names := providerNamesForMeta(req, nil); len(names) != 1 || names[0] != "builtin:key-value" {
		t.Fatal(names)
	}
	if !isPreviewItemType("keyspace") {
		t.Fatal("key item rejected")
	}
	r := NewPreviewResolver(nil, nil, nil)
	if _, err := r.Preview(context.Background(), req); err != ErrPreviewRequiresScannedMeta {
		t.Fatalf("unscanned key allowed: %v", err)
	}
	itemID := uint(42)
	req.MetaItemID = &itemID
	req.Metadata = &commonModels.MetaNode{Attributes: map[string]interface{}{"item": map[string]interface{}{"data_type": "key_value", "layout": "single"}}}
	req.ItemScannedDepth = "deep"
	if schemaCoverage(req) != "unknown" {
		t.Fatal("identity scan advertised complete field schema")
	}
}
