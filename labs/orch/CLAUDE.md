# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

This directory (`labs/orch`) is an **experimental lab environment** for orchestration and data lineage functionality. It is **isolated from the main ADDP codebase** and used for learning, validation, and prototyping before integration.

**Important**: Do NOT mix functionality from this directory with other ADDP modules during development. This is a standalone testing ground.

## Directory Structure

```
labs/orch/
├── lineage/              # Pure lineage tracking library (zero workflow engine dependencies)
├── lineage-demo/         # Temporal integration example (demonstrates how to integrate lineage)
├── temporal/             # Temporal Server setup (docker-compose)
├── scripts/              # Utility scripts
├── init_test_data.sh     # Initialize test data in PostgreSQL
├── insert_test_data.go   # Go-based test data insertion
└── LINEAGE_REFACTOR.md   # Refactoring documentation
```

## Core Components

### 1. lineage Library (Pure Library)

**Location**: `lineage/`

A workflow engine-agnostic data lineage tracking library with zero external dependencies on Temporal, Airflow, or any orchestration framework.

**Architecture**:
```
lineage/
├── models/              # Data models (lineage.go, types.go)
├── repository/          # Data access layer (GORM-based)
├── service/             # Business logic (LineageRecorder interface, LineageService)
└── storage/             # Database connectors (SQLite, PostgreSQL)
```

**Key Design Principles**:
- **Engine-agnostic**: Uses `ExternalWorkflowID` instead of `TemporalWorkflowID`
- **Zero dependencies**: No workflow engine imports in `go.mod`
- **Strategy pattern**: `LineageRecorder` interface allows multiple implementations
- **Repository pattern**: Clean data access abstraction

**Data Model**:
```go
type DataLineage struct {
    ExternalWorkflowID  string  // Generic workflow ID (not Temporal-specific)
    ExternalExecutionID string  // Generic execution ID
    WorkflowEngine      string  // "temporal" | "airflow" | "manual" | ""

    SourceItemID      uint
    TargetItemID      uint
    LineageType       string    // transform, copy, merge, aggregate
    Status            string    // success, failed, partial

    RecordsProcessed  int64
    BytesWritten      int64
    // ... other fields
}
```

**Usage**:
```go
// Initialize
db, _ := storage.NewSQLiteDB("./lineage.db")
repo := repository.NewLineageRepository(db)
recorder := service.NewLineageService(repo)

// Record lineage
lineage, _ := recorder.Record(ctx, &models.LineageInput{
    SourceItemID: 1,
    TargetItemID: 2,
    LineageType:  "transform",
    Status:       "success",
})

// Query upstream/downstream
upstream, _ := recorder.QueryUpstream(ctx, 2)
downstream, _ := recorder.QueryDownstream(ctx, 1)
```

### 2. lineage-demo (Temporal Integration Example)

**Location**: `lineage-demo/`

Demonstrates how to integrate the pure `lineage` library with Temporal workflows using **dependency injection**.

**Architecture**:
```
lineage-demo/
├── workflow/         # Temporal Workflow + Activity (thin wrapper)
├── worker/           # Worker with dependency injection
├── starter/          # Workflow trigger
├── api/              # REST API for querying lineage
├── web/              # Web UI for visualization
└── frontend/         # Vue.js frontend (optional)
```

**Integration Pattern**:
```
┌─────────────────────────────────────┐
│  Temporal Workflow                  │
│  - Extract Temporal metadata        │
│  - Construct LineageInput           │
│  - Call Activity                    │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│  Activity (Thin Wrapper)            │
│  - Get LineageRecorder from context │
│  - Inject Temporal metadata         │
│  - Call recorder.Record()           │
└──────────────┬──────────────────────┘
               │
┌──────────────▼──────────────────────┐
│  Lineage Library                    │
│  (No Temporal dependencies)         │
└─────────────────────────────────────┘
```

**Dependency Injection in Worker**:
```go
// worker/main.go
lineageService := service.NewLineageService(repo)

activityWrapper := func(ctx context.Context, input interface{}) error {
    ctx = context.WithValue(ctx, "lineage_recorder", lineageService)
    return workflow.RecordLineageActivity(ctx, input.(*models.LineageInput))
}
w.RegisterActivity(activityWrapper)
```

### 3. temporal Directory

**Location**: `temporal/`

Contains `docker-compose.yml` for running Temporal Server locally.

**Usage**:
```bash
cd temporal
docker-compose up -d
```

## Common Commands

### Running the Complete Demo

**Prerequisites**:
- PostgreSQL running (for storing lineage data)
- Temporal Server running (`cd temporal && docker-compose up -d`)

**Step 1: Start Worker**
```bash
cd lineage-demo/worker
go run main.go
```

**Step 2: Trigger Workflow**
```bash
cd lineage-demo/starter
go run main.go
```

**Step 3: Start API Server**
```bash
cd lineage-demo/api
go run server.go
```

**Step 4: Query Lineage**
```bash
# Query all lineage
curl http://localhost:9090/api/lineage/all | jq

# Query upstream lineage
curl http://localhost:9090/api/lineage/upstream/2 | jq

# Query downstream lineage
curl http://localhost:9090/api/lineage/downstream/1 | jq

# Build lineage graph
curl http://localhost:9090/api/lineage/graph/2?max_depth=3 | jq
```

**Step 5: Open Web UI**
- Open `lineage-demo/web/index.html` in browser
- Enter API URL: `http://localhost:9090`

### Testing Commands

```bash
# Test lineage library
cd lineage
go test ./...

# Initialize test data
./init_test_data.sh

# Query lineage using shell script
cd lineage-demo
./query_lineage.sh
```

### Frontend Development

```bash
cd lineage-demo/frontend
npm install
npm run dev
# Access at http://localhost:5173
```

