package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"github.com/addp/engines/duckdb/internal/duckdb"
)

func main() {
	output := flag.String("output", ".cache/duckdb/extensions", "extension output directory")
	platform := flag.String("platform", "", "DuckDB extension platform; defaults to the current platform")
	repository := flag.String("repository", duckdb.OfficialExtensionRepository, "DuckDB extension repository")
	verify := flag.Bool("verify", true, "load every extension after download when preparing for the current platform")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	currentPlatform, err := duckdb.CurrentPlatform(ctx)
	if err != nil {
		log.Fatal(err)
	}
	if *platform == "" {
		*platform = currentPlatform
	}
	absoluteOutput, err := filepath.Abs(*output)
	if err != nil {
		log.Fatal(err)
	}
	client := &http.Client{Timeout: 5 * time.Minute}
	if err := duckdb.PrepareRequiredExtensions(ctx, client, *repository, *platform, absoluteOutput); err != nil {
		log.Fatal(err)
	}
	if *verify {
		if *platform != currentPlatform {
			log.Fatalf("cannot verify target platform %s on current platform %s", *platform, currentPlatform)
		}
		if err := os.Setenv("DUCKDB_EXTENSION_DIRECTORY", absoluteOutput); err != nil {
			log.Fatal(err)
		}
		if err := duckdb.VerifyRequiredExtensions(ctx); err != nil {
			log.Fatal(err)
		}
	}
	fmt.Printf("DuckDB %s extensions ready: %s\n", duckdb.DuckDBVersion, absoluteOutput)
}
