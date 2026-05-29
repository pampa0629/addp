package catalogview

import (
	"strings"
	"testing"
	"time"

	commonJSON "github.com/addp/common/jsonmap"
	"github.com/addp/common/models"
)

func TestConvertNodeType(t *testing.T) {
	tests := []struct {
		name         string
		metaNodeType string
		want         ResourceType
	}{
		{
			name:         "数据库",
			metaNodeType: "database",
			want:         TypeDatabase,
		},
		{
			name:         "Schema",
			metaNodeType: "schema",
			want:         TypeSchema,
		},
		{
			name:         "Bucket",
			metaNodeType: "bucket",
			want:         TypeBucket,
		},
		{
			name:         "前缀（目录）",
			metaNodeType: "prefix",
			want:         TypeDirectory,
		},
		{
			name:         "表",
			metaNodeType: "table",
			want:         TypeTable,
		},
		{
			name:         "集合",
			metaNodeType: "collection",
			want:         TypeCollection,
		},
		{
			name:         "图整体",
			metaNodeType: "graph",
			want:         TypeGraph,
		},
		{
			name:         "对象",
			metaNodeType: "object",
			want:         TypeObject,
		},
		{
			name:         "未知类型",
			metaNodeType: "unknown_type",
			want:         TypeUnknown,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := convertNodeType(tt.metaNodeType)
			if got != tt.want {
				t.Errorf("convertNodeType(%s) = %v, want %v", tt.metaNodeType, got, tt.want)
			}
		})
	}
}

func TestParsePath(t *testing.T) {
	tests := []struct {
		name     string
		fullName string
		nodeType string
		want     []string
	}{
		{
			name:     "Schema 单级",
			fullName: "public",
			nodeType: "schema",
			want:     []string{"public"},
		},
		{
			name:     "Database 单级",
			fullName: "business",
			nodeType: "database",
			want:     []string{"business"},
		},
		{
			name:     "MinIO 多级路径",
			fullName: "uploads/2024/geo",
			nodeType: "prefix",
			want:     []string{"uploads", "2024", "geo"},
		},
		{
			name:     "Bucket",
			fullName: "my-bucket",
			nodeType: "bucket",
			want:     []string{"my-bucket"},
		},
		{
			name:     "空路径",
			fullName: "",
			nodeType: "schema",
			want:     []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parsePath(tt.fullName, tt.nodeType)
			if !equalStringSlice(got, tt.want) {
				t.Errorf("parsePath(%s, %s) = %v, want %v", tt.fullName, tt.nodeType, got, tt.want)
			}
		})
	}
}

func TestBuildFromMeta(t *testing.T) {
	builder := NewTreeBuilder(nil)

	engine := &models.Engine{
		ID:         1,
		Name:       "PostgreSQL 主库",
		EngineType: "postgresql",
	}

	now := time.Now()
	metaNodes := []*models.MetaNode{
		{
			ID:             100,
			EngineID:       1,
			ParentNodeID:   nil,
			NodeType:       "schema",
			Name:           "public",
			FullName:       "public",
			Depth:          1,
			Path:           "public",
			ScanStatus:     "completed",
			LastScanAt:     &now,
			ItemCount:      10,
			TotalSizeBytes: 1024000,
		},
		{
			ID:             101,
			EngineID:       1,
			ParentNodeID:   nil,
			NodeType:       "schema",
			Name:           "private",
			FullName:       "private",
			Depth:          1,
			Path:           "private",
			ScanStatus:     "pending",
			ItemCount:      5,
			TotalSizeBytes: 512000,
		},
	}

	tree, err := builder.BuildFromMeta(engine, metaNodes, 2)
	if err != nil {
		t.Fatalf("BuildFromMeta() error = %v", err)
	}

	// 验证根节点
	if tree.Label != "PostgreSQL 主库" {
		t.Errorf("tree.Label = %s, want %s", tree.Label, "PostgreSQL 主库")
	}
	if tree.Type != "engine" {
		t.Errorf("tree.Type = %s, want %s", tree.Type, "engine")
	}

	// 验证子节点数量
	if len(tree.Children) != 2 {
		t.Errorf("len(tree.Children) = %d, want 2", len(tree.Children))
	}

	// 验证第一个子节点
	if len(tree.Children) > 0 {
		child := tree.Children[0]
		if child.Label != "public" {
			t.Errorf("child.Label = %s, want %s", child.Label, "public")
		}
		if child.Type != "schema" {
			t.Errorf("child.Type = %s, want %s", child.Type, "schema")
		}

		// 验证 Locator
		expectedLocator := "addp://engine/1/path/public?type=schema&node_id=100"
		if child.Locator != expectedLocator {
			t.Errorf("child.Locator = %s, want %s", child.Locator, expectedLocator)
		}

		// 验证元数据
		if nodeID, ok := child.Metadata["node_id"].(uint); !ok || nodeID != 100 {
			t.Errorf("child.Metadata[node_id] = %v, want 100", child.Metadata["node_id"])
		}
		if itemCount, ok := child.Metadata["item_count"].(int); !ok || itemCount != 10 {
			t.Errorf("child.Metadata[item_count] = %v, want 10", child.Metadata["item_count"])
		}
	}
}

