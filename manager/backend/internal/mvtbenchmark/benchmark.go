package mvtbenchmark

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/addp/common/spatial"
	_ "github.com/lib/pq"
)

const (
	DefaultIterations  = 5
	DefaultWarmup      = 0
	DefaultConcurrency = 1
	DefaultExtent      = 1024
	DefaultTimeoutMS   = 3000
)

type Config struct {
	DSN            string     `json:"dsn,omitempty"`
	Iterations     int        `json:"iterations,omitempty"`
	Warmup         int        `json:"warmup,omitempty"`
	Concurrency    int        `json:"concurrency,omitempty"`
	TimeoutMS      int        `json:"timeout_ms,omitempty"`
	ExplainAnalyze bool       `json:"explain_analyze,omitempty"`
	Scenarios      []Scenario `json:"scenarios"`
}

type Scenario struct {
	Name           string            `json:"name"`
	TargetKind     string            `json:"target_kind,omitempty"`
	Schema         string            `json:"schema"`
	Table          string            `json:"table"`
	GeometryColumn string            `json:"geometry_column"`
	PrimaryKey     string            `json:"primary_key,omitempty"`
	Layer          string            `json:"layer,omitempty"`
	SRID           int               `json:"srid"`
	Columns        []string          `json:"columns,omitempty"`
	Extent         int               `json:"extent,omitempty"`
	Buffer         int               `json:"buffer,omitempty"`
	Tiles          []TileCoord       `json:"tiles"`
	Tags           map[string]string `json:"tags,omitempty"`
}

type TileCoord struct {
	Z int `json:"z"`
	X int `json:"x"`
	Y int `json:"y"`
}

type Report struct {
	StartedAt  time.Time        `json:"started_at"`
	FinishedAt time.Time        `json:"finished_at"`
	Config     ConfigSummary    `json:"config"`
	Scenarios  []ScenarioReport `json:"scenarios"`
	DBStats    DBStats          `json:"db_stats"`
}

type ConfigSummary struct {
	Iterations     int  `json:"iterations"`
	Warmup         int  `json:"warmup"`
	Concurrency    int  `json:"concurrency"`
	TimeoutMS      int  `json:"timeout_ms"`
	ExplainAnalyze bool `json:"explain_analyze"`
	ScenarioCount  int  `json:"scenario_count"`
	DSNSet         bool `json:"dsn_set"`
}

type ScenarioReport struct {
	Name       string            `json:"name"`
	TargetKind string            `json:"target_kind,omitempty"`
	RenderPath string            `json:"render_path"`
	Schema     string            `json:"schema"`
	Table      string            `json:"table"`
	GeomColumn string            `json:"geometry_column"`
	SRID       int               `json:"srid"`
	Tags       map[string]string `json:"tags,omitempty"`
	Tiles      []TileReport      `json:"tiles"`
	Summary    MetricSummary     `json:"summary"`
	Warnings   []string          `json:"warnings,omitempty"`
}

type TileReport struct {
	Tile     TileCoord     `json:"tile"`
	Summary  MetricSummary `json:"summary"`
	Runs     []RunReport   `json:"runs"`
	Explain  []string      `json:"explain,omitempty"`
	Warnings []string      `json:"warnings,omitempty"`
}

type RunReport struct {
	Iteration  int     `json:"iteration"`
	DurationMS float64 `json:"duration_ms"`
	SizeBytes  int     `json:"size_bytes"`
	Empty      bool    `json:"empty"`
	TimedOut   bool    `json:"timed_out,omitempty"`
	Error      string  `json:"error,omitempty"`
}

type MetricSummary struct {
	Runs           int     `json:"runs"`
	SuccessfulRuns int     `json:"successful_runs"`
	ErrorRuns      int     `json:"error_runs"`
	TimeoutRuns    int     `json:"timeout_runs"`
	EmptyRuns      int     `json:"empty_runs"`
	MinDurationMS  float64 `json:"min_duration_ms"`
	P50DurationMS  float64 `json:"p50_duration_ms"`
	P95DurationMS  float64 `json:"p95_duration_ms"`
	P99DurationMS  float64 `json:"p99_duration_ms"`
	MaxDurationMS  float64 `json:"max_duration_ms"`
	AvgSizeBytes   float64 `json:"avg_size_bytes"`
	MaxSizeBytes   int     `json:"max_size_bytes"`
	RawMVTSizeP95B float64 `json:"raw_mvt_size_p95_bytes"`
	ErrorRate      float64 `json:"error_rate"`
	TimeoutRate    float64 `json:"timeout_rate"`
	EmptyTileRate  float64 `json:"empty_tile_rate"`
}

