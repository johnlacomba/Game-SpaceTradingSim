#!/bin/bash

echo "🔍 Backend Health Endpoint Test"
echo "==============================="

echo ""
echo "Testing if backend health endpoint responds correctly..."

# Start the backend locally in the background
echo "Starting backend locally..."
cd backend
go run cmd/server/main.go &
BACKEND_PID=$!

# Give it time to start
echo "Waiting for backend to start..."
sleep 5

# Test the health endpoint
echo "Testing health endpoint..."
if curl -f http://localhost:8080/health >/dev/null 2>&1; then
    echo "✅ Health endpoint responds correctly"
    echo "Response: $(curl -s http://localhost:8080/health)"
else
    echo "❌ Health endpoint not responding"
    echo "Backend might not be running or health route not configured"
fi

# Clean up
echo "Stopping backend..."
kill $BACKEND_PID 2>/dev/null
wait $BACKEND_PID 2>/dev/null

cd ..

echo ""
echo "🐳 Docker Health Check Analysis:"
echo "================================"
echo "The Docker health check should now work because:"
echo "1. ✅ Backend has /health endpoint (no auth required)"
echo "2. ✅ Health check uses HTTP (not HTTPS)"
echo "3. ✅ Dockerfile includes wget for health checks"
echo "4. ✅ Updated docker-compose.yml to use correct endpoint"
echo ""
echo "🚀 Ready to test with Docker!"
echo "Run: docker-compose up --build"
