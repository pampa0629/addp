package plugin

import (
	"context"
	"testing"
	"time"
)

func TestStorageCatalogEntryUsesStorageSummary(t *testing.T) {
	modifiedAt := time.Unix(300, 0)
	size := int64(64)
	parent := ObjectDirectoryPath(7, "addp", "")

	entry := ObjectLeafCatalogEntry(parent, StorageObjectFacts{
		Name:        "orders.csv",
		Path:        "addp/datasets/orders.csv",
		Size:        size,
		ModifiedAt:  modifiedAt,
		ContentType: "text/csv",
		ETag:        "etag-1",
	})

	if entry.Storage == nil {
		t.Fatal("Storage is nil")
	}
	if entry.Storage.Path != "addp/datasets/orders.csv" || entry.Storage.ContentType != "text/csv" || entry.Storage.ETag != "etag-1" {
		t.Fatalf("Storage = %#v", entry.Storage)
	}
	if entry.Storage.Name != "" {
		t.Fatalf("entry storage name = %q, want empty", entry.Storage.Name)
	}
	if entry.Storage.Extension != "" {
		t.Fatalf("entry storage extension = %q, want empty", entry.Storage.Extension)
	}
	if entry.Storage.SizeBytes == nil || *entry.Storage.SizeBytes != size {
		t.Fatalf("Storage.SizeBytes = %#v, want %d", entry.Storage.SizeBytes, size)
	}
	if entry.UpdatedAt == nil || !entry.UpdatedAt.Equal(modifiedAt) {
		t.Fatalf("UpdatedAt = %#v, want %v", entry.UpdatedAt, modifiedAt)
	}
}

func TestStorageCatalogFactsCarryStorageDetails(t *testing.T) {
	modifiedAt := time.Unix(400, 0)
	callbacks := ObjectCatalogCallbacks{
		GetObjectStorageFactsFunc: func(context.Context, ConnectionInfo, string) (*StorageObjectFacts, error) {
			return &StorageObjectFacts{
				Name:        "orders.csv",
				Path:        "addp/datasets/orders.csv",
				Size:        64,
				ModifiedAt:  modifiedAt,
				ContentType: "text/csv",
				ETag:        "etag-1",
			}, nil
		},
	}

	facts, err := DescribeObjectCatalogFacts(context.Background(), callbacks, nil, 7, ObjectItemPath(7, "addp", "datasets/orders.csv"))
	if err != nil {
		t.Fatalf("DescribeObjectCatalogFacts() error = %v", err)
	}
	if facts.Storage == nil {
		t.Fatal("Storage is nil")
	}
	if facts.Storage.Name != "orders.csv" {
		t.Fatalf("facts storage name = %q, want orders.csv", facts.Storage.Name)
	}
	if facts.Storage.Extension != ".csv" {
		t.Fatalf("facts storage extension = %q, want .csv", facts.Storage.Extension)
	}
}
