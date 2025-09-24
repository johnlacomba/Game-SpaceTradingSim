#!/bin/bash

echo "🐳 Docker Health Check Deep Diagnosis"
echo "===================================="

echo ""
echo "1️⃣  Checking Current Container Status..."
docker-compose ps

echo ""
echo "2️⃣  Testing Backend Health from Host..."
echo "Trying to reach backend health endpoint from outside Docker..."

# Try different ways to reach the backend
echo "Testing localhost:8080/health..."
if curl -f -m 5 http://localhost:8080/health 2>/dev/null; then
    echo "✅ Backend reachable from host on port 8080"
else
    echo "❌ Backend not reachable from host on port 8080"
fi

echo ""
echo "3️⃣  Testing Health Check from Inside Container..."
# Test the exact same command that Docker health check uses
if docker-compose ps backend | grep -q "Up"; then
    echo "Backend container is running, testing health check command..."
    if docker-compose exec -T backend wget -q --spider http://localhost:8080/health; then
        echo "✅ Health check command works inside container"
    else
        echo "❌ Health check command fails inside container"
        echo "Testing if the endpoint exists..."
        docker-compose exec -T backend wget -qO- http://localhost:8080/health || echo "Endpoint not responding"
    fi
else
    echo "❌ Backend container is not running"
fi

echo ""
echo "4️⃣  Checking Backend Logs..."
echo "Recent backend logs:"
docker-compose logs --tail=20 backend

echo ""
echo "5️⃣  Testing Different Health Check Approaches..."

# Test with curl instead of wget
echo "Testing with curl inside container..."
if docker-compose ps backend | grep -q "Up"; then
    docker-compose exec -T backend sh -c "curl -f http://localhost:8080/health" || echo "curl test failed"
fi

echo ""
echo "6️⃣  Checking Container Network..."
echo "Container internal IP and ports:"
docker-compose exec -T backend sh -c "netstat -ln | grep 8080" || echo "netstat not available"

echo ""
echo "🔍 Diagnosis Complete!"
echo "===================="
echo "If the health endpoint works from inside the container but Docker"
echo "health check still fails, it might be a timing or Docker issue."
