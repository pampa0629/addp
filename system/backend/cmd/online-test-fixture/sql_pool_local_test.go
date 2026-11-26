package main

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestLocalSQLPoolFixtureRequiresAnExclusiveProfileAndDatabase(t *testing.T) {
	directory := filepath.Join(t.TempDir(), "private")
	if err := os.Mkdir(directory, 0700); err != nil {
		t.Fatal(err)
	}
	output := filepath.Join(directory, "identities.env")
	env := []string{sqlPoolLocalRun + "=addp-sql-pool-local-" + strings.Repeat("a", 32), "POSTGRES_DB=addp_test", "POSTGRES_HOST=127.0.0.1"}
	const suite = "sql-pool-consumer-flow"
	if err := validateExternalEnvironment(env, output, suite); err != nil {
		t.Fatal(err)
	}
	for _, change := range []string{"POSTGRES_DB=addp_online", "POSTGRES_HOST=other", "GITHUB_ACTIONS=true", "ADDP_ONLINE_HOSTED=1"} {
		if err := validateExternalEnvironment(append(slices.Clone(env), change), output, suite); err == nil {
			t.Fatalf("local fixture accepted %s", change)
		}
	}
	if err := validateExternalEnvironment(env, output, "ftp-consumer-flow"); err == nil {
		t.Fatal("local SQL pool profile accepted another suite")
	}
	if err := validateExternalEnvironment(env, "identities.env", suite); err == nil {
		t.Fatal("local SQL pool profile accepted an unowned output")
	}
}
