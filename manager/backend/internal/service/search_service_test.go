package service

import "testing"

func TestMapMeilisearchHitUsesItemFingerprintAsDocumentID(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id":  "item-fingerprint",
		"asset_id":     "item-fingerprint",
		"content_hash": "content-sha256",
		"engine_id":    float64(9),
		"engine_type":  "minio",
		"asset_type":   "object",
		"full_name":    "addp/reports/report.docx",
		"name":         "report.docx",
	})

	if doc.DocumentID != "item-fingerprint" {
		t.Fatalf("DocumentID = %q, want item fingerprint", doc.DocumentID)
	}
	if doc.AssetID != "item-fingerprint" {
		t.Fatalf("AssetID = %q, want item fingerprint", doc.AssetID)
	}
	if doc.FileName != "report.docx" {
		t.Fatalf("FileName = %q, want report.docx", doc.FileName)
	}
	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object" {
		t.Fatalf("Locator = %q, want object locator", doc.Locator)
	}
}

func TestMapMeilisearchHitPrefersIndexedLocator(t *testing.T) {
	t.Parallel()

	doc := mapMeilisearchHit(map[string]interface{}{
		"document_id": "item-fingerprint",
		"asset_id":    "item-fingerprint",
		"locator":     "addp://engine/9/path/addp/reports/report.docx?type=object&meta_id=7",
		"engine_id":   float64(9),
		"asset_type":  "object",
		"full_name":   "wrong/path.txt",
		"name":        "report.docx",
	})

	if doc.Locator != "addp://engine/9/path/addp/reports/report.docx?type=object&meta_id=7" {
		t.Fatalf("Locator = %q, want indexed locator", doc.Locator)
	}
}

func TestBuildSearchDocumentLocatorForCoreEngineTypes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		doc  SearchDocument
		want string
	}{
		{
			name: "postgres table",
			doc: SearchDocument{
				EngineID:  1,
				AssetType: "table",
				FullName:  "public.users",
			},
			want: "addp://engine/1/path/public/users?type=table",
		},
		{
			name: "mongodb collection",
			doc: SearchDocument{
				EngineID:  2,
				AssetType: "collection",
				FullName:  "business.orders",
			},
			want: "addp://engine/2/path/business/orders?type=collection",
		},
		{
			name: "neo4j graph",
			doc: SearchDocument{
				EngineID:  3,
				AssetType: "graph",
				FullName:  "neo4j",
			},
			want: "addp://engine/3/path/neo4j?type=graph",
		},
		{
			name: "object storage object",
			doc: SearchDocument{
				EngineID:  4,
				AssetType: "object",
				FullName:  "bucket/dir/file.csv",
			},
			want: "addp://engine/4/path/bucket/dir/file.csv?type=object",
		},
		{
			name: "filesystem file",
			doc: SearchDocument{
				EngineID:  5,
				AssetType: "file",
				FullName:  "/exports/file.csv",
			},
			want: "addp://engine/5/path/exports/file.csv?type=file",
		},
		{
			name: "fallback storage parts",
			doc: SearchDocument{
				EngineID:  6,
				AssetType: "object",
				Bucket:    "addp",
				Path:      "reports/",
				Name:      "a.docx",
			},
			want: "addp://engine/6/path/addp/reports/a.docx?type=object",
		},
		{
			name: "metadata item type",
			doc: SearchDocument{
				EngineID: 7,
				FullName: "data/file.csv",
				Metadata: map[string]interface{}{
					"item": map[string]interface{}{
						"type": "file",
					},
				},
			},
			want: "addp://engine/7/path/data/file.csv?type=file",
		},
		{
			name: "missing type does not guess",
			doc: SearchDocument{
				EngineID: 8,
				FullName: "addp/reports/a.docx",
			},
			want: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := buildSearchDocumentLocator(tt.doc); got != tt.want {
				t.Fatalf("locator = %q, want %q", got, tt.want)
			}
		})
	}
}