func TestBuildFromMetadataTreeAttachesItems(t *testing.T) {
	builder := NewTreeBuilder(nil)
	engine := &models.Engine{
		ID:         7,
		Name:       "PostgreSQL 主库",
		EngineType: "postgresql",
	}
	schemaID := uint(11)
	rowCount := int64(42)
	sizeBytes := int64(2048)
	tree, err := builder.BuildFromMetadataTree(engine, &models.MetadataTree{
		TopNodes: []models.MetaNode{
			{
				ID:         schemaID,
				EngineID:   7,
				NodeType:   "schema",
				Name:       "public",
				FullName:   "public",
				Depth:      1,
				ItemCount:  1,
				ScanStatus: "completed",
			},
		},
		Items: []models.MetaItem{
			{
				ID:        21,
				EngineID:  7,
				NodeID:    schemaID,
				ItemType:  "table",
				Name:      "roads",
				FullName:  "public.roads",
				RowCount:  &rowCount,
				SizeBytes: &sizeBytes,
				Attributes: map[string]interface{}{
					"item": map[string]interface{}{
						"data_type": "table",
					},
				},
			},
		},
	})
	if err != nil {
		t.Fatalf("BuildFromMetadataTree() error = %v", err)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("len(tree.Children) = %d, want 1", len(tree.Children))
	}
	schema := tree.Children[0]
	if len(schema.Children) != 1 {
		t.Fatalf("len(schema.Children) = %d, want 1: %#v", len(schema.Children), schema.Children)
	}
	item := schema.Children[0]
	if item.Label != "roads" || item.Type != "table" {
		t.Fatalf("item = %s/%s, want roads/table", item.Label, item.Type)
	}
	if !strings.Contains(item.Locator, "public/roads") || !strings.Contains(item.Locator, "item_id=21") {
		t.Fatalf("item locator = %q, want virtual item locator", item.Locator)
	}
	if got := item.Metadata["size_bytes"]; got != sizeBytes {
		t.Fatalf("item size_bytes = %#v, want %d", got, sizeBytes)
	}
	if got := item.Metadata["row_count"]; got != rowCount {
		t.Fatalf("item row_count = %#v, want %d", got, rowCount)
	}
	if got := commonJSON.Int64(item.Metadata, "type_info.table", "row_count"); got != rowCount {
		t.Fatalf("item type_info.table.row_count = %d, want %d", got, rowCount)
	}
}

func TestBuildFromMetaMergesWholeScopeItemWithSamePathDirectory(t *testing.T) {
	builder := NewTreeBuilder(nil)
	engine := &models.Engine{ID: 26, Name: "Business NFS", EngineType: "nfs"}
	parentID := uint(95)
	metaNodes := []*models.MetaNode{
		{
			ID:           parentID,
			EngineID:     26,
			ParentNodeID: nil,
			NodeType:     "dir",
			Name:         "lake",
			FullName:     "lake",
			Depth:        1,
			Path:         "lake",
			ScanStatus:   "completed",
			ItemCount:    1,
		},
		{
			ID:           100975,
			EngineID:     26,
			ParentNodeID: &parentID,
			NodeType:     "file",
			Name:         "lake",
			FullName:     "lake",
			Depth:        1,
			Path:         "lake",
			ScanStatus:   "completed",
			Attributes: map[string]interface{}{
				"item_id":      uint(975),
				"is_meta_item": true,
				"item": map[string]interface{}{
					"layout":    "whole",
					"data_type": "table",
					"format":    "parquet",
				},
			},
		},
	}

	tree, err := builder.BuildFromMeta(engine, metaNodes, 2)
	if err != nil {
		t.Fatalf("BuildFromMeta() error = %v", err)
	}
	if len(tree.Children) != 1 {
		t.Fatalf("len(tree.Children) = %d, want 1: %#v", len(tree.Children), tree.Children)
	}
	child := tree.Children[0]
	if child.Label != "lake" || child.Type != "file" {
		t.Fatalf("child = %s/%s, want lake/file", child.Label, child.Type)
	}
	if strings.Contains(child.Locator, "node_id=95") || !strings.Contains(child.Locator, "item_id=975") {
		t.Fatalf("locator = %q, want whole-scope item locator", child.Locator)
	}
}

func TestConvertNodeToTree(t *testing.T) {
	builder := NewTreeBuilder(nil)

	loc := &ResourceLocator{
		EngineID: 1,
		Path:     []string{"public", "users"},
		Type:     TypeTable,
		ItemID:   uintPtr(100),
	}

	metadata := map[string]interface{}{
		"item_id":    uint(100),
		"item_count": 1000,
		"size_bytes": int64(1048576),
	}

	node := builder.ConvertNodeToTree(loc, metadata)

	// 验证节点
	if node.Label != "users" {
		t.Errorf("node.Label = %s, want %s", node.Label, "users")
	}
	if node.Type != "table" {
		t.Errorf("node.Type = %s, want %s", node.Type, "table")
	}

	// 验证 Locator
	expectedLocator := "addp://engine/1/path/public/users?type=table&item_id=100"
	if node.Locator != expectedLocator {
		t.Errorf("node.Locator = %s, want %s", node.Locator, expectedLocator)
	}

	// 验证元数据
	if itemID, ok := node.Metadata["item_id"].(uint); !ok || itemID != 100 {
		t.Errorf("node.Metadata[item_id] = %v, want 100", node.Metadata["item_id"])
	}
}

