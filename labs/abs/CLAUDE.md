# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Overview

**Location**: `/Users/pampa/code/addp/labs/`

This is a research and experimentation directory for the ADDP (All Domain Data Platform) project. Each subdirectory contains isolated feature experiments that do not interact with the main ADDP system, allowing for rapid prototyping without increasing complexity.

**Philosophy**: 本目录是为了推进进展,对单个功能做前沿研究和 AI 实现。每个功能点单独一个目录,不和 ADDP 系统发生交互,以避免增加复杂度,从而拖慢研发进度。

### Current Projects

**ABS (AI Bootstrapping System)** - An AI-powered code generation and auto-deployment platform that:

1. Accepts natural language descriptions of code to build
2. Uses Claude/Codex AI to generate production-ready code
3. Automatically compiles and deploys the generated code
4. Monitors progress in real-time via WebSocket
5. Auto-registers generated apps for easy access

Designed for rapid prototyping and AI-assisted development.

## Repository Structure

```
labs/
├── readme.md                     # Repository overview
├── CLAUDE.md                     # This file
├── QUICK_START.md                # Quick start guide
└── abs/                          # AI Bootstrapping System
    ├── readme.md                 # ABS detailed documentation
    ├── QUICKSTART.md             # ABS quick start
    ├── Makefile                  # Build commands
    ├── backend/
    │   ├── cmd/server/main.go    # Application entry point
    │   ├── internal/
    │   │   ├── api/              # HTTP handlers + routing
    │   │   ├── models/           # Task + App data models
    │   │   └── service/          # Business logic layer
    │   │       ├── code_generator.go     # CodeGenerator interface
    │   │       ├── claude_client.go      # Claude API client
    │   │       ├── codex_client.go       # Codex API client
    │   │       ├── codex_cli_client.go   # Codex CLI wrapper
    │   │       ├── config.go             # Config loading
    │   │       ├── task_service.go       # Task pipeline
    │   │       ├── app_service.go        # App registry
    │   │       └── websocket.go          # WebSocket manager
    │   ├── go.mod                # Dependencies
    │   └── .env.example          # Config template
    └── frontend/
        ├── src/
        │   ├── App.vue           # Root component
        │   ├── api/              # HTTP + WebSocket clients
        │   ├── components/       # FloatingInput + AppPreview
        │   ├── store/task.js     # Pinia state
        │   └── views/            # AppPreview page
        └── package.json
```

## Technology Stack

**Backend (Go 1.23)**:
- Gin (HTTP framework)
- Gorilla WebSocket (real-time updates)
- godotenv (configuration)
- External APIs: Anthropic Claude OR OpenAI Codex

**Frontend (Vue 3)**:
- Vite (build tool)
- Pinia (state management)
- Axios (HTTP client)
- Custom WebSocket service with auto-reconnect

**Infrastructure**:
- In-memory storage (stateless, no database)
- WebSocket for real-time progress updates
- File-based workspace for generated code

## Quick Start Commands

```bash
# From labs/abs directory
cd abs

# One-time setup
make init                       # Install deps + create .env
# Edit backend/.env: set CODE_GENERATOR and API keys

# Development (recommended - starts both frontend + backend)
make dev                        # Runs on ports 8090 (backend) + 5180 (frontend)

# Or run separately
make dev-backend                # Backend only
make dev-frontend               # Frontend only

# Utilities
make stop                       # Stop all services
make clean                      # Clean workspace + build artifacts
```

### Backend Commands

```bash
cd abs/backend

go run cmd/server/main.go       # Run server
go build -o abs-server cmd/server/main.go  # Build binary
go test ./...                   # Run tests
go mod download                 # Install dependencies
```

### Frontend Commands

```bash
cd abs/frontend

npm run dev                     # Dev server (http://localhost:5180)
npm run build                   # Production build → dist/
npm run preview                 # Preview production build
npm install                     # Install dependencies
```

## Key Architecture

### Code Generator Abstraction

The system uses a **pluggable code generation interface**:

```go
type CodeGenerator interface {
    GenerateCode(prompt string) (string, error)
    Provider() string
}
```

**Implementations**:
- `ClaudeClient` - Direct Anthropic Claude API
- `CodexClient` - Direct OpenAI/Codex API
- `CodexCLIClient` - Wraps Codex CLI for full planning/execution

**Selection via environment**: `CODE_GENERATOR=codex|codex_cli|claude`

