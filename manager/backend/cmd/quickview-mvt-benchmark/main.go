package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/addp/manager/internal/mvtbenchmark"
)

func main() {
	configPath := flag.String("config", "", "benchmark config JSON path")
	dsn := flag.String("dsn", "", "PostgreSQL DSN; overrides config.dsn")
	outputPath := flag.String("output", "", "output report path; stdout when empty")
	explainAnalyze := flag.Bool("explain-analyze", false, "run EXPLAIN ANALYZE for each configured tile after measured runs")
	flag.Parse()

	if *configPath == "" {
		fmt.Fprintln(os.Stderr, "-config is required")
		os.Exit(2)
	}

	cfg, err := mvtbenchmark.LoadConfig(*configPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load config: %v\n", err)
		os.Exit(1)
	}
	if *dsn != "" {
		cfg.DSN = *dsn
	}
	if *explainAnalyze {
		cfg.ExplainAnalyze = true
	}
	cfg = mvtbenchmark.NormalizeConfig(cfg)
	if err := mvtbenchmark.ValidateConfig(cfg); err != nil {
		fmt.Fprintf(os.Stderr, "invalid config: %v\n", err)
		os.Exit(2)
	}

	ctx := context.Background()
	executor, err := mvtbenchmark.OpenSQLExecutor(ctx, cfg.DSN, cfg.Concurrency)
	if err != nil {
		fmt.Fprintf(os.Stderr, "connect postgres: %v\n", err)
		os.Exit(1)
	}
	defer executor.Close()

	report, err := mvtbenchmark.Run(ctx, cfg, executor)
	if err != nil {
		fmt.Fprintf(os.Stderr, "run benchmark: %v\n", err)
		os.Exit(1)
	}
	report.FinishedAt = time.Now().UTC()

	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		fmt.Fprintf(os.Stderr, "encode report: %v\n", err)
		os.Exit(1)
	}
	data = append(data, '\n')

	if *outputPath == "" {
		_, _ = os.Stdout.Write(data)
		return
	}
	if err := os.WriteFile(*outputPath, data, 0o644); err != nil {
		fmt.Fprintf(os.Stderr, "write report: %v\n", err)
		os.Exit(1)
	}
}
