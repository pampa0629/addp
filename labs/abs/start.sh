#!/bin/bash

# ABS Development Startup Script

set -e

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
cd "$PROJECT_ROOT"

# Colors for output
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
RED='\033[0;31m'
NC='\033[0m' # No Color

echo -e "${GREEN}🤖 Starting ABS - AI Bootstrapping System${NC}"
echo ""

# Check if .env exists (at project root)
if [ ! -f ".env" ]; then
    echo -e "${YELLOW}⚠️  No .env file found. Creating from template...${NC}"
    cp .env.example .env
    echo -e "${RED}❌ IMPORTANT: Edit .env and set your CLAUDE_API_KEY${NC}"
    echo -e "${YELLOW}Press Enter after updating the API key...${NC}"
    read
fi

# Check if CLAUDE_API_KEY is set
source .env
if [ -z "$CLAUDE_API_KEY" ] || [ "$CLAUDE_API_KEY" = "your-anthropic-api-key-here" ]; then
    echo -e "${RED}❌ CLAUDE_API_KEY is not set in .env${NC}"
    echo "Please edit .env and add your Anthropic API key"
    exit 1
fi

# Check if dependencies are installed
if [ ! -d "frontend/node_modules" ]; then
    echo -e "${YELLOW}📦 Installing frontend dependencies...${NC}"
    cd frontend && npm install && cd ..
fi

# Start backend
echo -e "${GREEN}🚀 Starting backend on http://localhost:8090${NC}"
cd backend
go run cmd/server/main.go &
BACKEND_PID=$!
cd ..

# Wait for backend to be ready
echo -e "${YELLOW}⏳ Waiting for backend to start...${NC}"
sleep 3

# Start frontend
echo -e "${GREEN}🎨 Starting frontend on http://localhost:5180${NC}"
cd frontend
npm run dev &
FRONTEND_PID=$!
cd ..

echo ""
echo -e "${GREEN}✅ ABS is running!${NC}"
echo ""
echo "  Frontend: http://localhost:5180"
echo "  Backend:  http://localhost:8090"
echo "  WebSocket: ws://localhost:8090/ws"
echo ""
echo -e "${YELLOW}Press Ctrl+C to stop all services${NC}"

# Cleanup function
cleanup() {
    echo ""
    echo -e "${YELLOW}🛑 Stopping services...${NC}"
    kill $BACKEND_PID 2>/dev/null || true
    kill $FRONTEND_PID 2>/dev/null || true
    echo -e "${GREEN}✅ Stopped${NC}"
    exit 0
}

trap cleanup SIGINT SIGTERM

# Wait for processes
wait