### Async Task Processing Pipeline

```
User Input → Create Task (return immediately)
                ↓ (background goroutine)
         Generate Code (Claude/Codex)
                ↓
         Write to Workspace
                ↓
         Auto-Detect & Compile (Go/Python/Node)
                ↓
         Deploy & Auto-Register App
                ↓
         WebSocket Broadcasts Progress
```

**Task Lifecycle**: `pending → processing → compiling → deploying → completed|failed`

Each stage broadcasts real-time updates via WebSocket to all connected clients.

### Multi-File Code Generation

AI-generated code can include multiple files using filepath markers:

```
// filepath: main.go
package main
...

// filepath: utils.go
package main
...
```

The `parseCodeFiles()` method in `task_service.go` automatically splits this into separate files in the workspace.

### Auto-Detection & Compilation

The system automatically detects languages and compiles/runs:

- **Go**: Detects `package main`, runs `go mod init` + `go mod tidy` + `go build` → `./app`
- **Python**: Detects `.py` files, finds `python3`/`python`, runs entry file
- **Node.js**: Reads `package.json` scripts, runs `npm run dev`/`npm run start`

Detection logic in [task_service.go:226-584](abs/backend/internal/service/task_service.go)

### App Registry & Auto-Registration

After successful deployment, tasks auto-register as apps if:
1. `app_manifest.json` exists in workspace (preferred), OR
2. System can detect start command (binary, package.json, Python entry)

**App Manifest Schema**:
```json
{
  "name": "My App",
  "description": "Description",
  "entry_url": "http://localhost:3000",
  "start_command": ["./app"],
  "category": "tool",
  "icon": "🚀",
  "tags": ["generated"]
}
```

See [app_manifest.go](abs/backend/internal/models/app_manifest.go) for full schema.

## API Reference

### REST Endpoints

```
POST   /api/tasks              # Create task from prompt
GET    /api/tasks              # List all tasks
GET    /api/tasks/:id          # Get task by ID
GET    /health                 # Health check

POST   /api/apps               # Register app
GET    /api/apps               # List apps
GET    /api/apps/:id           # Get app by ID
POST   /api/apps/:id/launch    # Launch app process
DELETE /api/apps/:id           # Delete app
```

### WebSocket

```
WS     /ws                     # Real-time progress updates
```

**Progress Update Format**:
```json
{
  "task_id": "uuid",
  "status": "processing|compiling|deploying|completed|failed",
  "message": "Status description",
  "log": "Optional log entry"
}
```

## Configuration

Backend environment variables in `abs/backend/.env`:

```bash
# Code Generator Selection (IMPORTANT)
CODE_GENERATOR=codex            # Options: codex | codex_cli | claude

# Codex Configuration (default, recommended)
CODEX_API_KEY=sk-...            # Required when CODE_GENERATOR=codex
CODEX_BASE_URL=https://api.aicodemirror.com/api/codex/backend-api/codex
CODEX_MODEL=gpt-5

# Codex CLI Configuration (for planning/execution mode)
CODEX_CLI_PATH=codex            # Path to codex executable
CODEX_CLI_ARGS="--skip-git-repo-check --full-auto"
CODEX_CLI_TIMEOUT=300s

# Claude Configuration (legacy fallback)
CLAUDE_API_KEY=sk-ant-...       # Required when CODE_GENERATOR=claude
CLAUDE_MODEL=claude-sonnet-4-5-20250929

# Server Configuration
PORT=8090
FRONTEND_URL=http://localhost:5180
WORKSPACE_DIR=./workspace
AUTO_RELOAD=true
APPS_DATA_FILE=./apps.json
```

**CORS** is auto-configured based on `FRONTEND_URL`.

## Development Workflows

### Adding Language Support

To support new languages (e.g., Rust, Ruby), edit `task_service.go`:

1. **Add detection** in `compileCode()` method:
   ```go
   if strings.Contains(task.Code, "Cargo.toml") {
       // Rust detected
       cmd := exec.Command("cargo", "build", "--release")
       // ... execute
   }
   ```

2. **Add runtime detection** in `detectStartCommand()` for auto-registration

3. **Test** with a prompt that generates code in that language

### Adding New Code Generator

1. **Implement interface** in `internal/service/`:
   ```go
   type MyLLMClient struct { /* ... */ }
   func (c *MyLLMClient) GenerateCode(prompt string) (string, error) { /* ... */ }
   func (c *MyLLMClient) Provider() string { return "myllm" }
   ```