func TestGetNodeByLocator(t *testing.T) {
	// 构建测试树
	tree := &TreeNode{
		Locator: "addp://engine/1/path/?type=database",
		Label:   "Root",
		Type:    "engine",
		Children: []*TreeNode{
			{
				Locator: "addp://engine/1/path/public?type=schema",
				Label:   "public",
				Type:    "schema",
				Children: []*TreeNode{
					{
						Locator: "addp://engine/1/path/public/users?type=table",
						Label:   "users",
						Type:    "table",
					},
				},
			},
		},
	}

	tests := []struct {
		name     string
		locator  string
		wantNil  bool
		wantType string
	}{
		{
			name:     "查找根节点",
			locator:  "addp://engine/1/path/?type=database",
			wantNil:  false,
			wantType: "engine",
		},
		{
			name:     "查找 schema",
			locator:  "addp://engine/1/path/public?type=schema",
			wantNil:  false,
			wantType: "schema",
		},
		{
			name:     "查找表",
			locator:  "addp://engine/1/path/public/users?type=table",
			wantNil:  false,
			wantType: "table",
		},
		{
			name:     "查找不存在的节点",
			locator:  "addp://engine/1/path/private/data?type=table",
			wantNil:  true,
			wantType: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := GetNodeByLocator(tree, tt.locator)
			if (got == nil) != tt.wantNil {
				t.Errorf("GetNodeByLocator() = %v, wantNil %v", got, tt.wantNil)
				return
			}
			if !tt.wantNil && got.Type != tt.wantType {
				t.Errorf("GetNodeByLocator().Type = %s, want %s", got.Type, tt.wantType)
			}
		})
	}
}

func TestFilterTreeByType(t *testing.T) {
	// 构建测试树
	tree := &TreeNode{
		Locator: "addp://engine/1/path/?type=database",
		Label:   "Root",
		Type:    "engine",
		Children: []*TreeNode{
			{
				Locator: "addp://engine/1/path/public?type=schema",
				Label:   "public",
				Type:    "schema",
				Children: []*TreeNode{
					{
						Locator: "addp://engine/1/path/public/users?type=table",
						Label:   "users",
						Type:    "table",
					},
					{
						Locator: "addp://engine/1/path/public/orders?type=table",
						Label:   "orders",
						Type:    "table",
					},
				},
			},
			{
				Locator: "addp://engine/1/path/private?type=schema",
				Label:   "private",
				Type:    "schema",
				Children: []*TreeNode{
					{
						Locator: "addp://engine/1/path/private/logs?type=table",
						Label:   "logs",
						Type:    "table",
					},
				},
			},
		},
	}

	// 过滤只保留 schema 和 table
	filtered := FilterTreeByType(tree, []string{"schema", "table"})

	// 验证根节点仍存在（engine 类型）
	if filtered.Type != "engine" {
		t.Errorf("filtered.Type = %s, want %s", filtered.Type, "engine")
	}

	// 验证 schema 节点仍存在
	if len(filtered.Children) != 2 {
		t.Errorf("len(filtered.Children) = %d, want 2", len(filtered.Children))
	}

	// 验证 table 节点仍存在
	if len(filtered.Children) > 0 && len(filtered.Children[0].Children) != 2 {
		t.Errorf("len(filtered.Children[0].Children) = %d, want 2", len(filtered.Children[0].Children))
	}
}

func TestEngineIcon(t *testing.T) {
	tests := []struct {
		engineType string
		want       string
	}{
		{"postgresql", "Database"},
		{"mysql", "Database"},
		{"MongoDB", "DocumentText"},
		{"minio", "FolderOpen"},
		{"python_workflow", "Grid"},
		{"unknown", "Database"},
	}

	for _, tt := range tests {
		t.Run(tt.engineType, func(t *testing.T) {
			got := EngineIcon(&models.Engine{EngineType: tt.engineType})
			if got != tt.want {
				t.Errorf("EngineIcon(%s) = %s, want %s", tt.engineType, got, tt.want)
			}
		})
	}
}

func TestGetIconByType(t *testing.T) {
	tests := []struct {
		nodeType string
		want     string
	}{
		{"database", "Database"},
		{"schema", "Folder"},
		{"bucket", "FolderOpen"},
		{"table", "Table"},
		{"collection", "DocumentText"},
		{"object", "Document"},
		{"unknown", "Document"},
	}

	for _, tt := range tests {
		t.Run(tt.nodeType, func(t *testing.T) {
			got := getIconByType(tt.nodeType)
			if got != tt.want {
				t.Errorf("getIconByType(%s) = %s, want %s", tt.nodeType, got, tt.want)
			}
		})
	}
}

// 辅助函数

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
