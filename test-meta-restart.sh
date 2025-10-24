#!/bin/bash
cd /Users/pampa/code/addp/meta/backend
SERVER_PORT=8082 CGO_ENABLED=1 go run cmd/server/main.go > /tmp/meta-test.log 2>&1 &
META_PID=$!
echo "Started Meta backend with PID $META_PID"
sleep 5
if kill -0 $META_PID 2>/dev/null; then
  echo "Meta is running successfully"
  curl -s http://localhost:8082/health
else
  echo "Meta failed to start. Logs:"
  tail -30 /tmp/meta-test.log
fi