2. **Add to resolver** in `task_service.go`:
   ```go
   case "myllm":
       return NewMyLLMClient(config.MyLLMAPIKey, ...)
   ```

3. **Update config.go** to load required environment variables

### Debugging Tips

**WebSocket in Browser Console**:
```javascript
const ws = new WebSocket('ws://localhost:8090/ws')
ws.onmessage = (e) => console.log('Progress:', JSON.parse(e.data))
ws.readyState  // 0=connecting, 1=open, 2=closing, 3=closed
```

**Check Backend Health**:
```bash
curl http://localhost:8090/health
```

**View Generated Code**:
```bash
ls -la abs/backend/workspace/<task-id>/
cat abs/backend/workspace/<task-id>/main.go
```

## Common Issues

### "CODEX_API_KEY is required" or "CLAUDE_API_KEY is required"

**Problem**: Missing API key for selected code generator.

**Solution**:
1. Set `CODE_GENERATOR` in `.env` to `codex`, `codex_cli`, or `claude`
2. Set corresponding API key:
   - `CODEX_API_KEY=sk-...` for Codex
   - `CLAUDE_API_KEY=sk-ant-...` for Claude
3. Restart backend

### Port Already in Use

**Problem**: Another process is using port 8090 or 5180.

**Solution**:
```bash
# Kill process on port
lsof -ti:8090 | xargs kill     # Backend
lsof -ti:5180 | xargs kill     # Frontend

# Or change port in .env
PORT=8091
```

### Frontend Cannot Connect to Backend

**Problem**: CORS errors or connection refused.

**Solution**:
1. Ensure backend is running: `curl http://localhost:8090/health`
2. Check `FRONTEND_URL` in backend `.env` matches frontend origin
3. Check browser console (F12) for specific error

### WebSocket Disconnects

**Problem**: WebSocket connection drops frequently.

**Solution**: Frontend auto-reconnects with exponential backoff. If persistent:
1. Check backend logs for errors
2. Verify firewall/proxy isn't blocking WebSocket
3. Check browser console for close code/reason

### Compilation Failures

**Problem**: Generated code has syntax errors.

**Solution**:
1. Make prompt more specific and detailed
2. Check task logs in UI for compiler errors
3. Manually fix code in `workspace/<task-id>/` and re-run
4. Try different code generator (Claude vs Codex)
5. Simplify the request to smaller components

### App Not Auto-Registering

**Problem**: Task completes but app doesn't appear in registry.

**Solution**:
1. Check if `app_manifest.json` was generated in workspace
2. Verify app has detectable start command (compiled binary, package.json, or Python entry)
3. Check task logs for auto-registration messages
4. Manually register via `POST /api/apps` if needed

## Security Considerations

⚠️ **This is a development tool with security implications**:

1. **Arbitrary Code Execution**: AI-generated code runs with full system access
2. **No Sandboxing**: Generated apps can access filesystem, network, etc.
3. **API Key Storage**: `.env` contains sensitive keys (gitignored but local)
4. **No Authentication**: All endpoints are public
5. **No Rate Limiting**: Unbounded API calls possible

**For production deployment**, implement:
- User authentication and authorization
- Docker/VM sandboxing for generated code execution
- Resource limits (CPU, memory, disk quotas)
- Rate limiting and API quota management
- Code review workflow before execution
- HTTPS/TLS encryption
- Audit logging

## Project Philosophy

This `labs/` directory is intentionally isolated from the main ADDP system to:

- ✅ Enable rapid experimentation without affecting production
- ✅ Test bleeding-edge AI code generation capabilities
- ✅ Validate architectural patterns before ADDP integration
- ✅ Serve as prototype for future ADDP AI-powered features
- ✅ Minimize complexity and maximize development speed

When features mature, they may be integrated into the main ADDP platform with appropriate production hardening.

## References

- Main ADDP documentation: See parent directory CLAUDE.md
- Anthropic Claude API: https://docs.anthropic.com/
- Gin framework: https://gin-gonic.com/
- Vue 3: https://vuejs.org/
- WebSocket Protocol: https://developer.mozilla.org/en-US/docs/Web/API/WebSocket

---

**Last Updated**: 2025-01-09
**Version**: 0.2.0
**Status**: Active development / Experimental

