.PHONY: help init dev-backend dev-frontend dev stop clean

help:
	@echo "ABS - AI Bootstrapping System"
	@echo ""
	@echo "Available commands:"
	@echo "  make init          - Initialize project (install dependencies)"
	@echo "  make dev           - Start both backend and frontend in dev mode"
	@echo "  make dev-backend   - Start backend only"
	@echo "  make dev-frontend  - Start frontend only"
	@echo "  make stop          - Stop all running services"
	@echo "  make clean         - Clean build artifacts and workspace"

init:
	@echo "Installing backend dependencies..."
	cd backend && go mod download
	@echo "Installing frontend dependencies..."
	cd frontend && npm install
	@echo "Creating .env file..."
	cp -n .env.example .env || true
	@echo "✅ Initialization complete!"
	@echo ""
	@echo "⚠️  IMPORTANT: Edit .env and set your CLAUDE_API_KEY"

dev-backend:
	@echo "Starting ABS backend..."
	set -a; [ -f .env ] && . ./.env; set +a; cd backend && go run cmd/server/main.go

dev-frontend:
	@echo "Starting ABS frontend..."
	cd frontend && npm run dev

dev:
	@echo "Starting ABS in development mode..."
	@echo "Backend will run on http://localhost:8090"
	@echo "Frontend will run on http://localhost:5180"
	@echo ""
	@make -j2 dev-backend dev-frontend

stop:
	@echo "Stopping ABS services..."
	@# 1. Stop registered apps
	@if [ -f workspace/apps.json ]; then \
		echo "  ⮑ Stopping registered apps..."; \
		APP_PIDS=$$(grep -o '"running_pid":[0-9]*' workspace/apps.json 2>/dev/null | grep -o '[0-9]*' || true); \
		if [ -n "$$APP_PIDS" ]; then \
			for pid in $$APP_PIDS; do \
				if ps -p $$pid >/dev/null 2>&1; then \
					echo "    Killing app PID=$$pid"; \
					kill $$pid 2>/dev/null || true; \
				fi; \
			done; \
		fi; \
	fi
	@# 2. Stop ABS core services
	@pkill -f "go run cmd/server/main.go" || true
	@pkill -f "vite" || true
	@echo "✅ Services stopped"

clean:
	@echo "Cleaning workspace..."
	rm -rf workspace/*
	@echo "Cleaning build artifacts..."
	rm -rf frontend/dist
	rm -rf frontend/node_modules/.vite
	@echo "✅ Cleanup complete"
