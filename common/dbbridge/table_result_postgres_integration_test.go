package dbbridge

import (
	"context"
	"github.com/google/uuid"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"os"
	"strings"
	"testing"
	"time"
)

func TestTableResultTransactionsAgainstPostgres(t *testing.T) {
	dsn := os.Getenv("ADDP_TEST_TABLE_RESULT_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("ADDP_TEST_TABLE_RESULT_POSTGRES_DSN is not set")
	}
	db, err := gorm.Open(postgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	schema := "result_" + strings.ReplaceAll(uuid.NewString(), "-", "")[:12]
	must := func(sql string) {
		t.Helper()
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	must("CREATE SCHEMA " + schema)
	t.Cleanup(func() { db.Exec("DROP SCHEMA " + schema + " CASCADE"); pool, _ := db.DB(); pool.Close() })
	target := `"` + schema + `"."target"`
	must("CREATE TABLE " + target + " (id int PRIMARY KEY)")
	must("INSERT INTO " + target + " VALUES (99)")
	write := func(ctx context.Context, query, mode string) error {
		_, err := executeTableResultTransaction(ctx, db, target, "INSERT INTO "+target+" "+query, nil, mode)
		return err
	}
	check := func(want string) {
		t.Helper()
		var got string
		if err := db.Raw("SELECT COALESCE(string_agg(id::text, ',' ORDER BY id),'') FROM " + target).Scan(&got).Error; err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("rows=%s want=%s", got, want)
		}
	}
	if err := write(context.Background(), "SELECT 1 UNION ALL SELECT 1", "overwrite"); err == nil {
		t.Fatal("duplicate key should fail")
	}
	check("99")
	for i := 0; i < 2; i++ {
		if err := write(context.Background(), "SELECT 1 UNION ALL SELECT 2", "overwrite"); err != nil {
			t.Fatal(err)
		}
		check("1,2")
	}
	if err := write(context.Background(), "SELECT 3", "append"); err != nil {
		t.Fatal(err)
	}
	check("1,2,3")
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := write(ctx, "SELECT 4 FROM pg_sleep(1)", "overwrite"); err == nil {
		t.Fatal("cancel should fail")
	}
	check("1,2,3")
	// Simultaneous refreshes serialize into one complete result, never mixed partial rows.
	done := make(chan error, 2)
	for _, q := range []string{"SELECT 10 UNION ALL SELECT 11", "SELECT 20 UNION ALL SELECT 21"} {
		go func(q string) { done <- write(context.Background(), q, "overwrite") }(q)
	}
	for i := 0; i < 2; i++ {
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	}
	var sum int
	db.Raw("SELECT sum(id) FROM " + target).Scan(&sum)
	if sum != 21 && sum != 41 {
		t.Fatalf("mixed result: %d", sum)
	}
}
