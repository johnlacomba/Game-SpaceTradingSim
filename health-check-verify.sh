#!/bin/bash

echo "🩺 Health Check Verification"
echo "==========================="

echo ""
echo "Step 1: Testing health endpoint locally..."
cd backend
echo "Starting backend..."
go run cmd/server/main.go &
BACKEND_PID=$!

# Wait for server to start
sleep 3

echo "Testing /health endpoint..."
HEALTH_RESPONSE=$(curl -s http://localhost:8080/health)
if [ "$HEALTH_RESPONSE" = "OK" ]; then
    echo "✅ /health endpoint works: $HEALTH_RESPONSE"
else
    echo "❌ /health endpoint failed: $HEALTH_RESPONSE"
fi

echo "Testing /ping endpoint..."
PING_RESPONSE=$(curl -s http://localhost:8080/ping)
if [ "$PING_RESPONSE" = "pong" ]; then
    echo "✅ /ping endpoint works: $PING_RESPONSE"
else
    echo "❌ /ping endpoint failed: $PING_RESPONSE"
fi

# Stop backend
kill $BACKEND_PID
wait $BACKEND_PID 2>/dev/null
cd ..

echo ""
echo "Step 2: Testing Docker health check command..."
echo "Starting containers..."
docker-compose up -d backend

# Wait for container to start
sleep 10

echo "Testing health check from inside container..."
if docker-compose exec -T backend sh -c "wget --quiet --tries=1 --spider http://localhost:8080/health"; then
    echo "✅ Docker health check command works"
else
    echo "❌ Docker health check command failed"
    echo "Trying alternative approaches..."
    
    # Try with verbose output
    echo "Testing with curl:"
    docker-compose exec -T backend curl -f http://localhost:8080/health || echo "curl failed"
    
    echo "Testing with wget (verbose):"
    docker-compose exec -T backend wget -qO- http://localhost:8080/health || echo "wget verbose failed"
    
    echo "Checking if port 8080 is listening:"
    docker-compose exec -T backend netstat -ln 2>/dev/null | grep 8080 || echo "netstat not available"
fi

echo ""
echo "Step 3: Container logs..."
docker-compose logs --tail=10 backend

echo ""
echo "Step 4: Container status..."
docker-compose ps backend

docker-compose down

echo ""
echo "🔍 Analysis complete!"
echo "If health check works locally but fails in Docker, check:"
echo "1. Container startup timing"
echo "2. Port binding issues"
echo "3. Docker network configuration"
