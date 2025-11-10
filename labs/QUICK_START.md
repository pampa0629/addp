# ABS Quick Start Guide

## What is ABS?

**AI Bootstrapping System** - A tool that lets you describe code in English, and AI generates, compiles, and deploys it automatically.

## Project Location

```
/Users/pampa/code/addp/labs/
├── abs/backend/     # Go server (port 8090)
├── abs/frontend/    # Vue app (port 5180)
└── CLAUDE.md        # Full architecture documentation
```

## 5-Minute Setup

### 1. Backend Setup
```bash
cd /Users/pampa/code/addp/labs/abs/backend

# Copy environment template
cp .env.example .env

# Edit .env and add your Anthropic API key:
# CLAUDE_API_KEY=sk-ant-xxxxxxxxxxxx

# Run the server
go run cmd/server/main.go
# ✓ Server running on http://localhost:8090
```

### 2. Frontend Setup (new terminal)
```bash
cd /Users/pampa/code/addp/labs/abs/frontend

# Install dependencies
npm install

# Start dev server
npm run dev
# ✓ Frontend running on http://localhost:5180
```

### 3. Try It Out
1. Open http://localhost:5180 in your browser
2. Click the floating input box in bottom-right
3. Type: "Create a simple HTTP server that returns Hello World"
4. Click "Generate & Deploy"
5. Watch the AI generate, compile, and deploy your code!

## Key Commands

### Backend
```bash
cd abs/backend
go run cmd/server/main.go          # Run server
go build -o abs-server             # Build binary
go test ./...                       # Run tests
```

### Frontend
```bash
cd abs/frontend
npm install                         # Install deps
npm run dev                         # Dev server
npm run build                       # Production build
npm run preview                     # Preview prod build
```

## Project Structure

```
Backend (Go):
internal/api/          - HTTP handlers & routes
internal/service/      - Business logic
  ├── task_service.go    - Task processing pipeline
  ├── claude_client.go    - Claude API integration
  └── websocket.go        - Real-time updates
internal/models/       - Data structures
cmd/server/main.go     - Entry point

Frontend (Vue 3):
src/App.vue            - Main page
src/components/        - UI components
src/store/            - State management (Pinia)
src/api/              - HTTP/WebSocket clients
```

## How It Works

```
User Input
   ↓
POST /api/tasks
   ↓
Backend creates task (async)
   ↓
ClaudeClient generates code
   ↓
Write to workspace
   ↓
Compile (if Go)
   ↓
Deploy (run binary)
   ↓
WebSocket broadcasts progress
   ↓
Frontend updates UI in real-time
```

## API Reference

### REST Endpoints
```
POST   /api/tasks       - Create task
GET    /api/tasks       - List tasks
GET    /api/tasks/:id   - Get task details
GET    /health          - Health check
```

### WebSocket
```
WS     /ws              - Real-time progress updates
```

## Configuration

Edit `.env` in backend:
```bash
CLAUDE_API_KEY=sk-ant-...                     # REQUIRED
CLAUDE_MODEL=claude-sonnet-4-5-20250929       # LLM model
PORT=8090                                     # Backend port
FRONTEND_URL=http://localhost:5180            # For CORS
WORKSPACE_DIR=./workspace                     # Code output dir
AUTO_RELOAD=true                              # Auto-reload
```

## Troubleshooting

### "CLAUDE_API_KEY is required"
- You must set CLAUDE_API_KEY in .env
- Get key from https://console.anthropic.com/

### Frontend can't connect to backend
- Ensure backend is running on port 8090
- Check FRONTEND_URL in backend .env matches your frontend origin

### WebSocket connection fails
- Check browser console (F12)
- Verify backend WebSocket endpoint: ws://localhost:8090/ws

### Go build fails
- Run `go mod download` to fetch dependencies
- Ensure Go 1.23+ is installed: `go version`

### npm install fails
- Delete node_modules: `rm -rf node_modules`
- Clear cache: `npm cache clean --force`
- Try again: `npm install`

## What to Do Next

### For Backend Development
1. Read `abs/backend/internal/service/task_service.go` to understand the pipeline
2. Check out `internal/service/claude_client.go` for Claude API integration
3. See `internal/api/router.go` for available endpoints

### For Frontend Development
1. Check `src/App.vue` for main layout
2. Review `src/components/FloatingInput.vue` for UI widget
3. Explore `src/store/task.js` for state management

### To Add Features
- New API endpoint? Add to `internal/api/handler.go` + `router.go`
- New task stage? Modify `internal/service/task_service.go`
- New UI component? Create in `src/components/`
- New state? Extend `src/store/task.js`

## Examples

### Generate a Python Server
```
Create a Flask API server with /hello and /goodbye endpoints
```

### Generate a Frontend
```
Create a Vue 3 component that displays a list of todos with add/remove functionality
```

### Generate with Specific Requirements
```
Create a Go server that:
- Listens on port 3000
- Has a /users endpoint that returns JSON
- Uses error handling for all routes
```

## Links

- **CLAUDE.md** - Full architecture guide (this directory)
- **Anthropic Docs** - https://docs.anthropic.com/
- **Gin Framework** - https://gin-gonic.com/
- **Vue 3** - https://vuejs.org/
- **Vite** - https://vitejs.dev/

## Performance

- Current setup: 10-50 concurrent users
- Single process, in-memory storage
- No database persistence (lost on restart)
- Each task runs in background goroutine

For production: Add database, load balancer, monitoring

## Security Notes

⚠️ **Development Only** - Not for production use:
- No authentication
- Public API endpoints
- Executes code from Claude (not sandboxed)
- Workspace files not isolated
- No rate limiting

For production: Add auth, sandboxing, rate limiting, HTTPS

## File Sizes

- Backend code: ~600 lines of Go
- Frontend code: ~800 lines of Vue
- Total: ~1400 lines (excluding tests)

Minimal, focused codebase - easy to understand and extend.

## Next Steps

1. Get Claude API key: https://console.anthropic.com/
2. Set up backend: `cp .env.example .env` + add API key
3. Start backend: `go run cmd/server/main.go`
4. Start frontend: `npm install && npm run dev`
5. Open http://localhost:5180
6. Try generating your first AI-powered app!

---

For detailed architecture, see **CLAUDE.md** in this directory.
