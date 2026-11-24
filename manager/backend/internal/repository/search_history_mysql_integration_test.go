package repository

import (
	"fmt"
	"testing"
)

func TestIntegrationMySQLManagerSearchHistory(t *testing.T) {
	db := managerMySQLStoreDB(t, "manager", "ADDP_TEST_MYSQL_MANAGER_STORAGE")
	if err := MigrateSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	ctx := t.Context()
	repo := NewSearchHistoryRepository(db)
	const keep = 4
	start := make(chan struct{})
	done := make(chan error, 12)
	for i := range 12 {
		go func() {
			<-start
			done <- repo.Record(ctx, 7, 31, fmt.Sprintf("term-%02d", i), keep)
		}()
	}
	close(start)
	for range 12 {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	rows, err := repo.List(ctx, 7, 31, 20)
	if err != nil || len(rows) != keep {
		t.Fatalf("concurrent retention: %v, %v", rows, err)
	}
	for _, owner := range [][2]uint{{7, 31}, {7, 32}, {8, 31}} {
		if err := repo.Record(ctx, owner[0], owner[1], "Orders", keep); err != nil {
			t.Fatal(err)
		}
	}
	first, err := repo.List(ctx, 7, 31, 1)
	if err != nil || len(first) != 1 || first[0].Query != "Orders" {
		t.Fatalf("first insert: %v, %v", first, err)
	}
	for _, term := range []string{"orders", "Orders"} {
		if err := repo.Record(ctx, 7, 31, term, keep); err != nil {
			t.Fatal(err)
		}
	}
	if err := MigrateSearchSchema(db); err != nil {
		t.Fatal(err)
	}
	rows, err = repo.List(ctx, 7, 31, 20)
	if err != nil || len(rows) != keep || rows[0].ID != first[0].ID || !rows[0].CreatedAt.Equal(first[0].CreatedAt) || rows[1].Query != "orders" {
		t.Fatalf("repeat after installation: %v, %v", rows, err)
	}
	for _, owner := range [][2]uint{{7, 32}, {8, 31}} {
		if err := repo.DeleteByID(ctx, owner[0], owner[1], first[0].ID); err != nil {
			t.Fatal(err)
		}
	}
	rows, err = repo.List(ctx, 7, 31, 20)
	if err != nil || len(rows) != keep {
		t.Fatalf("foreign deletion: %v, %v", rows, err)
	}
	if err := repo.DeleteAll(ctx, 7, 31); err != nil {
		t.Fatal(err)
	}
	for _, owner := range [][2]uint{{7, 32}, {8, 31}} {
		rows, err = repo.List(ctx, owner[0], owner[1], 20)
		if err != nil || len(rows) != 1 || rows[0].Query != "Orders" {
			t.Fatalf("other owner's history: %v, %v", rows, err)
		}
	}
}