## API Endpoints

- `GET /api/lineage/all` - Query all lineage records
- `GET /api/lineage/upstream/:item_id` - Query upstream lineage
- `GET /api/lineage/downstream/:item_id` - Query downstream lineage
- `GET /api/lineage/graph/:item_id?max_depth=3` - Build lineage graph
- `GET /api/lineage/workflow/:workflow_id` - Query by workflow ID

## Key Architecture Decisions

### 1. Separation of Concerns

**lineage/** is a **pure library** with no workflow engine dependencies. All workflow-specific code lives in **lineage-demo/**.

**Why**: Allows lineage library to be used with:
- Temporal workflows
- Airflow DAGs
- Manual record keeping (no workflow engine)
- Any other orchestration framework

### 2. Dependency Injection

Activities receive `LineageRecorder` through `context.Context` rather than global variables.

**Benefits**:
- Testable (can inject mock recorders)
- No global state
- Follows Go best practices

### 3. Engine-Agnostic Data Model

Uses `ExternalWorkflowID` instead of `TemporalWorkflowID`, with a `WorkflowEngine` field to identify the source.

**Supports**:
- Temporal: `WorkflowEngine = "temporal"`
- Airflow: `WorkflowEngine = "airflow"`
- Manual: `WorkflowEngine = ""` or `"manual"`

## Migration to ADDP Meta Module

This code is designed for eventual integration into the ADDP Meta module:

**Migration Steps**:
1. Copy `lineage/` to `meta/backend/internal/lineage/`
2. Change storage from SQLite to PostgreSQL `metadata` schema
3. Add lineage API routes to Meta module router
4. Integrate with Meta's scanning service
5. Use from Transfer module activities

**No Changes Required**:
- ✅ Data models (`models/lineage.go`, `models/types.go`)
- ✅ Repository layer (`repository/lineage_repository.go`)
- ✅ Service layer (`service/lineage_service.go`, `service/recorder.go`)
- ✅ Only storage configuration needs update (SQLite → PostgreSQL)

## Development Guidelines

### Adding New Lineage Types

Edit `lineage/models/lineage.go` and add to `LineageType` constants:

```go
const (
    LineageTypeTransform  = "transform"
    LineageTypeCopy       = "copy"
    LineageTypeMerge      = "merge"
    LineageTypeAggregate  = "aggregate"
    LineageTypeYourNew    = "your_new"  // Add here
)
```

### Extending the Lineage Graph

Modify `service/lineage_service.go`:

```go
func (s *LineageService) BuildLineageGraph(ctx context.Context, itemID uint, maxDepth int) (*LineageGraph, error) {
    // Add custom graph building logic
}
```

### Adding New Query Filters

Edit `models/types.go`:

```go
type LineageQuery struct {
    SourceItemID *uint   `json:"source_item_id,omitempty"`
    TargetItemID *uint   `json:"target_item_id,omitempty"`
    YourFilter   *string `json:"your_filter,omitempty"`  // Add here
    // ...
}
```

Then implement filtering in `repository/lineage_repository.go`.

## Important Files

### Documentation
- [`LINEAGE_REFACTOR.md`](LINEAGE_REFACTOR.md) - Refactoring report and design rationale
- [`lineage/README.md`](lineage/README.md) - Lineage library usage guide
- [`lineage-demo/README.md`](lineage-demo/README.md) - Temporal integration guide

### Core Code
- [`lineage/models/lineage.go`](lineage/models/lineage.go) - Core data model
- [`lineage/service/recorder.go`](lineage/service/recorder.go) - LineageRecorder interface
- [`lineage/service/lineage_service.go`](lineage/service/lineage_service.go) - Business logic
- [`lineage-demo/workflow/lineage_workflow.go`](lineage-demo/workflow/lineage_workflow.go) - Temporal integration

### Scripts
- [`init_test_data.sh`](init_test_data.sh) - Initialize test data in PostgreSQL
- [`lineage-demo/query_lineage.sh`](lineage-demo/query_lineage.sh) - Query lineage via curl

## Troubleshooting

### Temporal Server not running
```bash
cd temporal
docker-compose up -d
```

### Database connection failed
```bash
# Check PostgreSQL is running
psql -h localhost -U addp -d addp -c "SELECT 1;"

# If using default ADDP setup:
# - Host: localhost
# - Port: 5432
# - User: addp
# - Password: addp_password
# - Database: addp
```

### Worker fails to start
```bash
# Ensure lineage database is initialized
cd lineage-demo/worker
rm -f lineage.db  # Clean slate
go run main.go    # Will auto-create tables
```

### API returns empty results
```bash
# Insert test data first
cd /Users/pampa/code/addp/labs/orch
./init_test_data.sh
```

## Testing Strategy

### Unit Tests (lineage library)
```bash
cd lineage
go test ./models -v
go test ./repository -v
go test ./service -v
```

### Integration Tests (lineage-demo)
```bash
# Full workflow test
cd lineage-demo
go test -v
```

### Manual Testing
```bash
# 1. Start all services
cd temporal && docker-compose up -d
cd lineage-demo/worker && go run main.go &
cd lineage-demo/api && go run server.go &

# 2. Trigger workflow
cd lineage-demo/starter && go run main.go

# 3. Verify results
curl http://localhost:9090/api/lineage/all | jq
sqlite3 worker/lineage.db "SELECT * FROM data_lineages;"
```

## Related Documentation

- Main ADDP project: [`../../CLAUDE.md`](../../CLAUDE.md)
- Lineage refactoring report: [`LINEAGE_REFACTOR.md`](LINEAGE_REFACTOR.md)
- Lineage library: [`lineage/README.md`](lineage/README.md)
- Temporal integration: [`lineage-demo/README.md`](lineage-demo/README.md)
