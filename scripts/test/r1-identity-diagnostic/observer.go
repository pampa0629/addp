// Temporary diagnostic compiled separately from the pinned fixture.
package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"time"

	"github.com/addp/system/internal/config"
	"github.com/addp/system/internal/migration"
	_ "github.com/lib/pq"
	"golang.org/x/crypto/bcrypt"
)

func run() error {
	dsn := config.Load().PostgreSQLDSN()
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if os.Args[1] == "migrate" {
		return migration.NewRunner(dsn).Run(ctx)
	}
	raw, err := os.ReadFile(os.Args[2])
	if err != nil {
		return err
	}
	var v map[string]string
	if err := json.Unmarshal(raw, &v); err != nil {
		return err
	}
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return err
	}
	defer db.Close()
	var hash string
	err = db.QueryRowContext(ctx, `SELECT c.client_secret_hash FROM system.oauth_clients c
        JOIN system.principals p ON p.id=c.service_principal_id
        JOIN system.tenant_memberships m ON m.principal_id=p.id
        WHERE c.client_id=$1 AND p.id=$2 AND m.id=$3 AND m.tenant_id=$4
        AND p.principal_type='service_principal' AND p.status='active' AND m.status='active'
        AND c.status='active' AND NOT EXISTS (SELECT 1 FROM system.role_assignments a WHERE a.principal_id=p.id)`,
		v["ADDP_REDIS_HTTP_CLIENT_ID"], v["ADDP_REDIS_HTTP_ACCOUNT_ID"], v["ADDP_REDIS_HTTP_MEMBERSHIP_ID"],
		v["ADDP_ONLINE_TEST_TENANT_ID"]).Scan(&hash)
	if err != nil {
		return err
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(v["ADDP_REDIS_HTTP_CLIENT_SECRET"]))
}
func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "observer operation failed")
		os.Exit(1)
	}
}
