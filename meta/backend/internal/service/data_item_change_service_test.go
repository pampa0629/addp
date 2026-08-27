package service

import (
	"errors"
	"testing"
	"time"

	metaErrors "github.com/addp/meta/internal/errors"
	"github.com/addp/meta/internal/metatest"
)

func TestDataItemChangeCursorRoundTrip(t *testing.T) {
	for _, id := range []int64{0, 1, 42, 9223372036854775807} {
		cursor := EncodeDataItemChangeCursor(id)
		got, err := DecodeDataItemChangeCursor(cursor)
		if err != nil {
			t.Fatalf("decode cursor for %d: %v", id, err)
		}
		if got != id {
			t.Fatalf("decoded cursor = %d, want %d", got, id)
		}
	}
	if got, err := DecodeDataItemChangeCursor(""); err != nil || got != 0 {
		t.Fatalf("empty cursor = (%d, %v), want (0, nil)", got, err)
	}
	for _, cursor := range []string{"!", EncodeDataItemChangeCursor(-1), "MDE"} {
		if _, err := DecodeDataItemChangeCursor(cursor); !errors.Is(err, metaErrors.ErrInvalidChangeCursor) {
			t.Fatalf("cursor %q error = %v, want ErrInvalidChangeCursor", cursor, err)
		}
	}
}

func TestDataItemChangeServiceListsTenantChangesWithStableCursor(t *testing.T) {
	db := metatest.OpenMetadataDB(t)
	if err := db.Exec(`CREATE TABLE meta.data_item_changes (
		id INTEGER PRIMARY KEY AUTOINCREMENT,
		tenant_id INTEGER NOT NULL,
		item_id INTEGER NOT NULL,
		source_identity TEXT NOT NULL,
		operation TEXT NOT NULL,
		snapshot JSON NOT NULL,
		observed_at DATETIME NOT NULL
	)`).Error; err != nil {
		t.Fatalf("create data_item_changes: %v", err)
	}
	now := time.Date(2026, 8, 26, 10, 0, 0, 0, time.UTC)
	for _, row := range []struct {
		tenantID  uint
		identity  string
		operation string
	}{
		{tenantID: 7, identity: "fingerprint-a", operation: "upsert"},
		{tenantID: 8, identity: "other-tenant", operation: "upsert"},
		{tenantID: 7, identity: "fingerprint-b", operation: "missing"},
	} {
		if err := db.Exec(`INSERT INTO meta.data_item_changes
			(tenant_id, item_id, source_identity, operation, snapshot, observed_at)
			VALUES (?, 1, ?, ?, '{"name":"orders"}', ?)`, row.tenantID, row.identity, row.operation, now).Error; err != nil {
			t.Fatalf("insert change: %v", err)
		}
	}

	service := NewDataItemChangeService(db)
	first, err := service.List(7, "", 1)
	if err != nil {
		t.Fatalf("list first page: %v", err)
	}
	if len(first.Changes) != 1 || first.Changes[0].SourceIdentity != "fingerprint-a" || !first.HasMore {
		t.Fatalf("first page = %#v", first)
	}
	if first.Changes[0].SourceVersion != "00000000000000000001" {
		t.Fatalf("source version = %q, want sortable owner sequence", first.Changes[0].SourceVersion)
	}

	second, err := service.List(7, first.NextCursor, 1)
	if err != nil {
		t.Fatalf("list second page: %v", err)
	}
	if len(second.Changes) != 1 || second.Changes[0].SourceIdentity != "fingerprint-b" || second.HasMore {
		t.Fatalf("second page = %#v", second)
	}
	if second.Changes[0].Snapshot["name"] != "orders" {
		t.Fatalf("snapshot = %#v", second.Changes[0].Snapshot)
	}
}