type DBStats struct {
	MaxOpenConnections int   `json:"max_open_connections"`
	OpenConnections    int   `json:"open_connections"`
	InUse              int   `json:"in_use"`
	Idle               int   `json:"idle"`
	WaitCount          int64 `json:"wait_count"`
	WaitDurationMS     int64 `json:"wait_duration_ms"`
	MaxIdleClosed      int64 `json:"max_idle_closed"`
	MaxIdleTimeClosed  int64 `json:"max_idle_time_closed"`
	MaxLifetimeClosed  int64 `json:"max_lifetime_closed"`
}

type Executor interface {
	ExecuteTile(ctx context.Context, query string, args []any) ([]byte, error)
	Explain(ctx context.Context, query string, args []any) ([]string, error)
	Stats() DBStats
	Close() error
}

type SQLExecutor struct {
	db *sql.DB
}

func OpenSQLExecutor(ctx context.Context, dsn string, maxOpenConns int) (*SQLExecutor, error) {
	db, err := sql.Open("postgres", dsn)
	if err != nil {
		return nil, err
	}
	if maxOpenConns < 1 {
		maxOpenConns = DefaultConcurrency
	}
	db.SetMaxOpenConns(maxOpenConns)
	db.SetMaxIdleConns(maxOpenConns)
	db.SetConnMaxLifetime(30 * time.Minute)
	if err := db.PingContext(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return &SQLExecutor{db: db}, nil
}

func (e *SQLExecutor) ExecuteTile(ctx context.Context, query string, args []any) ([]byte, error) {
	var tile []byte
	if err := e.db.QueryRowContext(ctx, query, args...).Scan(&tile); err != nil {
		return nil, err
	}
	if tile == nil {
		return []byte{}, nil
	}
	return tile, nil
}

func (e *SQLExecutor) Explain(ctx context.Context, query string, args []any) ([]string, error) {
	rows, err := e.db.QueryContext(ctx, "EXPLAIN (ANALYZE, BUFFERS, FORMAT TEXT) "+query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var lines []string
	for rows.Next() {
		var line string
		if err := rows.Scan(&line); err != nil {
			return nil, err
		}
		lines = append(lines, line)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return lines, nil
}

func (e *SQLExecutor) Stats() DBStats {
	stats := e.db.Stats()
	return DBStats{
		MaxOpenConnections: stats.MaxOpenConnections,
		OpenConnections:    stats.OpenConnections,
		InUse:              stats.InUse,
		Idle:               stats.Idle,
		WaitCount:          stats.WaitCount,
		WaitDurationMS:     stats.WaitDuration.Milliseconds(),
		MaxIdleClosed:      stats.MaxIdleClosed,
		MaxIdleTimeClosed:  stats.MaxIdleTimeClosed,
		MaxLifetimeClosed:  stats.MaxLifetimeClosed,
	}
}

func (e *SQLExecutor) Close() error {
	return e.db.Close()
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	return NormalizeConfig(cfg), nil
}

func NormalizeConfig(cfg Config) Config {
	if cfg.Iterations < 1 {
		cfg.Iterations = DefaultIterations
	}
	if cfg.Warmup < 0 {
		cfg.Warmup = DefaultWarmup
	}
	if cfg.Concurrency < 1 {
		cfg.Concurrency = DefaultConcurrency
	}
	if cfg.TimeoutMS < 1 {
		cfg.TimeoutMS = DefaultTimeoutMS
	}
	for i := range cfg.Scenarios {
		if cfg.Scenarios[i].Layer == "" {
			cfg.Scenarios[i].Layer = cfg.Scenarios[i].Table
		}
		if cfg.Scenarios[i].Extent < 1 {
			cfg.Scenarios[i].Extent = DefaultExtent
		}
		if cfg.Scenarios[i].Buffer < 1 {
			cfg.Scenarios[i].Buffer = mvtBufferForExtent(cfg.Scenarios[i].Extent)
		}
	}
	return cfg
}

func ValidateConfig(cfg Config) error {
	if strings.TrimSpace(cfg.DSN) == "" {
		return errors.New("dsn is required")
	}
	if len(cfg.Scenarios) == 0 {
		return errors.New("at least one scenario is required")
	}
	for i, scenario := range cfg.Scenarios {
		prefix := fmt.Sprintf("scenarios[%d]", i)
		if strings.TrimSpace(scenario.Name) == "" {
			return fmt.Errorf("%s.name is required", prefix)
		}
		if strings.TrimSpace(scenario.Schema) == "" {
			return fmt.Errorf("%s.schema is required", prefix)
		}
		if strings.TrimSpace(scenario.Table) == "" {
			return fmt.Errorf("%s.table is required", prefix)
		}
		if strings.TrimSpace(scenario.GeometryColumn) == "" {
			return fmt.Errorf("%s.geometry_column is required", prefix)
		}
		if scenario.SRID < 1 {
			return fmt.Errorf("%s.srid is required", prefix)
		}
		if len(scenario.Tiles) == 0 {
			return fmt.Errorf("%s.tiles must not be empty", prefix)
		}
		for tileIndex, tile := range scenario.Tiles {
			if tile.Z < 0 || tile.X < 0 || tile.Y < 0 {
				return fmt.Errorf("%s.tiles[%d] must use non-negative z/x/y", prefix, tileIndex)
			}
		}
	}
	return nil
}

func Run(ctx context.Context, cfg Config, executor Executor) (Report, error) {
	cfg = NormalizeConfig(cfg)
	if err := ValidateConfig(cfg); err != nil {
		return Report{}, err
	}
	if executor == nil {
		return Report{}, errors.New("executor is required")
	}

	report := Report{
		StartedAt: time.Now().UTC(),
		Config: ConfigSummary{
			Iterations:     cfg.Iterations,
			Warmup:         cfg.Warmup,
			Concurrency:    cfg.Concurrency,
			TimeoutMS:      cfg.TimeoutMS,
			ExplainAnalyze: cfg.ExplainAnalyze,
			ScenarioCount:  len(cfg.Scenarios),
			DSNSet:         strings.TrimSpace(cfg.DSN) != "",
		},
		Scenarios: make([]ScenarioReport, 0, len(cfg.Scenarios)),
	}

	for _, scenario := range cfg.Scenarios {
		scenarioReport, err := runScenario(ctx, cfg, scenario, executor)
		if err != nil {
			return Report{}, err
		}
		report.Scenarios = append(report.Scenarios, scenarioReport)
	}

	report.FinishedAt = time.Now().UTC()
	report.DBStats = executor.Stats()
	return report, nil
}

func runScenario(ctx context.Context, cfg Config, scenario Scenario, executor Executor) (ScenarioReport, error) {
	scenario = NormalizeConfig(Config{Scenarios: []Scenario{scenario}}).Scenarios[0]
	query, args := buildScenarioQuery(scenario)

	for _, tile := range scenario.Tiles {
		for i := 0; i < cfg.Warmup; i++ {
			if err := executeWarmup(ctx, cfg.TimeoutMS, executor, query, args, tile); err != nil {
				return ScenarioReport{}, fmt.Errorf("warmup %s z%d/%d/%d: %w", scenario.Name, tile.Z, tile.X, tile.Y, err)
			}
		}
	}

	tileReports := make([]TileReport, len(scenario.Tiles))
	for i, tile := range scenario.Tiles {
		tileReport, err := runTile(ctx, cfg, scenario, query, args, tile, executor)
		if err != nil {
			return ScenarioReport{}, err
		}
		tileReports[i] = tileReport
	}

	allRuns := make([]RunReport, 0, len(tileReports)*cfg.Iterations)
	for _, tileReport := range tileReports {
		allRuns = append(allRuns, tileReport.Runs...)
	}
	return ScenarioReport{
		Name:       scenario.Name,
		TargetKind: scenario.TargetKind,
		RenderPath: renderPath(scenario),
		Schema:     scenario.Schema,
		Table:      scenario.Table,
		GeomColumn: scenario.GeometryColumn,
		SRID:       scenario.SRID,
		Tags:       scenario.Tags,
		Tiles:      tileReports,
		Summary:    summarizeRuns(allRuns),
		Warnings:   scenarioWarnings(scenario),
	}, nil
}

func runTile(ctx context.Context, cfg Config, scenario Scenario, query string, baseArgs []any, tile TileCoord, executor Executor) (TileReport, error) {
	runs := make([]RunReport, cfg.Iterations)
	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, cfg.Concurrency)

	for i := 0; i < cfg.Iterations; i++ {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			run := executeMeasuredTile(ctx, cfg.TimeoutMS, executor, query, baseArgs, tile, i+1)
			mu.Lock()
			runs[i] = run
			mu.Unlock()
		}()
	}
	wg.Wait()

	tileReport := TileReport{
		Tile:    tile,
		Summary: summarizeRuns(runs),
		Runs:    runs,
	}
	if cfg.ExplainAnalyze {
		explainCtx, cancel := context.WithTimeout(ctx, time.Duration(cfg.TimeoutMS)*time.Millisecond)
		defer cancel()
		explain, err := executor.Explain(explainCtx, query, tileArgs(baseArgs, tile))
		if err != nil {
			tileReport.Warnings = append(tileReport.Warnings, "explain_analyze_failed: "+err.Error())
		} else {
			tileReport.Explain = explain
		}
	}
	return tileReport, nil
}

func executeWarmup(ctx context.Context, timeoutMS int, executor Executor, query string, baseArgs []any, tile TileCoord) error {
	run := executeMeasuredTile(ctx, timeoutMS, executor, query, baseArgs, tile, 0)
	if run.Error != "" {
		return errors.New(run.Error)
	}
	return nil
}

func executeMeasuredTile(ctx context.Context, timeoutMS int, executor Executor, query string, baseArgs []any, tile TileCoord, iteration int) RunReport {
	queryCtx, cancel := context.WithTimeout(ctx, time.Duration(timeoutMS)*time.Millisecond)
	defer cancel()

	start := time.Now()
	data, err := executor.ExecuteTile(queryCtx, query, tileArgs(baseArgs, tile))
	duration := float64(time.Since(start).Microseconds()) / 1000
	report := RunReport{
		Iteration:  iteration,
		DurationMS: duration,
		SizeBytes:  len(data),
		Empty:      len(data) == 0,
	}
	if err != nil {
		report.Error = err.Error()
		report.TimedOut = isTimeoutError(err) || errors.Is(queryCtx.Err(), context.DeadlineExceeded)
	}
	return report
}

func buildScenarioQuery(scenario Scenario) (string, []any) {
	opt := spatial.MVTOptions{
		Layer:  scenario.Layer,
		Extent: scenario.Extent,
		Buffer: scenario.Buffer,
		SRID:   scenario.SRID,
	}
	query, args := spatial.BuildMVTQuery(
		scenario.Schema,
		scenario.Table,
		scenario.GeometryColumn,
		scenario.Columns,
		0, 0, 0,
		opt,
		scenario.PrimaryKey,
	)
	return query, args
}

func tileArgs(baseArgs []any, tile TileCoord) []any {
	args := append([]any(nil), baseArgs...)
	args[0] = tile.Z
	args[1] = tile.X
	args[2] = tile.Y
	return args
}

func summarizeRuns(runs []RunReport) MetricSummary {
	summary := MetricSummary{Runs: len(runs)}
	if len(runs) == 0 {
		return summary
	}

	durations := make([]float64, 0, len(runs))
	sizes := make([]float64, 0, len(runs))
	sizeTotal := 0
	for _, run := range runs {
		if run.Error != "" {
			summary.ErrorRuns++
			if run.TimedOut {
				summary.TimeoutRuns++
			}
			continue
		}
		summary.SuccessfulRuns++
		if run.Empty {
			summary.EmptyRuns++
		}
		durations = append(durations, run.DurationMS)
		sizes = append(sizes, float64(run.SizeBytes))
		sizeTotal += run.SizeBytes
		if run.SizeBytes > summary.MaxSizeBytes {
			summary.MaxSizeBytes = run.SizeBytes
		}
	}

	if len(durations) > 0 {
		sort.Float64s(durations)
		summary.MinDurationMS = durations[0]
		summary.P50DurationMS = percentile(durations, 50)
		summary.P95DurationMS = percentile(durations, 95)
		summary.P99DurationMS = percentile(durations, 99)
		summary.MaxDurationMS = durations[len(durations)-1]
	}
	if summary.SuccessfulRuns > 0 {
		sort.Float64s(sizes)
		summary.AvgSizeBytes = float64(sizeTotal) / float64(summary.SuccessfulRuns)
		summary.RawMVTSizeP95B = percentile(sizes, 95)
	}
	summary.ErrorRate = rate(summary.ErrorRuns, summary.Runs)
	summary.TimeoutRate = rate(summary.TimeoutRuns, summary.Runs)
	summary.EmptyTileRate = rate(summary.EmptyRuns, summary.SuccessfulRuns)
	return summary
}

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	if len(sorted) == 1 {
		return sorted[0]
	}
	rank := (p / 100) * float64(len(sorted)-1)
	low := int(math.Floor(rank))
	high := int(math.Ceil(rank))
	if low == high {
		return sorted[low]
	}
	weight := rank - float64(low)
	return sorted[low]*(1-weight) + sorted[high]*weight
}

func rate(part, total int) float64 {
	if total == 0 {
		return 0
	}
	return float64(part) / float64(total)
}

func renderPath(scenario Scenario) string {
	if scenario.SRID == 3857 {
		if scenario.TargetKind != "" {
			return scenario.TargetKind
		}
		return "web_mercator_target"
	}
	return "source_transform_path"
}

func scenarioWarnings(scenario Scenario) []string {
	var warnings []string
	if scenario.SRID != 3857 {
		warnings = append(warnings, "source geometry will be transformed to EPSG:3857 inside each tile query")
	}
	if scenario.TargetKind == "ready_3857_optimization" && scenario.SRID != 3857 {
		warnings = append(warnings, "ready_3857_optimization target should normally use srid=3857")
	}
	return warnings
}

func isTimeoutError(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "canceling statement") ||
		strings.Contains(msg, "statement timeout")
}

func mvtBufferForExtent(extent int) int {
	buffer := extent / 32
	if buffer < 8 {
		return 8
	}
	if buffer > 64 {
		return 64
	}
	return buffer
}
